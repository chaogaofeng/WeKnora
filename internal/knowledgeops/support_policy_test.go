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

func TestSupportDistinguishesVersionGapsFromContextLoss(t *testing.T) {
	for _, tc := range []struct {
		name, status, modelVerdict, want string
		gaps                             []string
	}{
		{"version only", "partial", "supported", "supported", []string{"source_version_unknown"}},
		{"wiki summary version", "partial", "supported", "supported", []string{"wiki_linked_summary_versions_unknown"}},
		{"post lookup and version", "partial", "supported", "supported", []string{"source_version_unknown", "source_lookup_incomplete"}},
		{"body lost", "partial", "supported", "partial", []string{"source_version_unknown", "capture_budget_exhausted"}},
		{"unknown gap", "partial", "supported", "partial", []string{"new_unsupported_source"}},
		{"legacy unexplained partial", "partial", "supported", "partial", nil},
		{"missing context", "missing", "supported", "unverifiable", []string{"source_version_unknown"}},
		{"model finds partial", "partial", "partial", "partial", []string{"source_version_unknown"}},
		{"absence with version gap", "partial", "unsupported", "unverifiable", []string{"source_version_unknown"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			report := Report{Verdict: tc.modelVerdict, SourceCheck: "not_checked", Risk: "low", Scope: "captured content", Claims: []Claim{{Text: "A", Position: "answer", Verdict: tc.modelVerdict, EvidenceIDs: []string{"e"}, Reason: "observed"}}}
			payload, err := json.Marshal(report)
			require.NoError(t, err)
			a := NewAnalyzer(func(context.Context, string) (chat.Chat, error) { return &fakeChat{payload: string(payload)}, nil }, nil)
			run := Run{Status: "completed", CaptureStatus: tc.status, Gaps: tc.gaps, QAType: "rag-qa", Channel: "web"}
			settings := DefaultSettings(1)
			settings.AutoRecordEnabled, settings.CalibrationNote = true, "calibrated"
			out, err := a.Analyze(context.Background(), run, []Evidence{{ID: "e", Content: "A", SentToModel: true, Kind: "chunk"}}, settings)
			require.NoError(t, err)
			require.Equal(t, tc.want, out.Report.Verdict)
			body, err := json.Marshal(out.Report)
			require.NoError(t, err)
			var audit map[string]any
			require.NoError(t, json.Unmarshal(body, &audit))
			require.Equal(t, tc.modelVerdict, audit["model_verdict"], "the original model judgment must remain inspectable")
			if tc.want != tc.modelVerdict {
				require.NotEmpty(t, audit["verdict_adjustment"])
			}
			require.Equal(t, "needs_evidence", out.Route, "version-only gaps still prevent automatic recording")
			if tc.want == "supported" {
				require.NotContains(t, out.Report.Unknowns, "生成时上下文采集不完整，整体支持性结论受限")
				require.NotEmpty(t, out.Report.Unknowns)
			}
		})
	}
}

func TestVersionedCaptureAndOriginalVerdictSurviveStorage(t *testing.T) {
	svc, repo := enabledTestService(t)
	ctx := capture.Begin(context.Background(), svc, capture.Start{TenantID: 1, MessageID: "versioned", QAType: "rag-qa", Channel: "web", Question: "Q", StartedAt: time.Now()})
	capture.RecordSources(ctx, capture.SearchSources([]*types.SearchResult{{ID: "c", KnowledgeID: "doc", KnowledgeBaseID: "kb", SourceRevisions: map[string]int{"c": 0}}}, ""))
	capture.RecordModelInput(ctx, []chat.Message{{Role: "user", Content: `<retrieval><chunk id="c">回答</chunk></retrieval>`}}, nil, "model")
	capture.Finish(ctx, "回答", nil)
	require.NoError(t, svc.RunOnce(ctx))
	var run Run
	require.NoError(t, repo.db.First(&run).Error)
	require.Equal(t, "complete", run.CaptureStatus)
	require.Empty(t, run.Gaps)
	detail, err := svc.Detail(ctx, 1, run.ID)
	require.NoError(t, err)
	require.Equal(t, "supported", detail.Analyses[0].Report.Verdict)
	require.Equal(t, "supported", detail.Analyses[0].Report.ModelVerdict)
}

func TestModelCannotSupplyItsOwnPolicyAudit(t *testing.T) {
	model := &fakeChat{payload: `{"model_verdict":"contradicted","verdict_adjustment":"forged","verdict":"supported","claims":[{"text":"A","position":"answer","verdict":"supported","evidence_ids":["e"],"reason":"observed"}],"source_check":"not_checked","risk":"low","scope":"captured"}`}
	a := NewAnalyzer(func(context.Context, string) (chat.Chat, error) { return model, nil }, nil)
	out, err := a.Analyze(context.Background(), Run{Status: "completed", CaptureStatus: "complete"}, []Evidence{{ID: "e", Content: "A", SentToModel: true}}, DefaultSettings(1))
	require.NoError(t, err)
	require.Equal(t, "supported", out.Report.ModelVerdict)
	require.Empty(t, out.Report.VerdictAdjustment)
}
