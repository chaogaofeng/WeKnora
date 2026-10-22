package knowledgeops

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAnswerOutcomeReviewsAcceptBusinessConclusionsWithoutChangingDiagnosis(t *testing.T) {
	for _, tc := range []struct {
		action, verdict, reason string
	}{
		{"accepted", "usable", ""},
		{"revision_required", "revision_required", "  需要补充办理条件  "},
		{"excluded", "excluded", ""},
	} {
		t.Run(tc.action, func(t *testing.T) {
			_, repo, engine := workbenchFixture(t)
			seedWorkbench(t, repo, "case", "low", "quality_sample", "", 0)
			report := Report{Verdict: "unsupported", Risk: "low", SourceCheck: "consistent", Scope: "本轮知识回答"}
			saveWorkbenchReport(t, repo, "case-a", report)

			review := Review{RunID: "case", AnalysisID: "case-a", Target: "answer", Action: tc.action, Verdict: tc.verdict, Reason: tc.reason, ReviewerID: "reviewer"}
			require.NoError(t, repo.AddReview(context.Background(), 1, &review))
			require.Equal(t, strings.TrimSpace(tc.reason), review.Reason)
			require.NotEmpty(t, review.ID)
			require.Equal(t, tc.verdict, review.Verdict)

			detail := workbenchGET(t, engine, "/runs/case")
			run := detail["run"].(map[string]any)
			require.Equal(t, tc.verdict, run["answer_outcome"])
			require.Equal(t, "reviewed", run["review_state"])
			require.Empty(t, run["pending_review_targets"])
			require.Empty(t, run["pending_items"])
			require.EqualValues(t, 0, workbenchGET(t, engine, "/runs?view=queue")["total"])
			stats := workbenchGET(t, engine, "/overview")
			require.EqualValues(t, 1, stats["sample_total"])
			require.EqualValues(t, 1, stats["sample_reviewed"])
			require.EqualValues(t, 0, stats["sample_corrected"], "business conclusions do not count as corrections to a machine verdict")
			listRun := workbenchGET(t, engine, "/runs")["items"].([]any)[0].(map[string]any)
			require.Equal(t, tc.verdict, listRun["answer_outcome"])

			var stored Analysis
			require.NoError(t, repo.db.First(&stored, "id = ?", "case-a").Error)
			require.Equal(t, report, stored.Report)
			history := workbenchGET(t, engine, "/reviews")["items"].([]any)[0].(map[string]any)
			require.Equal(t, "unsupported", history["machine_verdict"])
			require.Equal(t, tc.verdict, history["verdict"])
		})
	}
}

func TestAnswerOutcomeReviewValidationRejectsMismatchedSemanticsAndMissingReasons(t *testing.T) {
	for _, tc := range []struct {
		name, action, verdict, reason string
	}{
		{"accepted cannot impersonate machine support", "accepted", "supported", ""},
		{"accepted cannot be excluded", "accepted", "excluded", ""},
		{"revision cannot be usable", "revision_required", "usable", "修订回答"},
		{"excluded cannot be usable", "excluded", "usable", ""},
		{"missing acceptance verdict", "accepted", "", ""},
		{"revision needs an explanation", "revision_required", "revision_required", " \n\t "},
		{"evidence request needs an explanation", "needs_more_evidence", "unverifiable", " \t "},
		{"evidence request cannot approve answer", "needs_more_evidence", "usable", "需要附件"},
		{"legacy correction cannot approve answer", "corrected", "usable", "纠正"},
		{"oversized acceptance reason", "accepted", "usable", strings.Repeat("a", 4001)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := testStore(t)
			seedWorkbench(t, repo, "case", "low", "human_review", "", 0)
			review := Review{RunID: "case", AnalysisID: "case-a", Target: "answer", Action: tc.action, Verdict: tc.verdict, Reason: tc.reason, ReviewerID: "reviewer"}
			require.ErrorIs(t, repo.AddReview(context.Background(), 1, &review), ErrInvalid)
			var count int64
			require.NoError(t, repo.db.Model(&Review{}).Count(&count).Error)
			require.Zero(t, count)
		})
	}
}

func TestAcceptedAnswerKeepsIndependentSourceAndTechnicalWorkOpen(t *testing.T) {
	_, repo, engine := workbenchFixture(t)
	seedWorkbench(t, repo, "case", "low", "human_review", "", 0)
	require.NoError(t, repo.db.Model(&Run{}).Where("id = ?", "case").Update("status", "failed").Error)
	saveWorkbenchReport(t, repo, "case-a", Report{Verdict: "partial", SourceCheck: "unresolved", Risk: "low"})
	postWorkbenchReview(t, engine, "case", Review{AnalysisID: "case-a", Target: "answer", Action: "accepted", Verdict: "usable"})

	run := workbenchGET(t, engine, "/runs/case")["run"].(map[string]any)
	require.Equal(t, "usable", run["answer_outcome"])
	require.Equal(t, "reviewed", run["review_state"])
	require.Empty(t, run["pending_review_targets"])
	items := run["pending_items"].([]any)
	require.Len(t, items, 2)
	for _, item := range items {
		require.Contains(t, []string{"source_verification", "technical_followup"}, item.(map[string]any)["route"])
	}
	stats := workbenchGET(t, engine, "/overview")
	require.Equal(t, map[string]any{"source_verification": float64(1), "technical_followup": float64(1)}, stats["queue_counts"])
	require.EqualValues(t, 1, stats["pending_runs"])
}

func TestAnswerOutcomeFollowsLatestCurrentVersionAnswerReview(t *testing.T) {
	_, repo, engine := workbenchFixture(t)
	seedWorkbench(t, repo, "case", "low", "quality_sample", "", 0)
	postWorkbenchReview(t, engine, "case", Review{AnalysisID: "case-a", Target: "answer", Action: "accepted", Verdict: "usable"})
	postWorkbenchReview(t, engine, "case", Review{AnalysisID: "case-a", Target: "answer", Action: "needs_more_evidence", Verdict: "unverifiable", Reason: "需要附件支持办理条件"})
	run := workbenchGET(t, engine, "/runs/case")["run"].(map[string]any)
	require.Equal(t, "needs_evidence", run["answer_outcome"])
	require.Equal(t, "needs_more_evidence", run["review_state"])
	require.Equal(t, []any{"answer"}, run["pending_review_targets"])
	require.EqualValues(t, 0, workbenchGET(t, engine, "/overview")["sample_reviewed"])

	postWorkbenchReview(t, engine, "case", Review{AnalysisID: "case-a", Target: "answer", Action: "revision_required", Verdict: "revision_required", Reason: "附件已核对，需修订条件"})
	run = workbenchGET(t, engine, "/runs/case")["run"].(map[string]any)
	require.Equal(t, "revision_required", run["answer_outcome"])
	require.Empty(t, run["pending_items"])

	newAnalysis := Analysis{ID: "case-new", RunID: "case", TenantID: 1, Version: 2, Status: "succeeded", Route: "human_review", Report: Report{Verdict: "supported", Risk: "low", SourceCheck: "consistent"}, CreatedAt: time.Now().UTC()}
	require.NoError(t, repo.db.Create(&newAnalysis).Error)
	run = workbenchGET(t, engine, "/runs/case")["run"].(map[string]any)
	require.Equal(t, "", run["answer_outcome"], "a new analysis requires its own business conclusion")
	require.Equal(t, "unreviewed", run["review_state"])
	stale := Review{RunID: "case", AnalysisID: "case-a", Target: "answer", Action: "accepted", Verdict: "usable", ReviewerID: "reviewer"}
	require.ErrorIs(t, repo.AddReview(context.Background(), 1, &stale), ErrStale)
}

func TestLegacyMachineReviewsDoNotImplyUsableAnswer(t *testing.T) {
	for _, action := range []string{"confirmed", "corrected"} {
		t.Run(action, func(t *testing.T) {
			_, repo, engine := workbenchFixture(t)
			seedWorkbench(t, repo, "case", "low", "human_review", "", 0)
			postWorkbenchReview(t, engine, "case", Review{AnalysisID: "case-a", Target: "answer", Action: "accepted", Verdict: "usable"})
			postWorkbenchReview(t, engine, "case", Review{AnalysisID: "case-a", Target: "answer", Action: action, Verdict: "supported", Reason: "核对机器证据判断"})
			run := workbenchGET(t, engine, "/runs/case")["run"].(map[string]any)
			require.Equal(t, "reviewed", run["review_state"])
			require.Equal(t, "", run["answer_outcome"], "a newer legacy review cannot retain an earlier usability approval")
		})
	}
}
