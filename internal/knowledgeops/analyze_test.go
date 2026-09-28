package knowledgeops

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type fakeChat struct {
	payload string
	err     error
	calls   int
}

func (f *fakeChat) Chat(context.Context, []chat.Message, *chat.ChatOptions) (*types.ChatResponse, error) {
	f.calls++
	return &types.ChatResponse{Content: f.payload}, f.err
}
func (f *fakeChat) ChatStream(context.Context, []chat.Message, *chat.ChatOptions) (<-chan types.StreamResponse, error) {
	panic("unexpected stream")
}
func (f *fakeChat) GetModelName() string { return "test-model" }
func (f *fakeChat) GetModelID() string   { return "test-id" }
func TestTriageNeverLetsHighRiskOrPartialAutoRecord(t *testing.T) {
	s := DefaultSettings(1)
	s.AutoRecordEnabled = true
	s.CalibrationNote = "test calibration"
	s.SamplePercent = 0
	report := Report{Verdict: "supported", SourceCheck: "consistent", Risk: "low", Claims: []Claim{{Verdict: "supported"}}}
	run := Run{ID: "r", QAType: "rag-qa", Channel: "web", Question: "融资额度上限是多少", CaptureStatus: "complete", Status: "completed"}
	route, _ := Triage(run, report, s)
	require.Equal(t, "human_review", route)
	run.Question = "普通流程说明"
	run.CaptureStatus = "partial"
	route, _ = Triage(run, report, s)
	require.Equal(t, "needs_evidence", route)
	run.CaptureStatus = "complete"
	route, _ = Triage(run, report, s)
	require.Equal(t, "auto_record", route)
	s.AutoRecordEnabled = false
	route, _ = Triage(run, report, s)
	require.Equal(t, "human_review", route)
}
func TestAnalysisRejectsPostHocSupportAndMissingCitations(t *testing.T) {
	payload := `{"verdict":"supported","claims":[{"text":"A","position":"answer","verdict":"supported","evidence_ids":["later"],"reason":"x"}],"facts":[],"causes":[],"unknowns":[],"review_questions":[],"source_check":"consistent","risk":"low","scope":"x"}`
	m := &fakeChat{payload: payload}
	a := NewAnalyzer(func(context.Context, string) (chat.Chat, error) { return m, nil }, nil)
	_, err := a.Analyze(context.Background(), Run{Question: "Q", Answer: "A", Status: "completed", CaptureStatus: "complete"}, []Evidence{{ID: "later", Content: "A", PostHoc: true}}, DefaultSettings(1))
	require.Error(t, err)
	require.Equal(t, 1, m.calls)
}
func TestInvalidModelJSONDoesNotBecomeSupported(t *testing.T) {
	m := &fakeChat{payload: `{"verdict":"supported"}`}
	a := NewAnalyzer(func(context.Context, string) (chat.Chat, error) { return m, nil }, nil)
	_, err := a.Analyze(context.Background(), Run{Answer: "A"}, nil, DefaultSettings(1))
	require.Error(t, err)
}

func TestIncompleteCaptureCannotHaveUnqualifiedSupportedVerdict(t *testing.T) {
	m := &fakeChat{payload: `{"verdict":"supported","claims":[{"text":"A","position":"answer","verdict":"supported","evidence_ids":["captured"],"reason":"A"}],"facts":[],"causes":[],"unknowns":[],"review_questions":[],"source_check":"not_checked","risk":"low","scope":"only captured"}`}
	a := NewAnalyzer(func(context.Context, string) (chat.Chat, error) { return m, nil }, nil)
	result, err := a.Analyze(context.Background(), Run{Status: "completed", CaptureStatus: "partial"}, []Evidence{{ID: "captured", Kind: "wiki", Content: "A", SentToModel: true}}, DefaultSettings(1))
	require.NoError(t, err)
	require.Equal(t, "partial", result.Report.Verdict)
	require.Equal(t, "needs_evidence", result.Route)
	require.NotEmpty(t, result.Report.Unknowns)
}

func TestNotApplicableCannotHideContradictedClaim(t *testing.T) {
	r := Report{Verdict: "not_applicable", Risk: "low", Scope: "all", SourceCheck: "not_checked", Claims: []Claim{{Text: "wrong", Position: "answer", Verdict: "contradicted", EvidenceIDs: []string{"e"}, Reason: "conflicts"}}}
	require.Error(t, validateReport(r, []Evidence{{ID: "e", Kind: "chunk", Content: "source", SentToModel: true}}))
}

func TestUnknownModeCannotAutoRecord(t *testing.T) {
	s := DefaultSettings(1)
	s.AutoRecordEnabled = true
	s.CalibrationNote = "calibrated"
	s.SamplePercent = 0
	r := Run{ID: "unknown", Status: "completed", CaptureStatus: "complete", QAType: "unknown", Channel: "web"}
	route, _ := Triage(r, Report{Verdict: "supported", Risk: "low", SourceCheck: "not_checked"}, s)
	require.Equal(t, "needs_evidence", route)
}
