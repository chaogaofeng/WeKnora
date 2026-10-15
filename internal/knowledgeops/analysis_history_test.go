package knowledgeops

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/knowledgeops/capture"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/stretchr/testify/require"
)

func analysisHistoryFixture(t *testing.T) (*Service, *Repository, Run) {
	t.Helper()
	s, repo := enabledTestService(t)
	require.NoError(t, repo.db.Exec(`CREATE TABLE messages (id TEXT PRIMARY KEY, session_id TEXT, request_id TEXT, role TEXT, created_at DATETIME, deleted_at DATETIME)`).Error)
	now := time.Now().UTC().Add(-time.Minute)
	for i, id := range []string{"old", "private", "deleted", "unrecorded", "current"} {
		at := now.Add(time.Duration(i) * time.Second)
		r := Run{ID: id, TenantID: 1, SessionID: "s", MessageID: id + "-assistant", RequestID: id, Question: "question-" + id, Answer: "answer-" + id, StartedAt: at, FinishedAt: &at, Status: "completed", CaptureStatus: "missing"}
		require.NoError(t, repo.CreateRun(context.Background(), &r))
		require.NoError(t, repo.db.Exec(`INSERT INTO messages VALUES (?, 's', ?, 'user', ?, NULL)`, id+"-user", id, at).Error)
		require.NoError(t, repo.db.Exec(`INSERT INTO messages VALUES (?, 's', ?, 'assistant', ?, NULL)`, r.MessageID, id, at).Error)
	}
	require.NoError(t, repo.db.Exec(`UPDATE messages SET deleted_at = ? WHERE id = 'deleted-user'`, now).Error)
	s.access = func(_ context.Context, r Run, _ []Evidence) error {
		if r.ID == "private" {
			return ErrForbidden
		}
		return nil
	}
	run, err := repo.Run(context.Background(), 1, "current")
	require.NoError(t, err)
	run.HistoryContext = &capture.HistorySnapshot{HistoryInput: capture.HistoryInput{Turns: 3, IDsKnown: true, TurnIDs: []string{"old-assistant", "private-assistant", "deleted-assistant"}}}
	history, err := json.Marshal(run.HistoryContext)
	require.NoError(t, err)
	require.NoError(t, repo.db.Model(&Run{}).Where("id = ?", run.ID).Update("history_context", string(history)).Error)
	return s, repo, run
}

func TestDiagnosisHistoryUsesOnlyRecordedAuthorizedTurnsAsContext(t *testing.T) {
	s, repo, run := analysisHistoryFixture(t)
	m := &recoveryModel{responses: []string{noSourceReport}}
	s.analyzer = NewAnalyzer(func(context.Context, string) (chat.Chat, error) { return m, nil }, nil)
	require.NoError(t, repo.Enqueue(context.Background(), run, 1))
	require.NoError(t, s.RunOnce(context.Background()))
	require.Len(t, m.inputs, 1)
	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(m.inputs[0][1].Content), &payload))
	require.NotNil(t, payload["conversation_context"], "recorded history must reach diagnosis separately from source evidence")
	text, _ := json.Marshal(payload["conversation_context"])
	require.Contains(t, string(text), "answer-old")
	for _, omitted := range []string{"answer-private", "answer-deleted", "answer-unrecorded", "answer-current"} {
		require.NotContains(t, string(text), omitted)
	}
	require.Empty(t, payload["evidence"])
	var evidenceCount int64
	require.NoError(t, repo.db.Model(&Evidence{}).Where("run_id = ?", run.ID).Count(&evidenceCount).Error)
	require.Zero(t, evidenceCount)
	// A stored report derived from past text must remain subject to that text's permissions.
	s.access = func(_ context.Context, r Run, _ []Evidence) error {
		if r.ID == "old" {
			return ErrForbidden
		}
		return nil
	}
	_, err := s.loadDetail(context.Background(), 1, run.ID, false)
	require.ErrorIs(t, err, ErrForbidden)
}

func TestDiagnosisHistoryDoesNotGuessUnknownOrCompactedTurns(t *testing.T) {
	s, _, run := analysisHistoryFixture(t)
	for _, h := range []*capture.HistorySnapshot{
		nil,
		{HistoryInput: capture.HistoryInput{Turns: 3}},
		{HistoryInput: capture.HistoryInput{Turns: 1, IDsKnown: true, TurnIDs: []string{"old-assistant"}, Compacted: true}},
		{HistoryInput: capture.HistoryInput{Turns: 1, IDsKnown: true, TurnIDs: []string{"old-assistant"}}, EverCompacted: true},
		{HistoryInput: capture.HistoryInput{Turns: 2, IDsKnown: true, TurnIDs: []string{"old-assistant"}}},
	} {
		run.HistoryContext = h
		out, err := s.loadAnalysisHistory(context.Background(), run)
		require.NoError(t, err)
		require.Empty(t, out.Turns)
		require.Equal(t, "unavailable", out.Status)
	}
}

func TestConversationComparisonCannotSubstituteForSourceEvidence(t *testing.T) {
	for _, tc := range []struct{ status, id, historyStatus, wantCode, wantRoute string }{
		{"consistent", "old", "complete", "", "needs_evidence"},
		{"inconsistent", "old", "complete", "", "human_review"},
		{"consistent", "invented", "complete", "invalid_analysis_report", ""},
		{"consistent", "old", "partial", "invalid_analysis_report", ""},
	} {
		t.Run(tc.status+tc.id+tc.historyStatus, func(t *testing.T) {
			var report Report
			require.NoError(t, json.Unmarshal([]byte(noSourceReport), &report))
			report.ConversationCheck = &ConversationCheck{Status: tc.status, Reason: "compare only", TurnIDs: []string{tc.id}}
			body, _ := json.Marshal(report)
			m := &recoveryModel{responses: []string{string(body)}}
			a := NewAnalyzer(func(context.Context, string) (chat.Chat, error) { return m, nil }, nil)
			run := Run{Status: "completed", CaptureStatus: "missing", AnalysisHistory: &AnalysisHistory{Status: tc.historyStatus, Turns: []AnalysisHistoryTurn{{TurnID: "old", Answer: "提高解决率"}}}}
			out, err := a.Analyze(context.Background(), run, nil, DefaultSettings(1))
			if tc.wantCode != "" {
				require.Equal(t, tc.wantCode, analysisErrorCode(err))
				return
			}
			require.NoError(t, err)
			require.Equal(t, "unverifiable", out.Report.Verdict)
			require.Equal(t, tc.wantRoute, out.Route)
		})
	}
}

func TestDiagnosisHistoryExcludesOtherTenantsSessionsAndLaterTurns(t *testing.T) {
	for _, change := range []map[string]any{
		{"tenant_id": 2}, {"session_id": "other"}, {"started_at": time.Now().UTC().Add(time.Hour)},
		{"answer": string(make([]byte, analysisHistoryCharLimit+1))},
	} {
		s, repo, run := analysisHistoryFixture(t)
		require.NoError(t, repo.db.Model(&Run{}).Where("id = ?", "old").Updates(change).Error)
		out, err := s.loadAnalysisHistory(context.Background(), run)
		require.NoError(t, err)
		require.Empty(t, out.Turns)
		require.Equal(t, "partial", out.Status)
	}
}

func TestDiagnosisHistoryDoesNotExposeDeletedQuestionViaSteeringMessage(t *testing.T) {
	s, repo, run := analysisHistoryFixture(t)
	require.NoError(t, repo.db.Exec(`INSERT INTO messages VALUES ('old-steer', 's', 'old', 'user', ?, NULL)`, time.Now().UTC()).Error)
	require.NoError(t, repo.db.Exec(`UPDATE messages SET deleted_at = ? WHERE id = 'old-user'`, time.Now().UTC()).Error)
	out, err := s.loadAnalysisHistory(context.Background(), run)
	require.NoError(t, err)
	require.Empty(t, out.Turns)
	audit := &AnalysisAudit{History: &AnalysisHistoryAudit{RunIDs: []string{"old"}, TurnIDs: []string{"old-assistant"}}}
	require.ErrorIs(t, s.checkAnalysisHistoryAccess(context.Background(), run, []Analysis{{Report: Report{AnalysisAudit: audit}}}), ErrForbidden)
}

func TestDiagnosisHistoryExcludesTruncatedQuestion(t *testing.T) {
	s, repo, run := analysisHistoryFixture(t)
	require.NoError(t, repo.db.Model(&Run{}).Where("id = ?", "old").Update("gaps", `["question_capture_budget"]`).Error)
	out, err := s.loadAnalysisHistory(context.Background(), run)
	require.NoError(t, err)
	require.Empty(t, out.Turns)
	require.Equal(t, "partial", out.Status)
}
