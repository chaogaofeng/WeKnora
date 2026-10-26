package knowledgeops

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/knowledgeops/capture"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
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

func TestCapturedRAGFollowUpDiagnosesCurrentSourcesWithBoundHistory(t *testing.T) {
	s, repo, _ := analysisHistoryFixture(t)
	ctx := capture.Begin(context.Background(), s, capture.Start{TenantID: 1, SessionID: "s", MessageID: "follow-up-answer", Question: "展开第三点", QAType: "rag-qa", Channel: "web", StartedAt: time.Now()})
	capture.RecordQuery(ctx, capture.QueryEvent{Kind: "rewrite", Status: "rewritten", Query: "第三个方向的详细成果"})
	capture.RecordQuery(ctx, capture.QueryEvent{Kind: "retrieval", Status: "required", Intent: "kb_search", Reason: "intent_requires_retrieval"})
	capture.RecordSources(ctx, []capture.Source{{Kind: "chunk", ChunkID: "c", KBID: "kb", KnowledgeID: "doc", Version: "1", Content: "成果是原型"}})
	capture.RecordRAGModelInput(ctx, []chat.Message{
		{Role: "user", Content: "question-old", TurnID: "old-assistant"},
		{Role: "assistant", Content: "answer-old", TurnID: "old-assistant"},
		{Role: "user", Content: `<retrieval><chunk id="c">成果是原型</chunk></retrieval>`},
	}, nil, "m", 1)
	capture.Finish(ctx, "成果是原型", nil)
	var run Run
	require.NoError(t, repo.db.Where("message_id = ?", "follow-up-answer").Take(&run).Error)
	require.Equal(t, "partial", run.CaptureStatus, "capture audit retains the historical-source limit")
	evidence, err := repo.Evidence(ctx, 1, run.ID)
	require.NoError(t, err)
	var visible []Evidence
	for _, e := range evidence {
		if e.SentToModel {
			visible = append(visible, e)
		}
	}
	require.Len(t, visible, 1)
	report := Report{Verdict: "supported", Applicability: "knowledge", Risk: "low", SourceCheck: "not_checked", Scope: "current sources", Claims: []Claim{{Text: "成果是原型", Position: "answer", Verdict: "supported", Reason: "source", EvidenceIDs: []string{visible[0].ID}}}}
	body, err := json.Marshal(report)
	require.NoError(t, err)
	m := &recoveryModel{responses: []string{string(body)}}
	s.analyzer = NewAnalyzer(func(context.Context, string) (chat.Chat, error) { return m, nil }, nil)
	require.NoError(t, s.RunOnce(ctx))
	detail, err := s.loadDetail(ctx, 1, run.ID, false)
	require.NoError(t, err)
	require.Len(t, detail.Analyses, 1)
	analysis := detail.Analyses[0]
	require.Equal(t, "succeeded", analysis.Status)
	require.Equal(t, "supported", analysis.Report.Verdict)
	require.NotEqual(t, "needs_evidence", analysis.Route)
	require.Equal(t, "complete", analysis.Report.AnalysisAudit.History.Status)
	require.Equal(t, []string{"old-assistant"}, analysis.Report.AnalysisAudit.History.TurnIDs)
	require.Equal(t, "complete", detail.Run.Coverage.ContentStatus)
	var input struct {
		ConversationContext AnalysisHistory `json:"conversation_context"`
	}
	require.NoError(t, json.Unmarshal([]byte(m.inputs[0][1].Content), &input))
	require.Equal(t, "answer-old", input.ConversationContext.Turns[0].Answer)
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

func TestCapturedRAGHistorySupplementsRemainIncomplete(t *testing.T) {
	for _, tc := range []struct{ name, supplement, gap string }{
		{"attachment", (types.MessageAttachments{{FileName: "scope.txt", Content: "仅限2025年上海地区"}}).BuildPrompt(), "history_attachment_context_omitted"},
		{"image caption", "\n\n[用户上传图片内容]\n仅限2025年上海地区", "history_image_context_omitted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, repo, _ := analysisHistoryFixture(t)
			ctx := capture.Begin(context.Background(), s, capture.Start{TenantID: 1, SessionID: "s", MessageID: "follow-up-answer", Question: "展开第三点", QAType: "rag-qa", Channel: "web", StartedAt: time.Now()})
			capture.RecordSources(ctx, []capture.Source{{Kind: "chunk", ChunkID: "c", KBID: "kb", KnowledgeID: "doc", Version: "1", Content: "成果是原型"}})
			capture.RecordRAGModelInput(ctx, []chat.Message{
				{Role: "user", Content: "question-old" + tc.supplement, TurnID: "old-assistant"},
				{Role: "assistant", Content: "answer-old", TurnID: "old-assistant"},
				{Role: "user", Content: `<retrieval><chunk id="c">成果是原型</chunk></retrieval>`},
			}, nil, "m", 1)
			capture.Finish(ctx, "成果是原型", nil)
			var run Run
			require.NoError(t, repo.db.Where("message_id = ?", "follow-up-answer").Take(&run).Error)
			history, err := s.loadAnalysisHistory(ctx, run)
			require.NoError(t, err)
			require.Equal(t, "partial", history.Status, "matching turn IDs cannot recover historical supplements")
			require.Equal(t, []string{tc.gap}, history.ContextGaps)
			require.Len(t, history.Turns, 1, "authorized question and answer remain useful context")
			require.Equal(t, "question-old", history.Turns[0].Question)
			require.Contains(t, run.Gaps, tc.gap)
			run.AnalysisHistory = history
			require.Equal(t, "partial", withResolvedHistoryCapture(run).CaptureStatus)
			evidence, err := repo.Evidence(ctx, 1, run.ID)
			require.NoError(t, err)
			var visible []Evidence
			for _, e := range evidence {
				if e.SentToModel {
					visible = append(visible, e)
				}
			}
			require.Len(t, visible, 1)
			report := Report{Verdict: "supported", Applicability: "knowledge", Risk: "low", SourceCheck: "not_checked", Scope: "current sources", Claims: []Claim{{Text: "成果是原型", Position: "answer", Verdict: "supported", Reason: "source", EvidenceIDs: []string{visible[0].ID}}}}
			body, err := json.Marshal(report)
			require.NoError(t, err)
			m := &recoveryModel{responses: []string{string(body)}}
			s.analyzer = NewAnalyzer(func(context.Context, string) (chat.Chat, error) { return m, nil }, nil)
			require.NoError(t, s.RunOnce(ctx))
			detail, err := s.loadDetail(ctx, 1, run.ID, false)
			require.NoError(t, err)
			require.Len(t, detail.Analyses, 1)
			analysis := detail.Analyses[0]
			require.Equal(t, "succeeded", analysis.Status)
			require.Equal(t, "partial", analysis.Report.Verdict)
			require.NotEqual(t, "auto_record", analysis.Route)
			require.Equal(t, "partial", analysis.Report.AnalysisAudit.History.Status)
			require.Equal(t, []string{tc.gap}, analysis.Report.AnalysisAudit.History.ContextGaps)
			settings := DefaultSettings(1)
			settings.AutoRecordEnabled, settings.CalibrationNote = true, "calibrated"
			settings = withTestCalibration(settings)
			route, _ := Triage(run, analysis.Report, settings)
			require.Equal(t, "needs_evidence", route)
			require.Equal(t, "complete", detail.Run.Coverage.ContentStatus, "current knowledge bodies were saved in full")
			require.Contains(t, detail.Run.Coverage.ContextGaps, tc.gap)
			stored, err := json.Marshal(detail)
			require.NoError(t, err)
			require.NotContains(t, string(stored), "仅限2025年上海地区", "unbound historical supplements are not saved as knowledge evidence")
			var input struct {
				History AnalysisHistory `json:"conversation_context"`
			}
			require.NoError(t, json.Unmarshal([]byte(m.inputs[0][1].Content), &input))
			require.Equal(t, "partial", input.History.Status)
			require.Equal(t, []string{tc.gap}, input.History.ContextGaps)
		})
	}
}

func TestDiagnosisHistoryRetainsPriorSupplementGaps(t *testing.T) {
	for _, gap := range []string{"attachment_source_permissions_unbound", "image_context_omitted"} {
		t.Run(gap, func(t *testing.T) {
			s, repo, run := analysisHistoryFixture(t)
			run.HistoryContext = &capture.HistorySnapshot{HistoryInput: capture.HistoryInput{Turns: 1, IDsKnown: true, TurnIDs: []string{"old-assistant"}}}
			run.CaptureStatus, run.Gaps = "partial", []string{"history_source_permissions_unbound"}
			gaps, err := json.Marshal([]string{gap})
			require.NoError(t, err)
			require.NoError(t, repo.db.Model(&Run{}).Where("id = ?", "old").Update("gaps", string(gaps)).Error)
			history, err := s.loadAnalysisHistory(context.Background(), run)
			require.NoError(t, err)
			require.Equal(t, "partial", history.Status, "earlier captures must retain known historical omissions")
			require.Len(t, history.ContextGaps, 1)
			require.Contains(t, []string{capture.HistoryAttachmentContextOmitted, capture.HistoryImageContextOmitted}, history.ContextGaps[0])
			run.AnalysisHistory = history
			report := Report{Verdict: "supported", Applicability: "knowledge"}
			applySupportPolicy(run, &report)
			require.Equal(t, "partial", report.Verdict)
			// Neither gaps nor history text may be read from an inaccessible run.
			s.access = func(context.Context, Run, []Evidence) error { return ErrForbidden }
			history, err = s.loadAnalysisHistory(context.Background(), run)
			require.NoError(t, err)
			require.Empty(t, history.Turns)
			require.Empty(t, history.ContextGaps)
		})
	}
}

func TestNewCaptureUsesRenderedHistorySupplements(t *testing.T) {
	for _, gap := range []string{"attachment_source_permissions_unbound", "image_context_omitted"} {
		t.Run(gap, func(t *testing.T) {
			s, repo, _ := analysisHistoryFixture(t)
			gaps, err := json.Marshal([]string{gap})
			require.NoError(t, err)
			require.NoError(t, repo.db.Model(&Run{}).Where("id = ?", "old").Update("gaps", string(gaps)).Error)
			ctx := capture.Begin(context.Background(), s, capture.Start{TenantID: 1, SessionID: "s", MessageID: "follow-up-answer", Question: "展开第三点", StartedAt: time.Now()})
			// The original turn may have used an image without a caption. The
			// current answering input contains only plain history, with no supplement.
			capture.RecordRAGModelInput(ctx, []chat.Message{
				{Role: "user", Content: "question-old", TurnID: "old-assistant"},
				{Role: "assistant", Content: "answer-old", TurnID: "old-assistant"},
				{Role: "user", Content: "展开第三点"},
			}, nil, "m", 1)
			capture.Finish(ctx, "根据前文进一步说明", nil)
			var run Run
			require.NoError(t, repo.db.Where("message_id = ?", "follow-up-answer").Take(&run).Error)
			history, err := s.loadAnalysisHistory(ctx, run)
			require.NoError(t, err)
			require.Equal(t, "complete", history.Status, "older input omissions do not imply a loss in the actual current history input")
			require.Empty(t, history.ContextGaps)
		})
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
