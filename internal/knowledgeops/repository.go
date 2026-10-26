package knowledgeops

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrStale = errors.New("record version changed; refresh and retry")
var ErrInvalid = errors.New("invalid knowledge operations input")
var ErrForbidden = errors.New("knowledge operations access denied")

type Repository struct{ db *gorm.DB }

func NewRepository(db *gorm.DB) *Repository { return &Repository{db: db} }
func (r *Repository) Settings(ctx context.Context, tenant uint64) (Settings, error) {
	s := DefaultSettings(tenant)
	err := r.db.WithContext(ctx).Find(&s, "tenant_id = ?", tenant).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return DefaultSettings(tenant), nil
	}
	if err == nil {
		r.populateCalibration(ctx, &s)
	}
	return s, err
}
func (r *Repository) SaveSettings(ctx context.Context, tenant uint64, s Settings) error {
	if s.AutoRecordEnabled && strings.TrimSpace(s.CalibrationNote) == "" || len(s.CalibrationNote) > 4000 || tenant == 0 || s.SamplePercent < 0 || s.SamplePercent > 100 || s.MaxEvidenceChars < 2000 || s.MaxEvidenceChars > 120000 || s.MaxOutputTokens < 512 || s.MaxOutputTokens > 8192 || s.TimeoutSeconds < 10 || s.TimeoutSeconds > 180 || s.Enabled && s.ModelID == "" {
		return ErrInvalid
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		old, err := NewRepository(tx).settingsForCalibrationUpdate(ctx, tenant)
		if err != nil {
			return err
		}
		// Public derived fields are never accepted as approval. Preserve the old
		// server binding on ordinary edits, so changes become stale, not reapproved.
		s.CalibrationBinding = old.CalibrationBinding
		s.CalibrationPolicyVersion = old.CalibrationPolicyVersion
		s.CalibrationModelID = old.CalibrationModelID
		s.CalibrationConfirmedAt = old.CalibrationConfirmedAt
		s.TenantID = tenant
		s.calibrationModelFingerprint = old.calibrationModelFingerprint
		if s.CalibrationConfirmed {
			if strings.TrimSpace(s.CalibrationNote) == "" {
				return ErrInvalid
			}
			if err := NewRepository(tx).lockCalibrationModel(ctx, tenant, s.ModelID); err != nil {
				return ErrInvalid
			}
			fingerprint, err := NewRepository(tx).calibrationModelFingerprint(ctx, tenant, s.ModelID)
			if err != nil {
				return ErrInvalid
			}
			if s.CalibrationSnapshot == "" || s.CalibrationSnapshot != calibrationSnapshot(tenant, s.ModelID, fingerprint) {
				return ErrStale
			}
			s.calibrationModelFingerprint = fingerprint
			s.CalibrationPolicyVersion = PolicyVersion
			s.CalibrationModelID = s.ModelID
			now := time.Now().UTC()
			s.CalibrationConfirmedAt = &now
			s.CalibrationBinding = calibrationBinding(s, SourceLookupPolicyFromEnv())
		} else {
			if !calibrationValid(s) {
				s.CalibrationBinding = ""
			}
			if s.AutoRecordEnabled && !old.AutoRecordEnabled && !calibrationValid(s) {
				return ErrInvalid
			}
		}
		s.CalibrationConfirmed = false
		s.UpdatedAt = time.Now().UTC()
		s.CaptureEnabledAt = old.CaptureEnabledAt
		if s.Enabled && !old.Enabled {
			now := s.UpdatedAt
			s.CaptureEnabledAt = &now
		}
		return tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&s).Error
	})
}
func (r *Repository) CreateRun(ctx context.Context, run *Run) error {
	if run.Gaps == nil {
		run.Gaps = []string{}
	}
	return r.db.WithContext(ctx).Create(run).Error
}
func (r *Repository) Run(ctx context.Context, tenant uint64, id string) (Run, error) {
	var run Run
	err := r.db.WithContext(ctx).First(&run, "tenant_id = ? AND id = ?", tenant, id).Error
	return run, err
}
func (r *Repository) Evidence(ctx context.Context, tenant uint64, id string) ([]Evidence, error) {
	v := []Evidence{}
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND run_id = ?", tenant, id).Order("acquired_at, id").Find(&v).Error
	return v, err
}
func (r *Repository) Analyses(ctx context.Context, tenant uint64, id string) ([]Analysis, error) {
	v := []Analysis{}
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND run_id = ?", tenant, id).Order("version DESC").Find(&v).Error
	return v, err
}
func (r *Repository) Enqueue(ctx context.Context, run Run, version int) error {
	t := Task{ID: uuid.NewString(), TenantID: run.TenantID, RunID: run.ID, Version: version, Status: "pending", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "run_id"}, {Name: "version"}}, DoNothing: true}).Create(&t).Error
}
func (r *Repository) Claim(ctx context.Context, now time.Time, lease time.Duration) (*Task, error) {
	var candidates []Task
	err := r.db.WithContext(ctx).Where("tenant_id IN (SELECT tenant_id FROM knowledge_ops_settings WHERE enabled = ?) AND (status = ? OR (status = ? AND lease_until < ?))", true, "pending", "running", now).Order("created_at, id").Limit(20).Find(&candidates).Error
	if err != nil {
		return nil, err
	}
	for _, t := range candidates {
		settings, e := r.Settings(ctx, t.TenantID)
		if e != nil {
			return nil, e
		}
		if !settings.Enabled {
			continue
		}
		token := uuid.NewString()
		until := now.Add(lease)
		q := r.db.WithContext(ctx).Model(&Task{}).Where("id = ? AND (status = ? OR (status = ? AND lease_until < ?))", t.ID, "pending", "running", now).Updates(map[string]any{"status": "running", "lease_token": token, "lease_until": until, "attempts": gorm.Expr("attempts + 1"), "updated_at": now})
		if q.Error != nil {
			return nil, q.Error
		}
		if q.RowsAffected == 1 {
			t.Status = "running"
			t.LeaseToken = token
			t.LeaseUntil = &until
			t.Attempts++
			return &t, nil
		}
	}
	return nil, nil
}
func (r *Repository) CompleteTask(ctx context.Context, t Task) error {
	q := r.db.WithContext(ctx).Model(&Task{}).Where("id = ? AND lease_token = ? AND status = ?", t.ID, t.LeaseToken, "running").Updates(map[string]any{"status": "succeeded", "lease_until": nil, "updated_at": time.Now().UTC()})
	if q.Error != nil {
		return q.Error
	}
	if q.RowsAffected != 1 {
		return ErrStale
	}
	return nil
}
func (r *Repository) SaveAnalysis(ctx context.Context, t Task, a Analysis, initialSettings ...Settings) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockRun(tx, t.TenantID, t.RunID); err != nil {
			return err
		}
		if err := NewRepository(tx).CompleteTask(ctx, t); err != nil {
			return err
		}
		var initial Settings
		if len(initialSettings) > 0 {
			initial = initialSettings[0]
		}
		if err := NewRepository(tx).recheckAutomaticCalibration(ctx, t.TenantID, initial, &a); err != nil {
			return err
		}
		a.ID = uuid.NewString()
		a.TenantID = t.TenantID
		a.RunID = t.RunID
		a.Version = t.Version
		a.CreatedAt = time.Now().UTC()
		a.PolicyVersion = PolicyVersion
		return tx.Create(&a).Error
	})
}
func (r *Repository) AddReview(ctx context.Context, tenant uint64, v *Review) error {
	v.Reason = strings.TrimSpace(v.Reason)
	v.CauseCategory = "" // New reviews only assess answers; legacy cause rows are read-only.
	if v.Target != "answer" || v.ReviewerID == "" || len(v.Reason) > 4000 {
		return ErrInvalid
	}
	switch v.Action {
	case "accepted", "revision_required", "excluded", "needs_more_evidence":
		if v.Verdict != answerReviewVerdict(v.Action) {
			return ErrInvalid
		}
	case "confirmed", "corrected":
		allowed := map[string]bool{"supported": true, "partial": true, "contradicted": true, "unsupported": true, "unverifiable": true, "not_applicable": true}
		if !allowed[v.Verdict] {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	if (v.Action == "corrected" || v.Action == "revision_required" || v.Action == "needs_more_evidence") && v.Reason == "" {
		return ErrInvalid
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockRun(tx, tenant, v.RunID); err != nil {
			return err
		}
		var a Analysis
		if err := tx.First(&a, "tenant_id = ? AND run_id = ? AND id = ?", tenant, v.RunID, v.AnalysisID).Error; err != nil {
			return err
		}
		var newer int64
		if err := tx.Model(&Analysis{}).Where("tenant_id = ? AND run_id = ? AND version > ?", tenant, v.RunID, a.Version).Count(&newer).Error; err != nil {
			return err
		}
		if v.Action == "confirmed" && v.Verdict != a.Report.Verdict {
			return ErrInvalid
		}
		if newer > 0 {
			return ErrStale
		}
		v.ID = uuid.NewString()
		v.TenantID = tenant
		v.CreatedAt = time.Now().UTC()
		return tx.Create(v).Error
	})
}
func (r *Repository) Retry(ctx context.Context, tenant uint64, id string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockRun(tx, tenant, id); err != nil {
			return err
		}
		run, err := NewRepository(tx).Run(ctx, tenant, id)
		if err != nil {
			return err
		}
		if run.FinishedAt == nil {
			return fmt.Errorf("%w: run is still active", ErrInvalid)
		}
		var active int64
		if err = tx.Model(&Task{}).Where("tenant_id = ? AND run_id = ? AND status IN ?", tenant, id, []string{"pending", "running"}).Count(&active).Error; err != nil {
			return err
		}
		if active > 0 {
			return ErrStale
		}
		var max int
		if err := tx.Model(&Task{}).Where("tenant_id = ? AND run_id = ?", tenant, id).Select("COALESCE(MAX(version),0)").Scan(&max).Error; err != nil {
			return err
		}
		return NewRepository(tx).Enqueue(ctx, run, max+1)
	})
}

// PostgreSQL serializes version creation and reviews on the same parent row.
// SQLite serializes write transactions; its dialect omits FOR UPDATE.
func lockRun(tx *gorm.DB, tenant uint64, id string) error {
	var run Run
	return tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&run, "tenant_id = ? AND id = ?", tenant, id).Error
}
