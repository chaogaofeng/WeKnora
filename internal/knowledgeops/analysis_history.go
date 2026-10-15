package knowledgeops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"gorm.io/gorm"
)

const analysisHistoryTurnLimit = 10
const analysisHistoryCharLimit = 24000

// Conversation text is context for checking a recap, never source evidence.
// IDs must come from the captured model input; recent session messages alone
// do not establish what the answering model actually saw.
type AnalysisHistory struct {
	Status string                `json:"status"`
	Turns  []AnalysisHistoryTurn `json:"turns"`
}
type AnalysisHistoryTurn struct {
	TurnID   string `json:"turn_id"`
	RunID    string `json:"run_id"`
	Question string `json:"question"`
	Answer   string `json:"answer"`
}
type ConversationCheck struct {
	Status  string   `json:"status"`
	Reason  string   `json:"reason"`
	TurnIDs []string `json:"turn_ids"`
}
type AnalysisHistoryAudit struct {
	Status  string   `json:"status"`
	RunIDs  []string `json:"run_ids"`
	TurnIDs []string `json:"turn_ids"`
}

func (s *Service) analysisHistoryRun(ctx context.Context, current Run, turnID string) (Run, error) {
	// Captured runs do not bind a user-message ID. Multiple user messages
	// (e.g. steering) make that binding ambiguous, including after deletion.
	var matches []Run
	err := s.repo.db.WithContext(ctx).Model(&Run{}).Select("knowledge_ops_runs.*").
		Joins("JOIN messages AS answer ON answer.id = knowledge_ops_runs.message_id AND answer.session_id = knowledge_ops_runs.session_id").
		Where("knowledge_ops_runs.tenant_id = ? AND knowledge_ops_runs.session_id = ? AND knowledge_ops_runs.started_at < ? AND knowledge_ops_runs.status = ?", current.TenantID, current.SessionID, current.StartedAt, "completed").
		Where("answer.id = ? AND answer.role = ? AND answer.deleted_at IS NULL", turnID, "assistant").
		Where("(SELECT COUNT(*) FROM messages AS question WHERE question.session_id = answer.session_id AND question.request_id = answer.request_id AND question.role = 'user') = 1").
		Where("EXISTS (SELECT 1 FROM messages AS question WHERE question.session_id = answer.session_id AND question.request_id = answer.request_id AND question.role = 'user' AND question.deleted_at IS NULL)").
		Limit(2).Find(&matches).Error
	if err != nil {
		return Run{}, err
	}
	if len(matches) != 1 || s.access == nil {
		return Run{}, ErrForbidden
	}
	run := matches[0]
	evidence, err := s.repo.Evidence(ctx, run.TenantID, run.ID)
	if err != nil {
		return Run{}, err
	}
	if err := s.access(ctx, run, evidence); err != nil {
		return Run{}, ErrForbidden
	}
	return run, nil
}

func (s *Service) loadAnalysisHistory(ctx context.Context, run Run) (*AnalysisHistory, error) {
	h := run.HistoryContext
	out := &AnalysisHistory{Status: "unavailable", Turns: []AnalysisHistoryTurn{}}
	if h == nil {
		return out, nil
	}
	if h.Turns == 0 && !h.Compacted && !h.EverCompacted {
		out.Status = "not_used"
		return out, nil
	}
	if !h.IDsKnown || h.Compacted || h.EverCompacted || len(h.TurnIDs) != h.Turns || run.SessionID == "" {
		return out, nil
	}
	out.Status = "complete"
	remaining := analysisHistoryCharLimit
	seen := map[string]bool{}
	for _, id := range h.TurnIDs {
		if seen[id] || id == "" || len(out.Turns) >= analysisHistoryTurnLimit {
			out.Status = "partial"
			continue
		}
		seen[id] = true
		previous, err := s.analysisHistoryRun(ctx, run, id)
		if errors.Is(err, ErrForbidden) || errors.Is(err, gorm.ErrRecordNotFound) {
			out.Status = "partial"
			continue
		}
		if err != nil {
			return nil, err
		}
		// Omit an oversized turn instead of silently showing a shortened recap.
		chars := len([]rune(previous.Question)) + len([]rune(previous.Answer))
		if chars > remaining || strings.TrimSpace(previous.Answer) == "" || slices.Contains(previous.Gaps, "answer_capture_budget") || slices.Contains(previous.Gaps, "question_capture_budget") {
			out.Status = "partial"
			continue
		}
		remaining -= chars
		out.Turns = append(out.Turns, AnalysisHistoryTurn{TurnID: id, RunID: previous.ID, Question: previous.Question, Answer: previous.Answer})
	}
	return out, nil
}

func historyAudit(history *AnalysisHistory) *AnalysisHistoryAudit {
	if history == nil {
		return nil
	}
	audit := &AnalysisHistoryAudit{Status: history.Status}
	for _, turn := range history.Turns {
		audit.RunIDs = append(audit.RunIDs, turn.RunID)
		audit.TurnIDs = append(audit.TurnIDs, turn.TurnID)
	}
	return audit
}

// Reports may quote the supplied history. Re-check those dependencies on every
// read, even when only the latest analysis/summary is requested.
func (s *Service) checkAnalysisHistoryAccess(ctx context.Context, run Run, analyses []Analysis) error {
	checked := map[string]string{}
	for _, a := range analyses {
		if a.Report.AnalysisAudit == nil || a.Report.AnalysisAudit.History == nil {
			continue
		}
		h := a.Report.AnalysisAudit.History
		if len(h.TurnIDs) != len(h.RunIDs) || len(h.TurnIDs) > analysisHistoryTurnLimit {
			return ErrForbidden
		}
		for i, turnID := range h.TurnIDs {
			if checked[turnID] == h.RunIDs[i] && h.RunIDs[i] != "" {
				continue
			}
			previous, err := s.analysisHistoryRun(ctx, run, turnID)
			if err != nil || previous.ID != h.RunIDs[i] {
				return ErrForbidden
			}
			checked[turnID] = previous.ID
		}
	}
	return nil
}

func validateConversationCheck(check *ConversationCheck, history *AnalysisHistory) error {
	if check == nil { // Reports from older policies remain valid.
		return nil
	}
	if strings.TrimSpace(check.Reason) == "" {
		return fmt.Errorf("conversation check missing reason")
	}
	switch check.Status {
	case "consistent", "inconsistent":
		if len(check.TurnIDs) == 0 {
			return fmt.Errorf("conversation check missing history")
		}
	case "unverifiable", "not_applicable":
	default:
		return fmt.Errorf("invalid conversation check")
	}
	for _, id := range check.TurnIDs {
		if history == nil || !slices.ContainsFunc(history.Turns, func(t AnalysisHistoryTurn) bool { return t.TurnID == id }) {
			return fmt.Errorf("conversation check references unavailable history")
		}
	}
	if check.Status == "consistent" && (history == nil || history.Status != "complete") {
		return fmt.Errorf("conversation consistency requires complete history")
	}
	return nil
}
