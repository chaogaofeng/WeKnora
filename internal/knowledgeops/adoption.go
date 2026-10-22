package knowledgeops

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Adoption is an immutable, explicitly approved FAQ publication snapshot. It is
// separate from the human review: successful review does not mean indexing did.
type Adoption struct {
	ID             string     `json:"id" gorm:"primaryKey"`
	TenantID       uint64     `json:"-"`
	RunID          string     `json:"run_id"`
	AnalysisID     string     `json:"analysis_id"`
	ReviewID       string     `json:"review_id"`
	SessionID      string     `json:"session_id"`
	MessageID      string     `json:"message_id"`
	SourceActorID  string     `json:"source_actor_id"`
	ActorID        string     `json:"actor_id"`
	KBID           string     `json:"kb_id"`
	TargetTenantID uint64     `json:"-"`
	Question       string     `json:"question"`
	Answer         string     `json:"answer"`
	Status         string     `json:"status"`
	EntryID        int64      `json:"entry_id"`
	KnowledgeID    string     `json:"knowledge_id"`
	Reused         bool       `json:"reused"`
	Error          string     `json:"error"`
	LeaseToken     string     `json:"-"`
	LeaseUntil     *time.Time `json:"-"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func (Adoption) TableName() string { return "knowledge_ops_adoptions" }

type AdoptionInput struct {
	AnalysisID string `json:"analysis_id"`
	ReviewID   string `json:"review_id"`
	KBID       string `json:"kb_id"`
	Question   string `json:"question"`
	Answer     string `json:"answer"`
}
type AdoptionTarget struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// The lease outlives the bounded external publish and final persistence. A
// process that dies leaves a recoverable publishing snapshot, never a lock.
const adoptionLeaseDuration = 45 * time.Second
const adoptionPublishTimeout = 20 * time.Second

func (s *Service) SetAdoptionAdapter(adapter *AdoptionAdapter) { s.adoption = adapter }
func adoptionAuthority(ctx context.Context, tenant uint64) error {
	caller := types.CallerFromContext(ctx)
	principal, hasPrincipal := types.PrincipalFromContext(ctx)
	if tenant == 0 || caller.TenantID != tenant || caller.UserID == "" || !caller.Role.HasPermission(types.TenantRoleAdmin) || (hasPrincipal && principal.Type != types.PrincipalWebUser) {
		return ErrForbidden
	}
	return nil
}
func (s *Service) AdoptionTargets(ctx context.Context, tenant uint64) ([]AdoptionTarget, error) {
	if err := adoptionAuthority(ctx, tenant); err != nil {
		return nil, err
	}
	if s.adoption == nil {
		return nil, ErrInvalid
	}
	return s.adoption.targets(ctx)
}
func (s *Service) Adopt(ctx context.Context, tenant uint64, runID string, input AdoptionInput) (Adoption, error) {
	if err := adoptionAuthority(ctx, tenant); err != nil {
		return Adoption{}, err
	}
	if s.adoption == nil {
		return Adoption{}, ErrInvalid
	}
	input.Question = strings.TrimSpace(input.Question)
	input.Answer = strings.TrimSpace(input.Answer)
	if input.AnalysisID == "" || input.ReviewID == "" || input.KBID == "" || input.Question == "" || input.Answer == "" || len([]rune(input.Question)) > 16000 || len([]rune(input.Answer)) > 24000 {
		return Adoption{}, ErrInvalid
	}
	d, err := s.loadDetail(ctx, tenant, runID, false)
	if err != nil {
		return Adoption{}, err
	}
	_, kb, err := s.adoption.target(ctx, input.KBID, 0)
	if err != nil {
		return Adoption{}, err
	}
	value, claimed, err := s.repo.admitAdoption(ctx, d.Run, input, kb.TenantID)
	if err != nil || !claimed {
		return value, err
	}
	return s.publishAdoption(ctx, value)
}
func (s *Service) RetryAdoption(ctx context.Context, tenant uint64, runID, id string) (Adoption, error) {
	if err := adoptionAuthority(ctx, tenant); err != nil {
		return Adoption{}, err
	}
	if s.adoption == nil {
		return Adoption{}, ErrInvalid
	}
	d, err := s.loadDetail(ctx, tenant, runID, false)
	if err != nil {
		return Adoption{}, err
	}
	var value Adoption
	if err = s.repo.db.WithContext(ctx).First(&value, "tenant_id = ? AND run_id = ? AND id = ?", tenant, runID, id).Error; err != nil {
		return Adoption{}, err
	}
	if _, _, err = s.adoption.target(ctx, value.KBID, value.TargetTenantID); err != nil {
		return Adoption{}, err
	}
	input := AdoptionInput{AnalysisID: value.AnalysisID, ReviewID: value.ReviewID, KBID: value.KBID, Question: value.Question, Answer: value.Answer}
	value, claimed, err := s.repo.admitAdoption(ctx, d.Run, input, value.TargetTenantID)
	if err != nil || !claimed {
		return value, err
	}
	return s.publishAdoption(ctx, value)
}
func (r *Repository) adoptions(ctx context.Context, tenant uint64, runID string) ([]Adoption, error) {
	values := []Adoption{}
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND run_id = ?", tenant, runID).Order("created_at DESC,id").Find(&values).Error
	return values, err
}
func (s *Service) detailWithAdoptions(ctx context.Context, tenant uint64, id string) (Detail, error) {
	detail, err := s.loadDetail(ctx, tenant, id, true)
	if err != nil {
		return Detail{}, err
	}
	detail.Adoptions, err = s.repo.adoptions(ctx, tenant, id)
	return detail, err
}

// All admissions serialize with analysis/review version creation on the parent
// row. This short transaction is the approval snapshot's linearization point;
// it ends before calling FAQ services or embedding/indexing.
func (r *Repository) admitAdoption(ctx context.Context, run Run, input AdoptionInput, targetTenant uint64) (Adoption, bool, error) {
	var value Adoption
	claimed := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockRun(tx, run.TenantID, run.ID); err != nil {
			return err
		}
		err := tx.First(&value, "tenant_id = ? AND run_id = ? AND analysis_id = ?", run.TenantID, run.ID, input.AnalysisID).Error
		exists := err == nil
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if exists {
			if value.ReviewID != input.ReviewID || value.KBID != input.KBID || value.TargetTenantID != targetTenant || value.Question != input.Question || value.Answer != input.Answer {
				return ErrStale
			}
			if value.Status == "succeeded" {
				return nil
			}
			if value.Status == "publishing" && value.LeaseUntil != nil && value.LeaseUntil.After(time.Now().UTC()) {
				return nil
			}
		}
		if err := validateAdoptionReview(tx, run.TenantID, run.ID, input.AnalysisID, input.ReviewID); err != nil {
			return err
		}
		now := time.Now().UTC()
		until := now.Add(adoptionLeaseDuration)
		token := uuid.NewString()
		if !exists {
			value = Adoption{ID: uuid.NewString(), TenantID: run.TenantID, RunID: run.ID, AnalysisID: input.AnalysisID, ReviewID: input.ReviewID, SessionID: run.SessionID, MessageID: run.MessageID, SourceActorID: run.ActorID, ActorID: types.CallerFromContext(ctx).UserID, KBID: input.KBID, TargetTenantID: targetTenant, Question: input.Question, Answer: input.Answer, Status: "publishing", LeaseToken: token, LeaseUntil: &until, CreatedAt: now, UpdatedAt: now}
			if err := tx.Create(&value).Error; err != nil {
				return err
			}
		} else {
			q := tx.Model(&Adoption{}).Where("id = ? AND tenant_id = ? AND status <> ? AND (lease_until IS NULL OR lease_until <= ?)", value.ID, run.TenantID, "succeeded", now).
				Updates(map[string]any{"status": "publishing", "error": "", "lease_token": token, "lease_until": until, "updated_at": now})
			if q.Error != nil {
				return q.Error
			}
			if q.RowsAffected != 1 {
				return ErrStale
			}
			value.Status = "publishing"
			value.Error = ""
			value.LeaseToken = token
			value.LeaseUntil = &until
			value.UpdatedAt = now
		}
		claimed = true
		return nil
	})
	return value, claimed, err
}
func validateAdoptionReview(tx *gorm.DB, tenant uint64, runID, analysisID, reviewID string) error {
	var latest Analysis
	if err := tx.Where("tenant_id = ? AND run_id = ?", tenant, runID).Order("version DESC").First(&latest).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrInvalid
		}
		return err
	}
	if latest.ID != analysisID {
		return ErrStale
	}
	var pending int64
	if err := tx.Model(&Task{}).Where("tenant_id = ? AND run_id = ? AND version > ? AND status IN ?", tenant, runID, latest.Version, []string{"pending", "running"}).Count(&pending).Error; err != nil {
		return err
	}
	if pending != 0 {
		return ErrStale
	}
	var review Review
	if err := tx.Where("tenant_id = ? AND run_id = ? AND analysis_id = ? AND target = ?", tenant, runID, analysisID, "answer").Order("created_at DESC,id").First(&review).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrInvalid
		}
		return err
	}
	if review.ID != reviewID {
		return ErrStale
	}
	if review.Action != "accepted" || review.Verdict != "usable" {
		return ErrInvalid
	}
	return nil
}
func (s *Service) publishAdoption(ctx context.Context, value Adoption) (Adoption, error) {
	publishCtx, cancel := context.WithTimeout(ctx, adoptionPublishTimeout)
	defer cancel()
	var entry *types.FAQEntry
	reused := false
	code := "faq_publish_failed"
	// Recheck both resource access and the approval immediately before the
	// external side effect; retry never turns a revoked snapshot into a grant.
	targetCtx, err := s.authorizeAdoptionPublication(publishCtx, value)
	if err == nil {
		entry, reused, code, err = s.adoption.publish(targetCtx, value, func(checkCtx context.Context) (context.Context, error) {
			return s.authorizeAdoptionPublication(checkCtx, value)
		})
	}
	if errors.Is(err, ErrForbidden) {
		code = "access_revoked"
	}
	if errors.Is(err, ErrStale) || errors.Is(err, ErrInvalid) {
		code = "approval_changed"
	}
	status := "succeeded"
	if err != nil {
		status = "failed"
		logger.Warnf(ctx, "[KnowledgeOps] FAQ adoption failed id=%s run=%s code=%s: %v", value.ID, value.RunID, code, err)
	}
	updates := map[string]any{"status": status, "error": "", "lease_token": "", "lease_until": nil, "updated_at": time.Now().UTC()}
	if err != nil {
		updates["error"] = code
	} else {
		updates["entry_id"] = entry.ID
		updates["knowledge_id"] = entry.KnowledgeID
		updates["reused"] = reused
	}
	// A lost client connection must not strand a successfully indexed entry.
	// Lease-token fencing prevents an expired publisher finalizing a new lease.
	saveCtx, saveCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer saveCancel()
	q := s.repo.db.WithContext(saveCtx).Model(&Adoption{}).Where("id = ? AND tenant_id = ? AND status = ? AND lease_token = ?", value.ID, value.TenantID, "publishing", value.LeaseToken).Updates(updates)
	if q.Error != nil {
		return value, q.Error
	}
	if q.RowsAffected != 1 {
		return value, ErrStale
	}
	err = s.repo.db.WithContext(saveCtx).First(&value, "id = ? AND tenant_id = ?", value.ID, value.TenantID).Error
	return value, err
}

// FAQ listing can involve application services. Authorize again after that read
// and immediately before creation so a changed review or revoked source/target
// cannot be turned into a write using an earlier context grant.
func (s *Service) authorizeAdoptionPublication(ctx context.Context, value Adoption) (context.Context, error) {
	if _, err := s.loadDetail(ctx, value.TenantID, value.RunID, false); err != nil {
		return ctx, err
	}
	if err := validateAdoptionReview(s.repo.db.WithContext(ctx), value.TenantID, value.RunID, value.AnalysisID, value.ReviewID); err != nil {
		return ctx, err
	}
	scoped, _, err := s.adoption.target(ctx, value.KBID, value.TargetTenantID)
	return scoped, err
}
