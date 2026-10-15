package knowledgeops

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/stretchr/testify/require"
)

const socialReport = `{"applicability":"non_knowledge","applicability_reason":"问题和回答均为礼貌性结束语，无知识任务或事实主张","verdict":"not_applicable","claims":[],"facts":[],"causes":[],"unknowns":[],"review_questions":[],"source_check":"not_checked","risk":"low","scope":"本轮问题与回答"}`

func TestSocialReplyNeedsNeitherEvidenceNorReview(t *testing.T) {
	for _, gap := range []string{"history_source_permissions_unbound", "history_attachment_context_omitted", "history_image_context_omitted"} {
		t.Run(gap, func(t *testing.T) {
			run := Run{ID: "thanks", Status: "completed", QAType: "wiki-qa", Channel: "web", Question: "好的，谢谢你", Answer: "不客气！", CaptureStatus: "missing", Gaps: []string{"no_visible_source", "history_source_permissions_unbound"}}
			run.Gaps = append(run.Gaps, gap)
			a := NewAnalyzer(func(context.Context, string) (chat.Chat, error) { return &fakeChat{payload: socialReport}, nil }, nil)
			analysis, err := a.Analyze(context.Background(), run, nil, DefaultSettings(1))
			require.NoError(t, err)
			require.Equal(t, "not_required", analysis.Route)
			require.Empty(t, analysis.Report.Unknowns)
			run.Analysis = &analysis
			d := Detail{Run: run}
			populateCurrentWork(&d)
			require.Empty(t, queueRoutes(d))
			require.Equal(t, "not_required", d.Run.ReviewState)
			require.Equal(t, "not_required", captureCoverage(d.Run, nil).ContentStatus)
			require.Equal(t, "missing", d.Run.CaptureStatus, "the capture audit must not be rewritten")
		})
	}
}

func TestNonKnowledgeCannotHideRealProblems(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Run, *Report)
	}{
		{"legacy without applicability", func(_ *Run, r *Report) {
			*r = Report{Verdict: "not_applicable", Risk: "low", SourceCheck: "not_checked"}
		}},
		{"failed execution", func(r *Run, _ *Report) { r.Status = "failed" }},
		{"truncated answer", func(r *Run, _ *Report) { r.Gaps = append(r.Gaps, "answer_capture_budget") }},
		{"source conflict", func(_ *Run, r *Report) { r.SourceCheck = "inconsistent" }},
		{"unknowns", func(_ *Run, r *Report) { r.Unknowns = []string{"无法确定追问指代"} }},
		{"review question", func(_ *Run, r *Report) { r.ReviewQuestions = []string{"业务需求是否满足"} }},
		{"high risk", func(_ *Run, r *Report) { r.Risk = "high" }},
		{"knowledge claim", func(_ *Run, r *Report) { r.Claims = []Claim{{Text: "额度为100万", Verdict: "unverifiable"}} }},
		{"observed cause", func(_ *Run, r *Report) {
			r.Causes = []Cause{{Category: "retrieval", Summary: "未完成知识查询", Status: "suspected"}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var report Report
			require.NoError(t, json.Unmarshal([]byte(socialReport), &report))
			run := Run{Status: "completed", Question: "好的，谢谢你", Answer: "不客气！", CaptureStatus: "missing", Gaps: []string{"no_visible_source"}}
			tc.change(&run, &report)
			route, _ := Triage(run, report, DefaultSettings(1))
			require.NotEqual(t, "not_required", route)
		})
	}
}

func TestKnowledgeRequestsWithoutClaimsCannotAutoRecord(t *testing.T) {
	settings := DefaultSettings(1)
	settings.AutoRecordEnabled = true
	settings.CalibrationNote = "calibrated"
	settings.SamplePercent = 0
	for _, tc := range []struct{ question, applicability, want string }{
		{"第二点展开说说", "unknown", "needs_evidence"},
		{"请说明办理流程", "", "needs_evidence"},
		{"请说明办理流程", "knowledge", "human_review"},
	} {
		t.Run(tc.applicability, func(t *testing.T) {
			run := Run{Status: "completed", Question: tc.question, Answer: "请进一步说明", CaptureStatus: "complete", QAType: "rag-qa", Channel: "web"}
			report := Report{Applicability: tc.applicability, ApplicabilityReason: "没有完成知识任务", Verdict: "not_applicable", Risk: "low", SourceCheck: "not_checked"}
			route, _ := Triage(run, report, settings)
			require.Equal(t, tc.want, route)
		})
	}
}

func TestEmptySocialAnswerCannotFallThroughToAutoRecord(t *testing.T) {
	settings := DefaultSettings(1)
	settings.AutoRecordEnabled = true
	settings.CalibrationNote = "calibrated"
	var report Report
	require.NoError(t, json.Unmarshal([]byte(socialReport), &report))
	run := Run{Status: "completed", Question: "谢谢", CaptureStatus: "complete", QAType: "rag-qa", Channel: "web"}
	route, _ := Triage(run, report, settings)
	require.Equal(t, "human_review", route)
}

func TestSocialReplyExcludedFromQualityDenominatorsButPreservesExplicitReview(t *testing.T) {
	_, repo, engine := workbenchFixture(t)
	seedWorkbench(t, repo, "knowledge", "low", "human_review", "", 1)
	seedWorkbench(t, repo, "social", "low", "not_required", "", 0)
	require.NoError(t, repo.db.Model(&Run{}).Where("id = ?", "social").Updates(map[string]any{"question": "谢谢", "answer": "不客气", "capture_status": "missing", "gaps": `["no_visible_source"]`}).Error)
	require.NoError(t, repo.db.Model(&Analysis{}).Where("id = ?", "social-a").Update("report", socialReport).Error)
	overview := workbenchGET(t, engine, "/overview")
	require.EqualValues(t, 2, overview["total"])
	require.EqualValues(t, 2, overview["diagnosis_status"].(map[string]any)["succeeded"])
	require.EqualValues(t, 1, overview["non_knowledge_runs"])
	require.EqualValues(t, 1, overview["content_coverage"].(map[string]any)["total"])
	require.Equal(t, map[string]any{"supported": float64(1)}, overview["by_verdict"])
	require.EqualValues(t, 1, overview["pending_runs"])
	require.EqualValues(t, 1, workbenchGET(t, engine, "/runs?view=queue")["total"])
	require.NoError(t, repo.db.Create(&Review{ID: "explicit", TenantID: 1, RunID: "social", AnalysisID: "social-a", Target: "answer", Action: "needs_more_evidence", Reason: "实际回答包含遗漏的业务承诺", CreatedAt: time.Now()}).Error)
	require.EqualValues(t, 2, workbenchGET(t, engine, "/runs?view=queue")["total"])
}
