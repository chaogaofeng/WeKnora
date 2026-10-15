package knowledgeops

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/stretchr/testify/require"
)

func TestGenerationCauseRequiresAnObservedAnswerProblem(t *testing.T) {
	for _, tc := range []struct {
		name, verdict, summary string
		want                   int
	}{
		{"positive assessment", "supported", "模型准确提取要点，生成内容与原文高度一致，无幻觉或遗漏。", 0},
		{"unsupported detail", "unsupported", "The answer invented a requirement.", 1},
		{"omission with positive wording", "partial", "回答无幻觉，但遗漏了适用条件。", 1},
		{"contradiction", "contradicted", "The amount contradicts the source.", 1},
		{"uncertainty is not a generation fault", "unverifiable", "Evidence is unavailable.", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := Report{Verdict: "partial", Causes: []Cause{{Category: "generation", Summary: tc.summary, Status: "evidence_backed"}}, Claims: []Claim{{Verdict: tc.verdict}}}
			stats := newWorkbenchStats()
			stats.observe(Detail{Run: Run{Analysis: &Analysis{Status: "succeeded", Report: r}}})
			require.Equal(t, tc.want, stats.Causes["generation"])
			require.Len(t, r.Causes, 1, "assessment must not rewrite the original report")
		})
	}
}

func TestCauseDrilldownMatchesCountsAndPreservesReport(t *testing.T) {
	s, repo, engine := workbenchFixture(t)
	for _, id := range []string{"positive", "problem", "failed", "private"} {
		seedWorkbench(t, repo, id, "low", "human_review", "", 0)
		r := Report{Verdict: "partial", SourceCheck: "not_checked", Claims: []Claim{{Verdict: "partial"}}, Causes: []Cause{{Category: "generation", Summary: "omits condition", Status: "suspected"}, {Category: "generation", Summary: "overstates scope", Status: "suspected"}}}
		if id == "positive" {
			r.Claims[0].Verdict = "supported"
			r.Causes = []Cause{{Category: "generation", Summary: "准确提取，无幻觉或遗漏", Status: "evidence_backed"}}
		}
		body, err := json.Marshal(r)
		require.NoError(t, err)
		require.NoError(t, repo.db.Model(&Analysis{}).Where("id = ?", id+"-a").Update("report", string(body)).Error)
	}
	require.NoError(t, repo.db.Model(&Analysis{}).Where("id = ?", "failed-a").Update("status", "failed").Error)
	v := workbenchGET(t, engine, "/overview")
	require.Equal(t, map[string]any{"generation": float64(1)}, v["causes"])
	list := workbenchGET(t, engine, "/runs?cause_category=generation")
	require.EqualValues(t, 1, list["total"])
	require.Equal(t, "problem", list["items"].([]any)[0].(map[string]any)["id"])
	require.EqualValues(t, 0, workbenchGET(t, engine, "/runs?cause_category=generation&search=positive")["total"])
	d, err := s.Detail(context.Background(), 1, "positive")
	require.NoError(t, err)
	encoded, err := json.Marshal(d.Run.Analysis)
	require.NoError(t, err)
	var result map[string]any
	require.NoError(t, json.Unmarshal(encoded, &result))
	assessment, ok := result["cause_assessment"].(map[string]any)
	require.True(t, ok, "detail must explain why the recorded cause is excluded")
	require.Empty(t, assessment["candidates"])
	require.Len(t, assessment["unlinked"], 1)
	require.Len(t, d.Run.Analysis.Report.Causes, 1)
	var stored Analysis
	require.NoError(t, repo.db.First(&stored, "id = ?", "positive-a").Error)
	require.Equal(t, "准确提取，无幻觉或遗漏", stored.Report.Causes[0].Summary)
}

func TestUnlinkedGenerationAssessmentCannotAutoRecordOrBeConfirmed(t *testing.T) {
	r := Report{Verdict: "supported", Scope: "answer", Risk: "low", SourceCheck: "not_checked", Claims: []Claim{{Text: "A", Position: "answer", Verdict: "supported", Reason: "matches source", EvidenceIDs: []string{"e"}}}, Causes: []Cause{{Category: "generation", Summary: "准确提取，无幻觉或遗漏", Status: "evidence_backed", EvidenceIDs: []string{"e"}}}}
	body, err := json.Marshal(r)
	require.NoError(t, err)
	m := &fakeChat{payload: string(body)}
	analyzer := NewAnalyzer(func(context.Context, string) (chat.Chat, error) { return m, nil }, nil)
	s := DefaultSettings(1)
	s.AutoRecordEnabled, s.CalibrationNote, s.SamplePercent = true, "calibrated", 0
	a, err := analyzer.Analyze(context.Background(), Run{Status: "completed", CaptureStatus: "complete", QAType: "rag-qa", Channel: "web"}, []Evidence{{ID: "e", Kind: "chunk", Content: "A", SentToModel: true}}, s)
	require.NoError(t, err, "one inconsistent attribution must not discard the valid answer assessment")
	require.Equal(t, "supported", a.Report.Verdict)
	require.Equal(t, "human_review", a.Route)
	require.Equal(t, "自动诊断依据不足，需结合原问题和证据复核回答", a.RouteReason)
	require.Empty(t, groupKey(Run{TenantID: 1}, r, []Evidence{{KBID: "kb", Version: "v1", SentToModel: true}}))
	_, repo, _ := workbenchFixture(t)
	seedWorkbench(t, repo, "run", "low", "human_review", "", 0)
	require.NoError(t, repo.db.Model(&Analysis{}).Where("id = ?", "run-a").Update("report", string(body)).Error)
	review := Review{RunID: "run", AnalysisID: "run-a", Target: "cause", Verdict: "evidence_backed", CauseCategory: "generation", Action: "confirmed", Reason: "review", ReviewerID: "u", CreatedAt: time.Now()}
	require.ErrorIs(t, repo.AddReview(context.Background(), 1, &review), ErrInvalid)
	review.Action = "corrected"
	require.ErrorIs(t, repo.AddReview(context.Background(), 1, &review), ErrInvalid)
	review.Target = "answer"
	review.Verdict = "partial"
	require.NoError(t, repo.AddReview(context.Background(), 1, &review), "answer corrections remain possible")
}

func TestUnlinkedLegacyAttributionsAreNotGrouped(t *testing.T) {
	_, repo, engine := workbenchFixture(t)
	for _, id := range []string{"old-one", "old-two"} {
		seedWorkbench(t, repo, id, "low", "human_review", "legacy-group", 0)
		body, err := json.Marshal(Report{Claims: []Claim{{Verdict: "supported"}}, Causes: []Cause{{Category: "generation", Summary: "successful extraction", Status: "evidence_backed"}}})
		require.NoError(t, err)
		require.NoError(t, repo.db.Model(&Analysis{}).Where("id = ?", id+"-a").Update("report", string(body)).Error)
	}
	groups := workbenchGET(t, engine, "/groups")
	require.EqualValues(t, 2, groups["total"], "inconsistent legacy diagnoses stay independently reviewable")
	require.EqualValues(t, 0, workbenchGET(t, engine, "/runs?view=queue&group_key=legacy-group")["total"])
	var a Analysis
	require.NoError(t, repo.db.First(&a, "id = ?", "old-one-a").Error)
	require.Equal(t, "legacy-group", a.GroupKey, "persisted grouping history is unchanged")
}
