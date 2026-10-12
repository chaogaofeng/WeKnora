package knowledgeops

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func postWorkbenchReview(t *testing.T, engine *gin.Engine, run string, review Review) map[string]any {
	t.Helper()
	body, err := json.Marshal(review)
	require.NoError(t, err)
	req := httptest.NewRequest("POST", "/ops/runs/"+run+"/reviews", bytes.NewReader(body)).WithContext(types.WithCaller(context.Background(), types.Caller{TenantID: 1, UserID: "reviewer", Role: types.TenantRoleAdmin}))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	require.Equal(t, 200, w.Code, w.Body.String())
	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	return result["data"].(map[string]any)
}

func saveWorkbenchReport(t *testing.T, repo *Repository, id string, report Report) {
	t.Helper()
	body, err := json.Marshal(report)
	require.NoError(t, err)
	require.NoError(t, repo.db.Model(&Analysis{}).Where("id = ?", id).Update("report", string(body)).Error)
}

func TestPendingCauseEvidenceClosesOnlyForSameCategory(t *testing.T) {
	_, repo, engine := workbenchFixture(t)
	seedWorkbench(t, repo, "case", "low", "human_review", "g", 1)
	saveWorkbenchReport(t, repo, "case-a", Report{Verdict: "supported", SourceCheck: "consistent", Risk: "low", Causes: []Cause{{Category: "retrieval", Status: "suspected"}, {Category: "applicability", Status: "suspected"}}})
	postWorkbenchReview(t, engine, "case", Review{AnalysisID: "case-a", Target: "answer", Action: "confirmed", Verdict: "supported", Reason: "checked answer"})
	postWorkbenchReview(t, engine, "case", Review{AnalysisID: "case-a", Target: "cause", CauseCategory: "retrieval", Action: "needs_more_evidence", Verdict: "unresolved", Reason: "old request"})
	request := postWorkbenchReview(t, engine, "case", Review{AnalysisID: "case-a", Target: "cause", CauseCategory: "retrieval", Action: "needs_more_evidence", Verdict: "unresolved", Reason: "retrieval log still needed"})
	postWorkbenchReview(t, engine, "case", Review{AnalysisID: "case-a", Target: "cause", CauseCategory: "applicability", Action: "confirmed", Verdict: "suspected", Reason: "confirmed this candidate"})

	require.EqualValues(t, 1, workbenchGET(t, engine, "/runs?view=queue&route=needs_evidence")["total"], "a different cause cannot close the retrieval request")
	run := workbenchGET(t, engine, "/runs/case")["run"].(map[string]any)
	require.Equal(t, "partially_reviewed", run["review_state"])
	require.Equal(t, []any{"cause"}, run["pending_review_targets"])
	items := run["pending_items"].([]any)
	require.Len(t, items, 2)
	item := items[1].(map[string]any)
	require.Equal(t, map[string]any{"route": "needs_evidence", "reason": "retrieval log still needed", "target": "cause", "cause_category": "retrieval", "review_id": request["id"], "reviewer_id": "reviewer", "created_at": request["created_at"]}, item)

	postWorkbenchReview(t, engine, "case", Review{AnalysisID: "case-a", Target: "cause", CauseCategory: "retrieval", Action: "corrected", Verdict: "evidence_backed", Reason: "retrieval log supplied"})
	run = workbenchGET(t, engine, "/runs/case")["run"].(map[string]any)
	require.Equal(t, "reviewed", run["review_state"])
	require.Equal(t, []any{}, run["pending_items"])
	require.Equal(t, []any{}, run["pending_review_targets"])
	require.EqualValues(t, 0, workbenchGET(t, engine, "/groups")["total"])
}

func TestPendingCauseRequestsAreIndependentWithoutRequiringAllCandidates(t *testing.T) {
	a := &Analysis{ID: "a", Route: "human_review", Report: Report{SourceCheck: "consistent", Causes: []Cause{{Category: "retrieval"}, {Category: "applicability"}, {Category: "generation"}}}}
	d := Detail{Run: Run{Analysis: a}, Reviews: []Review{
		{AnalysisID: "a", Target: "cause", CauseCategory: "applicability", Action: "confirmed"},
		{AnalysisID: "a", Target: "cause", CauseCategory: "applicability", Action: "needs_more_evidence"},
		{AnalysisID: "a", Target: "cause", CauseCategory: "retrieval", Action: "needs_more_evidence"},
		{AnalysisID: "a", Target: "answer", Action: "confirmed"},
	}}
	require.Equal(t, "partially_reviewed", reviewState(d))
	require.Contains(t, queueRoutes(d), "needs_evidence")
	d.Reviews = append([]Review{{AnalysisID: "a", Target: "cause", CauseCategory: "retrieval", Action: "confirmed"}}, d.Reviews...)
	require.Equal(t, "reviewed", reviewState(d), "two overall conclusions suffice; no review of every candidate is required")
	require.Empty(t, queueRoutes(d))
}

func TestExplicitCauseRequestsRemainActionableWithoutMachineCandidates(t *testing.T) {
	_, repo, engine := workbenchFixture(t)
	seedWorkbench(t, repo, "auto", "low", "auto_record", "", 0)
	postWorkbenchReview(t, engine, "auto", Review{AnalysisID: "auto-a", Target: "answer", Action: "confirmed", Verdict: "supported", Reason: "answer checked"})
	postWorkbenchReview(t, engine, "auto", Review{AnalysisID: "auto-a", Target: "cause", CauseCategory: "retrieval", Action: "needs_more_evidence", Verdict: "unresolved", Reason: "need retrieval log"})
	postWorkbenchReview(t, engine, "auto", Review{AnalysisID: "auto-a", Target: "cause", CauseCategory: "applicability", Action: "needs_more_evidence", Verdict: "unresolved", Reason: "need product scope"})
	run := workbenchGET(t, engine, "/runs/auto")["run"].(map[string]any)
	require.Equal(t, "partially_reviewed", run["review_state"])
	require.Equal(t, []any{"cause"}, run["pending_review_targets"])
	items := run["pending_items"].([]any)
	require.Len(t, items, 2)
	categories := []any{}
	for _, item := range items {
		task := item.(map[string]any)
		require.Equal(t, "needs_evidence", task["route"], "automatic recording is not an active task")
		categories = append(categories, task["cause_category"])
	}
	require.ElementsMatch(t, []any{"retrieval", "applicability"}, categories)
	stats := workbenchGET(t, engine, "/overview")
	require.EqualValues(t, 1, stats["pending_runs"])
	require.Equal(t, map[string]any{"needs_evidence": float64(1)}, stats["queue_counts"], "multiple requests in one queue count the run once")

	postWorkbenchReview(t, engine, "auto", Review{AnalysisID: "auto-a", Target: "cause", CauseCategory: "applicability", Action: "corrected", Verdict: "evidence_backed", Reason: "product scope supplied"})
	run = workbenchGET(t, engine, "/runs/auto")["run"].(map[string]any)
	items = run["pending_items"].([]any)
	require.Len(t, items, 1)
	require.Equal(t, "retrieval", items[0].(map[string]any)["cause_category"])
	postWorkbenchReview(t, engine, "auto", Review{AnalysisID: "auto-a", Target: "cause", CauseCategory: "retrieval", Action: "corrected", Verdict: "evidence_backed", Reason: "retrieval log supplied"})
	require.EqualValues(t, 0, workbenchGET(t, engine, "/runs?view=queue")["total"])
}

func TestIndependentTasksDoNotRequireHumanConclusions(t *testing.T) {
	for _, route := range []string{"source_verification", "technical_followup"} {
		t.Run(route, func(t *testing.T) {
			_, repo, engine := workbenchFixture(t)
			seedWorkbench(t, repo, "case", "low", route, "", 0)
			if route == "source_verification" {
				saveWorkbenchReport(t, repo, "case-a", Report{Verdict: "supported", SourceCheck: "unresolved", Risk: "low"})
			} else {
				require.NoError(t, repo.db.Model(&Analysis{}).Where("id = ?", "case-a").Update("status", "failed").Error)
			}
			run := workbenchGET(t, engine, "/runs/case")["run"].(map[string]any)
			require.Equal(t, "unreviewed", run["review_state"])
			require.Equal(t, []any{}, run["pending_review_targets"])
			items := run["pending_items"].([]any)
			require.Len(t, items, 1)
			require.Equal(t, route, items[0].(map[string]any)["route"])
		})
	}
}

func TestPendingWorkIgnoresPreviousAnalysisReviews(t *testing.T) {
	_, repo, engine := workbenchFixture(t)
	seedWorkbench(t, repo, "case", "low", "human_review", "", 1)
	postWorkbenchReview(t, engine, "case", Review{AnalysisID: "case-a", Target: "cause", CauseCategory: "retrieval", Action: "needs_more_evidence", Verdict: "unresolved", Reason: "old retrieval question"})
	require.NoError(t, repo.db.Create(&Analysis{ID: "case-new", TenantID: 1, RunID: "case", Version: 2, Status: "succeeded", Route: "auto_record", Report: Report{Risk: "low", Verdict: "supported", SourceCheck: "consistent"}, CreatedAt: time.Now()}).Error)
	run := workbenchGET(t, engine, "/runs/case")["run"].(map[string]any)
	require.Equal(t, "unreviewed", run["review_state"])
	require.Equal(t, []any{}, run["pending_items"], "old requests cannot reopen the latest automatic record")
	require.Equal(t, []any{}, run["pending_review_targets"])
	require.EqualValues(t, 0, workbenchGET(t, engine, "/runs?view=queue")["total"])
	require.EqualValues(t, 0, workbenchGET(t, engine, "/overview")["pending_runs"])
	stale := Review{RunID: "case", AnalysisID: "case-a", Target: "cause", CauseCategory: "retrieval", Action: "corrected", Verdict: "evidence_backed", Reason: "old decision", ReviewerID: "reviewer"}
	require.ErrorIs(t, repo.AddReview(context.Background(), 1, &stale), ErrStale)
}

func TestBusinessRiskFilterPreservedWithChronologicalQueue(t *testing.T) {
	_, repo, engine := workbenchFixture(t)
	seedWorkbench(t, repo, "policy-high", "low", "human_review", "same", 0)
	seedWorkbench(t, repo, "medium", "medium", "human_review", "same", -1)
	require.NoError(t, repo.db.Model(&Run{}).Where("id = ?", "policy-high").Updates(map[string]any{"question": "融资额度上限是多少", "answer": "额度100万"}).Error)
	run, err := repo.Run(context.Background(), 1, "policy-high")
	require.NoError(t, err)
	model := &fakeChat{payload: `{"verdict":"supported","claims":[{"text":"额度100万","position":"answer","verdict":"supported","evidence_ids":["e"],"reason":"matches source"}],"facts":[],"causes":[],"unknowns":[],"review_questions":[],"source_check":"not_checked","risk":"low","scope":"loan policy"}`}
	analyzer := NewAnalyzer(func(context.Context, string) (chat.Chat, error) { return model, nil }, nil)
	analysis, err := analyzer.Analyze(context.Background(), run, []Evidence{{ID: "e", Kind: "chunk", Content: "额度100万", SentToModel: true}}, DefaultSettings(1))
	require.NoError(t, err)
	require.Equal(t, "human_review", analysis.Route)
	require.Equal(t, "low", analysis.Report.Risk)
	saveWorkbenchReport(t, repo, "policy-high-a", analysis.Report)
	page := workbenchGET(t, engine, "/runs?view=queue&page_size=1")
	first := page["items"].([]any)[0].(map[string]any)
	require.Equal(t, "medium", first["id"], "newest diagnosis must sort before older high-risk cases")
	require.Equal(t, "medium", first["effective_risk"])
	highRiskPage := workbenchGET(t, engine, "/runs?view=queue&risk=high")
	require.EqualValues(t, 1, highRiskPage["total"])
	highRiskRun := highRiskPage["items"].([]any)[0].(map[string]any)
	require.Equal(t, "policy-high", highRiskRun["id"])
	require.Equal(t, "high", highRiskRun["effective_risk"])
	require.EqualValues(t, 0, workbenchGET(t, engine, "/runs?view=queue&risk=low")["total"])
	groups := workbenchGET(t, engine, "/groups")
	representative := groups["items"].([]any)[0].(map[string]any)["representative"].(map[string]any)
	require.Equal(t, "medium", representative["id"])
	require.Equal(t, "medium", representative["effective_risk"])
	detail := workbenchGET(t, engine, "/runs/policy-high")
	require.Equal(t, "high", detail["run"].(map[string]any)["effective_risk"])
	require.Equal(t, "low", detail["analyses"].([]any)[0].(map[string]any)["report"].(map[string]any)["risk"], "read-time policy must preserve the original machine report")
}

func TestPendingItemsShareQueueCountersAndPreserveIndependentTasks(t *testing.T) {
	_, repo, engine := workbenchFixture(t)
	seedWorkbench(t, repo, "case", "low", "human_review", "", 0)
	require.NoError(t, repo.db.Model(&Run{}).Where("id = ?", "case").Update("status", "failed").Error)
	require.NoError(t, repo.db.Model(&Analysis{}).Where("id = ?", "case-a").Update("route_reason", "高风险业务结论，须人工复核").Error)
	saveWorkbenchReport(t, repo, "case-a", Report{Verdict: "supported", SourceCheck: "unresolved", Risk: "low"})
	postWorkbenchReview(t, engine, "case", Review{AnalysisID: "case-a", Target: "answer", Action: "confirmed", Verdict: "supported", Reason: "answer checked"})
	run := workbenchGET(t, engine, "/runs/case")["run"].(map[string]any)
	require.Equal(t, "reviewed", run["review_state"])
	require.Equal(t, []any{}, run["pending_review_targets"])
	items, ok := run["pending_items"].([]any)
	require.True(t, ok, "current pending items must be returned by detail")
	require.Len(t, items, 2)
	for _, item := range items {
		task := item.(map[string]any)
		require.Contains(t, []string{"source_verification", "technical_followup"}, task["route"])
		require.NotEmpty(t, task["reason"])
		require.NotContains(t, task["reason"], "高风险", "independent task reasons must not reuse the closed machine-review reason")
		require.EqualValues(t, 1, workbenchGET(t, engine, "/runs?view=queue&route="+task["route"].(string))["total"])
	}
	stats := workbenchGET(t, engine, "/overview")
	require.EqualValues(t, 1, stats["pending_runs"])
	require.Equal(t, map[string]any{"source_verification": float64(1), "technical_followup": float64(1)}, stats["queue_counts"])
	listRun := workbenchGET(t, engine, "/runs")["items"].([]any)[0].(map[string]any)
	require.Equal(t, items, listRun["pending_items"])
	groupRun := workbenchGET(t, engine, "/groups")["items"].([]any)[0].(map[string]any)["representative"].(map[string]any)
	require.Equal(t, items, groupRun["pending_items"])
}

func TestPureRAGSourceDisplayMatchesOverview(t *testing.T) {
	_, repo, engine := workbenchFixture(t)
	seedWorkbench(t, repo, "rag", "low", "auto_record", "", 0)
	require.NoError(t, repo.db.Model(&Run{}).Where("id = ?", "rag").Update("qa_type", "rag-qa").Error)
	saveWorkbenchReport(t, repo, "rag-a", Report{Verdict: "supported", SourceCheck: "not_checked", Risk: "low"})
	run := workbenchGET(t, engine, "/runs/rag")["run"].(map[string]any)
	require.Equal(t, "not_applicable", run["source_check_display"])
	require.Equal(t, []any{}, run["pending_items"])
	require.Equal(t, []any{}, run["pending_review_targets"])
	listRun := workbenchGET(t, engine, "/runs")["items"].([]any)[0].(map[string]any)
	require.Equal(t, "not_applicable", listRun["source_check_display"])
	require.Equal(t, map[string]any{"not_applicable": float64(1)}, workbenchGET(t, engine, "/overview")["source_checks"])
	require.Equal(t, "not_checked", run["analysis"].(map[string]any)["report"].(map[string]any)["source_check"])
}
