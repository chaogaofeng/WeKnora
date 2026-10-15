package knowledgeops

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type recoveryModel struct {
	fakeChat
	responses []string
	inputs    [][]chat.Message
}

func (m *recoveryModel) Chat(_ context.Context, messages []chat.Message, _ *chat.ChatOptions) (*types.ChatResponse, error) {
	m.inputs = append(m.inputs, append([]chat.Message(nil), messages...))
	if m.err != nil {
		return nil, m.err
	}
	response := m.responses[min(len(m.inputs)-1, len(m.responses)-1)]
	return &types.ChatResponse{Content: response, Usage: types.TokenUsage{PromptTokens: 100, CompletionTokens: 50}}, nil
}

const noSourceReport = `{"applicability":"knowledge","applicability_reason":"回答包含业务事实，但缺少原文","verdict":"unverifiable","claims":[{"text":"提高解决率","position":"answer","verdict":"unverifiable","evidence_ids":[],"reason":"本轮没有可核验原文"}],"facts":[],"causes":[],"unknowns":["原文未采集"],"review_questions":[],"source_check":"not_checked","risk":"low","scope":"只核查已提供材料"}`

func TestAnalysisCorrectsInvalidEvidenceOnceWithoutInventingSupport(t *testing.T) {
	m := &recoveryModel{responses: []string{
		`{"verdict":"supported","claims":[{"text":"提高解决率","position":"answer","verdict":"supported","evidence_ids":["d29"],"reason":"回答提到了文档"}],"source_check":"not_checked","risk":"low","scope":"回答"}`,
		noSourceReport,
	}}
	a := NewAnalyzer(func(context.Context, string) (chat.Chat, error) { return m, nil }, nil)
	out, err := a.Analyze(context.Background(), Run{Status: "completed", CaptureStatus: "missing", Question: "你刚才说的啥了", Answer: "提高解决率"}, nil, DefaultSettings(1))
	require.NoError(t, err)
	require.Len(t, m.inputs, 2)
	require.Equal(t, m.inputs[0][1].Content, m.inputs[1][1].Content, "retry must retain the original evidence input")
	require.Equal(t, "unverifiable", out.Report.Verdict)
	require.Equal(t, "needs_evidence", out.Route)
	require.Equal(t, 200, out.InputTokens)
	require.Equal(t, 100, out.OutputTokens)
	var wire map[string]any
	b, _ := json.Marshal(out.Report)
	require.NoError(t, json.Unmarshal(b, &wire))
	audit := wire["analysis_audit"].(map[string]any)
	require.EqualValues(t, 2, audit["attempts"])
	require.Len(t, audit["validation_issues"], 1)
}

func TestAnalysisRecoveryIsBoundedAndKeepsSafeFailureDetail(t *testing.T) {
	m := &recoveryModel{responses: []string{`{"verdict":"supported","claims":[{"text":"A","position":"answer","verdict":"supported","evidence_ids":[],"reason":"x"}],"source_check":"not_checked","risk":"low","scope":"x"}`}}
	a := NewAnalyzer(func(context.Context, string) (chat.Chat, error) { return m, nil }, nil)
	out, err := a.Analyze(context.Background(), Run{Status: "completed"}, nil, DefaultSettings(1))
	require.Equal(t, "invalid_analysis_evidence", analysisErrorCode(err))
	require.Len(t, m.inputs, 2)
	b, _ := json.Marshal(out.Report)
	require.Contains(t, string(b), "claim_missing_evidence")
	// Network/provider failures do not get an unbounded retry or expose secrets.
	m = &recoveryModel{fakeChat: fakeChat{err: errors.New("api_key=private-value")}}
	a = NewAnalyzer(func(context.Context, string) (chat.Chat, error) { return m, nil }, nil)
	out, err = a.Analyze(context.Background(), Run{}, nil, DefaultSettings(1))
	require.Equal(t, "analysis_model_failed", analysisErrorCode(err))
	require.Len(t, m.inputs, 1)
	b, _ = json.Marshal(out.Report)
	require.NotContains(t, string(b), "private-value")
}

func TestFailedCorrectionPersistsValidationAuditWithoutModelReport(t *testing.T) {
	s, repo, run := analysisHistoryFixture(t)
	m := &recoveryModel{responses: []string{`{"verdict":"supported","claims":[{"text":"invented-secret","position":"answer","verdict":"supported","evidence_ids":["not-captured"],"reason":"x"}],"source_check":"not_checked","risk":"low","scope":"x","analysis_audit":{"attempts":99,"history":{"run_ids":["forged"],"turn_ids":["forged"]}}}`}}
	s.analyzer = NewAnalyzer(func(context.Context, string) (chat.Chat, error) { return m, nil }, nil)
	require.NoError(t, repo.Enqueue(context.Background(), run, 1))
	require.NoError(t, s.RunOnce(context.Background()))
	analyses, err := repo.Analyses(context.Background(), 1, run.ID)
	require.NoError(t, err)
	require.Len(t, analyses, 1)
	a := analyses[0]
	require.Equal(t, "failed", a.Status)
	require.Equal(t, "invalid_analysis_evidence", a.Error)
	require.Equal(t, 2, a.Report.AnalysisAudit.Attempts)
	require.Len(t, a.Report.AnalysisAudit.ValidationIssues, 2)
	require.Equal(t, []string{"old"}, a.Report.AnalysisAudit.History.RunIDs)
	body, _ := json.Marshal(a.Report)
	require.NotContains(t, string(body), "invented-secret")
	require.NotContains(t, string(body), "forged")
}
