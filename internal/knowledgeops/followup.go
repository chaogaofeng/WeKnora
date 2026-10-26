package knowledgeops

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Followup is an append-only operational decision, independent of diagnosis,
// answer review and source evidence. PreviousID makes each route a linear audit.
type Followup struct {
	ID         string    `json:"id" gorm:"primaryKey"`
	TenantID   uint64    `json:"-"`
	RunID      string    `json:"run_id"`
	AnalysisID string    `json:"analysis_id"`
	Route      string    `json:"route"`
	Action     string    `json:"action"`
	Reason     string    `json:"reason"`
	ActorID    string    `json:"actor_id"`
	CreatedAt  time.Time `json:"created_at"`
	PreviousID string    `json:"previous_id"`
}

func (Followup) TableName() string { return "knowledge_ops_followups" }

type FollowupInput struct {
	AnalysisID string `json:"analysis_id"`
	Route      string `json:"route"`
	Action     string `json:"action"`
	Reason     string `json:"reason"`
	PreviousID string `json:"previous_id"`
}

type FollowupState struct {
	Route  string    `json:"route"`
	Reason string    `json:"reason"`
	State  string    `json:"state"`
	Latest *Followup `json:"latest,omitempty"`
}

// deriveFollowups uses only records bound to this diagnosis. Old decisions are
// still returned in Detail.Followups, but never close work in a newer version.
func deriveFollowups(d Detail) []FollowupState {
	states := []FollowupState{}
	for _, issue := range []FollowupState{
		{Route: "source_verification", Reason: sourceFollowupReason(d.Run), State: "pending"},
		{Route: "technical_followup", Reason: technicalFollowupReason(d.Run), State: "pending"},
	} {
		if issue.Reason == "" {
			continue
		}
		if d.Run.Analysis != nil {
			for i := range d.Followups {
				record := &d.Followups[i]
				if record.AnalysisID == d.Run.Analysis.ID && record.Route == issue.Route && (issue.Latest == nil || record.CreatedAt.After(issue.Latest.CreatedAt) || (record.CreatedAt.Equal(issue.Latest.CreatedAt) && record.ID > issue.Latest.ID)) {
					issue.Latest = record
				}
			}
		}
		if issue.Latest != nil && issue.Latest.Action != "reopened" {
			issue.State = "closed"
		}
		states = append(states, issue)
	}
	return states
}

func (s *Service) AddFollowup(ctx context.Context, tenant uint64, runID string, input FollowupInput) (Followup, error) {
	caller := types.CallerFromContext(ctx)
	principal, present := types.PrincipalFromContext(ctx)
	if tenant == 0 || caller.TenantID != tenant || caller.UserID == "" || !caller.Role.HasPermission(types.TenantRoleAdmin) || (present && principal.Type != types.PrincipalWebUser) {
		return Followup{}, ErrForbidden
	}
	input.Reason = strings.TrimSpace(input.Reason)
	if input.AnalysisID == "" || input.Reason == "" || len(input.Reason) > 4000 || (input.Route != "source_verification" && input.Route != "technical_followup") {
		return Followup{}, ErrInvalid
	}
	switch input.Action {
	case "resolved", "accepted_limitation", "not_required", "reopened":
	default:
		return Followup{}, ErrInvalid
	}
	var result Followup
	err := s.repo.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockRun(tx, tenant, runID); err != nil {
			return err
		}
		// Re-check access and current version under the same parent lock used by
		// Retry and SaveAnalysis; requests cannot race a new diagnosis into closure.
		scoped := &Service{repo: NewRepository(tx), access: s.access}
		d, err := scoped.loadDetail(ctx, tenant, runID, false)
		if err != nil {
			return err
		}
		if d.Run.Analysis == nil || d.Run.Analysis.ID != input.AnalysisID || d.PendingTasks > 0 {
			return ErrStale
		}
		var selected *FollowupState
		for _, state := range d.Run.Followups {
			if state.Route == input.Route {
				selected = &state
				break
			}
		}
		if selected == nil {
			return ErrInvalid
		}
		previousID := ""
		if selected.Latest != nil {
			previousID = selected.Latest.ID
		}
		if input.PreviousID != previousID {
			return ErrStale
		}
		if input.Action == "reopened" && selected.State != "closed" {
			return ErrInvalid
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		if selected.Latest != nil && !now.After(selected.Latest.CreatedAt) {
			now = selected.Latest.CreatedAt.Add(time.Microsecond)
		}
		result = Followup{ID: uuid.NewString(), TenantID: tenant, RunID: runID, AnalysisID: input.AnalysisID, Route: input.Route, Action: input.Action, Reason: input.Reason, ActorID: caller.UserID, PreviousID: previousID, CreatedAt: now}
		return tx.Create(&result).Error
	})
	// SQLite omits FOR UPDATE and can reject a competing writer before the
	// predecessor check. Treat that contention as a refresh conflict as well.
	if err != nil && s.repo.db.Dialector.Name() == "sqlite" {
		// Like GORM's SQLite translator, inspect the code via JSON so the
		// PostgreSQL build remains compatible with CGO disabled.
		var sqliteErr struct{ Code int }
		data, _ := json.Marshal(err)
		if json.Unmarshal(data, &sqliteErr) == nil && (sqliteErr.Code == 5 || sqliteErr.Code == 6) {
			return Followup{}, ErrStale // SQLITE_BUSY or SQLITE_LOCKED
		}
	}
	return result, err
}
