package knowledgeops

import (
	"context"
	"encoding/json"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
	"time"
)

func workbenchFixture(t *testing.T) (*Service, *Repository, *gin.Engine) {
	s, r := enabledTestService(t)
	s.access = func(_ context.Context, run Run, _ []Evidence) error {
		if run.ID == "private" {
			return ErrForbidden
		}
		return nil
	}
	e := gin.New()
	s.RegisterHTTP(e.Group("/ops"))
	return s, r, e
}
func seedWorkbench(t *testing.T, r *Repository, id, risk, route, group string, age int) {
	now := time.Now().UTC().Add(-time.Duration(age) * time.Hour)
	run := Run{ID: id, TenantID: 1, MessageID: id, Question: "Question " + id, QAType: "wiki-qa", Channel: "web", Status: "completed", Gaps: []string{}, CaptureStatus: "complete", StartedAt: now, FinishedAt: &now, UpdatedAt: now}
	require.NoError(t, r.db.Create(&run).Error)
	a := Analysis{ID: id + "-a", RunID: id, TenantID: 1, Version: 1, Status: "succeeded", Route: route, GroupKey: group, CreatedAt: now, Report: Report{Verdict: "supported", Risk: risk, SourceCheck: "consistent", Scope: "same source scope", ReviewQuestions: []string{"Check applicability"}}}
	require.NoError(t, r.db.Create(&a).Error)
}
func workbenchGET(t *testing.T, e *gin.Engine, path string) map[string]any {
	req := httptest.NewRequest("GET", "/ops"+path, nil).WithContext(types.WithCaller(context.Background(), types.Caller{TenantID: 1, UserID: "u", Role: types.TenantRoleAdmin}))
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	require.Equal(t, 200, w.Code, w.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return body["data"].(map[string]any)
}
func TestWorkbenchQueuePriorityAndAuthorizedGrouping(t *testing.T) {
	_, r, e := workbenchFixture(t)
	seedWorkbench(t, r, "low", "low", "quality_sample", "", 0)
	seedWorkbench(t, r, "high-old", "high", "human_review", "g", 3)
	seedWorkbench(t, r, "high-new", "high", "human_review", "g", 1)
	seedWorkbench(t, r, "private", "high", "human_review", "g", 4)
	page := workbenchGET(t, e, "/runs?view=queue&page_size=1")
	require.Equal(t, "high-old", page["items"].([]any)[0].(map[string]any)["id"])
	groups := workbenchGET(t, e, "/groups?page_size=1")
	require.EqualValues(t, 2, groups["total"])
	first := groups["items"].([]any)[0].(map[string]any)
	require.Equal(t, "g", first["key"])
	require.EqualValues(t, 2, first["count"])
	members := workbenchGET(t, e, "/runs?view=queue&group_key=g")
	require.EqualValues(t, 2, members["total"])
}
func TestWorkbenchOverviewSeparatesCoverageSourceAndPending(t *testing.T) {
	_, r, e := workbenchFixture(t)
	seedWorkbench(t, r, "done", "high", "human_review", "g", 2)
	seedWorkbench(t, r, "todo", "low", "quality_sample", "", 1)
	seedWorkbench(t, r, "private", "high", "human_review", "g", 3)
	require.NoError(t, r.db.Create(&Review{ID: "rv", TenantID: 1, RunID: "done", AnalysisID: "done-a", Target: "answer", Action: "confirmed", Verdict: "supported", CreatedAt: time.Now()}).Error)
	v := workbenchGET(t, e, "/overview")
	require.EqualValues(t, 2, v["total"])
	coverage, ok := v["coverage_by_mode"].(map[string]any)
	require.True(t, ok, "coverage matrix missing")
	require.EqualValues(t, 2, coverage["wiki-qa"].(map[string]any)["complete"])
	require.EqualValues(t, 2, v["source_checks"].(map[string]any)["consistent"])
	require.EqualValues(t, 1, v["queue_counts"].(map[string]any)["quality_sample"])
	require.Nil(t, v["queue_counts"].(map[string]any)["human_review"])
}
func TestWorkbenchReviewUsesBoundHistoricalAnalysis(t *testing.T) {
	_, r, e := workbenchFixture(t)
	seedWorkbench(t, r, "run", "low", "human_review", "", 100)
	require.NoError(t, r.db.Create(&Review{ID: "rv", TenantID: 1, RunID: "run", AnalysisID: "run-a", Target: "answer", Action: "corrected", Verdict: "partial", ReviewerID: "reviewer", Reason: "Original conclusion corrected", CreatedAt: time.Now()}).Error)
	require.NoError(t, r.db.Create(&Analysis{ID: "new-a", TenantID: 1, RunID: "run", Version: 2, Status: "succeeded", Report: Report{Verdict: "contradicted"}, CreatedAt: time.Now()}).Error)
	v := workbenchGET(t, e, "/reviews?review_from="+time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)+"&target=answer&action=corrected&reviewer_id=reviewer")
	require.EqualValues(t, 1, v["total"])
	row := v["items"].([]any)[0].(map[string]any)
	require.Equal(t, "Question run", row["question"])
	require.Equal(t, "supported", row["machine_verdict"])
	require.EqualValues(t, 1, row["analysis_version"])
	require.EqualValues(t, 0, workbenchGET(t, e, "/reviews?target=cause")["total"])
}

func TestWorkbenchTechnicalPendingAndHumanSupplementRemainVisible(t *testing.T) {
	_, r, e := workbenchFixture(t)
	seedWorkbench(t, r, "broken", "high", "human_review", "", 2)
	require.NoError(t, r.db.Model(&Run{}).Where("id = ?", "broken").Update("status", "failed").Error)
	require.NoError(t, r.db.Create(&Review{ID: "done", TenantID: 1, RunID: "broken", AnalysisID: "broken-a", Target: "answer", Action: "confirmed", Verdict: "supported", CreatedAt: time.Now()}).Error)
	require.EqualValues(t, 1, workbenchGET(t, e, "/runs?view=queue&route=technical_followup")["total"])
	seedWorkbench(t, r, "auto", "low", "auto_record", "", 1)
	require.NoError(t, r.db.Create(&Review{ID: "more", TenantID: 1, RunID: "auto", AnalysisID: "auto-a", Target: "answer", Action: "needs_more_evidence", Verdict: "unverifiable", CreatedAt: time.Now()}).Error)
	require.EqualValues(t, 1, workbenchGET(t, e, "/runs?view=queue&route=needs_evidence")["total"])
}

func TestIssueGroupingRequiresKnownVersionsForAllUsedSources(t *testing.T) {
	run := Run{TenantID: 1, ActorID: "u"}
	report := Report{Scope: "same product", SourceCheck: "consistent", Causes: []Cause{{Category: "generation", Summary: "missing condition"}}}
	known := Evidence{KBID: "kb", KnowledgeID: "doc", Version: "v1", Hash: "abc", SentToModel: true}
	require.NotEmpty(t, groupKey(run, report, []Evidence{known}))
	unknown := Evidence{KBID: "kb", KnowledgeID: "other", Version: "unknown", Hash: "def", SentToModel: true}
	require.Empty(t, groupKey(run, report, []Evidence{known, unknown}), "partial version knowledge must not merge separate cases")
}

func TestSourceVerificationQueueIsIndependentFromAnswerSupplement(t *testing.T) {
	_, repo, engine := workbenchFixture(t)
	for _, id := range []string{"source", "legacy", "answer"} {
		route := "needs_evidence"
		if id == "source" {
			route = "source_verification"
		}
		seedWorkbench(t, repo, id, "low", route, "", 0)
		if id != "answer" {
			report, err := json.Marshal(Report{Verdict: "supported", Risk: "low", SourceCheck: "unresolved"})
			require.NoError(t, err)
			require.NoError(t, repo.db.Model(&Analysis{}).Where("id = ?", id+"-a").Update("report", string(report)).Error)
			// Human confirmation closes answer review only, including legacy diagnoses.
			require.NoError(t, repo.db.Create(&Review{ID: id + "-review", TenantID: 1, RunID: id, AnalysisID: id + "-a", Target: "answer", Action: "confirmed", Verdict: "supported", CreatedAt: time.Now()}).Error)
		}
	}
	sources := workbenchGET(t, engine, "/runs?view=queue&route=source_verification")
	require.EqualValues(t, 2, sources["total"])
	answers := workbenchGET(t, engine, "/runs?view=queue&route=needs_evidence")
	require.EqualValues(t, 1, answers["total"])
	require.Equal(t, "answer", answers["items"].([]any)[0].(map[string]any)["id"])
	counts := workbenchGET(t, engine, "/overview")["queue_counts"].(map[string]any)
	require.EqualValues(t, 2, counts["source_verification"])
	require.EqualValues(t, 1, counts["needs_evidence"])
	var legacy Analysis
	require.NoError(t, repo.db.First(&legacy, "id = ?", "legacy-a").Error)
	require.Equal(t, "needs_evidence", legacy.Route, "read-time queue classification never rewrites the historical report")
}
