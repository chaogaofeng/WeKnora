package knowledgeops

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestAnswerReviewAloneCompletesCasesWithAutomaticCauses(t *testing.T) {
	for _, status := range []string{"unresolved", "suspected", "evidence_backed"} {
		t.Run(status, func(t *testing.T) {
			_, repo, engine := workbenchFixture(t)
			seedWorkbench(t, repo, "case", "low", "quality_sample", "group", 0)
			report := Report{Verdict: "unverifiable", Risk: "low", SourceCheck: "consistent", Causes: []Cause{{Category: "capture", Status: status, Summary: "No source content was available"}}}
			saveWorkbenchReport(t, repo, "case-a", report)
			before := workbenchGET(t, engine, "/runs/case")["run"].(map[string]any)
			require.Equal(t, []any{"answer"}, before["pending_review_targets"])
			postWorkbenchReview(t, engine, "case", Review{AnalysisID: "case-a", Target: "answer", Action: "confirmed", Verdict: "unverifiable"})
			detail := workbenchGET(t, engine, "/runs/case")
			run := detail["run"].(map[string]any)
			require.Equal(t, "reviewed", run["review_state"])
			require.Empty(t, run["pending_review_targets"])
			require.Empty(t, run["pending_items"])
			require.EqualValues(t, 0, workbenchGET(t, engine, "/runs?view=queue")["total"])
			require.EqualValues(t, 0, workbenchGET(t, engine, "/groups")["total"])
			require.EqualValues(t, 1, workbenchGET(t, engine, "/overview")["sample_reviewed"])
			var stored Analysis
			require.NoError(t, repo.db.First(&stored, "id = ?", "case-a").Error)
			require.Equal(t, report, stored.Report, "human review does not rewrite automatic diagnosis")
		})
	}
}

func TestLegacyCauseReviewIsReadOnlyAndCannotBlockAnswerReview(t *testing.T) {
	_, repo, engine := workbenchFixture(t)
	seedWorkbench(t, repo, "case", "low", "human_review", "", 0)
	legacy := Review{ID: "legacy", TenantID: 1, RunID: "case", AnalysisID: "case-a", Target: "cause", CauseCategory: "retrieval", Action: "needs_more_evidence", Verdict: "unresolved", Reason: "old root-cause request", ReviewerID: "reviewer", CreatedAt: time.Now()}
	require.NoError(t, repo.db.Create(&legacy).Error)
	before := workbenchGET(t, engine, "/runs/case")["run"].(map[string]any)
	require.Equal(t, "unreviewed", before["review_state"])
	require.Equal(t, []any{"answer"}, before["pending_review_targets"])
	postWorkbenchReview(t, engine, "case", Review{AnalysisID: "case-a", Target: "answer", Action: "confirmed", Verdict: "supported"})
	detail := workbenchGET(t, engine, "/runs/case")
	require.Equal(t, "reviewed", detail["run"].(map[string]any)["review_state"])
	require.Empty(t, detail["run"].(map[string]any)["pending_items"])
	require.Len(t, detail["reviews"], 2)
	history := workbenchGET(t, engine, "/reviews?target=cause")
	require.EqualValues(t, 1, history["total"])
	require.Equal(t, "old root-cause request", history["items"].([]any)[0].(map[string]any)["reason"])
}

func TestCauseReviewWritesAreRejected(t *testing.T) {
	for _, action := range []string{"confirmed", "corrected", "needs_more_evidence"} {
		t.Run(action, func(t *testing.T) {
			_, repo, engine := workbenchFixture(t)
			seedWorkbench(t, repo, "case", "low", "human_review", "", 0)
			saveWorkbenchReport(t, repo, "case-a", Report{Verdict: "unverifiable", Causes: []Cause{{Category: "capture", Status: "unresolved"}}})
			body, err := json.Marshal(Review{AnalysisID: "case-a", Target: "cause", CauseCategory: "capture", Action: action, Verdict: "unresolved", Reason: "no longer a review target"})
			require.NoError(t, err)
			req := httptest.NewRequest("POST", "/ops/runs/case/reviews", bytes.NewReader(body)).WithContext(types.WithCaller(context.Background(), types.Caller{TenantID: 1, UserID: "reviewer", Role: types.TenantRoleAdmin}))
			req.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, req)
			require.Equal(t, 400, response.Code)
			require.EqualValues(t, 0, workbenchGET(t, engine, "/reviews")["total"])
		})
	}
}

func TestAnswerEvidenceRequestReopensOnlyAnswerReview(t *testing.T) {
	_, repo, engine := workbenchFixture(t)
	seedWorkbench(t, repo, "case", "low", "human_review", "", 0)
	postWorkbenchReview(t, engine, "case", Review{AnalysisID: "case-a", Target: "answer", Action: "confirmed", Verdict: "supported"})
	postWorkbenchReview(t, engine, "case", Review{AnalysisID: "case-a", Target: "answer", Action: "needs_more_evidence", Verdict: "unverifiable", Reason: "Need the eligibility section"})
	run := workbenchGET(t, engine, "/runs/case")["run"].(map[string]any)
	require.Equal(t, "needs_more_evidence", run["review_state"])
	require.Equal(t, []any{"answer"}, run["pending_review_targets"])
	require.EqualValues(t, 1, workbenchGET(t, engine, "/runs?view=queue&route=needs_evidence")["total"])
	postWorkbenchReview(t, engine, "case", Review{AnalysisID: "case-a", Target: "answer", Action: "corrected", Verdict: "partial", Reason: "The answer omitted the eligibility condition"})
	run = workbenchGET(t, engine, "/runs/case")["run"].(map[string]any)
	require.Equal(t, "reviewed", run["review_state"])
	require.Empty(t, run["pending_items"])
}

func TestSampleCorrectionCountsOnlyAnswerReviews(t *testing.T) {
	d := Detail{Run: Run{Analysis: &Analysis{ID: "a", Status: "succeeded", Route: "quality_sample", Report: Report{Verdict: "supported", Risk: "low", SourceCheck: "not_checked"}}}, Reviews: []Review{
		{AnalysisID: "a", Target: "cause", Action: "corrected"},
	}}
	stats := newWorkbenchStats()
	stats.observe(d)
	require.Zero(t, stats.SampleCorrected)
	require.Zero(t, stats.SampleReviewed)
	d.Reviews = append([]Review{{AnalysisID: "a", Target: "answer", Action: "corrected"}}, d.Reviews...)
	stats = newWorkbenchStats()
	stats.observe(d)
	require.Equal(t, 1, stats.SampleCorrected)
	require.Equal(t, 1, stats.SampleReviewed)
}
