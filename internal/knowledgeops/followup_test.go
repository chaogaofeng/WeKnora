package knowledgeops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func postFollowup(t *testing.T, engine *gin.Engine, caller types.Caller, id string, input map[string]any, want int) map[string]any {
	t.Helper()
	body, err := json.Marshal(input)
	require.NoError(t, err)
	req := httptest.NewRequest("POST", "/ops/runs/"+id+"/followups", bytes.NewReader(body)).WithContext(types.WithCaller(context.Background(), caller))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	require.Equal(t, want, w.Code, w.Body.String())
	var response map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	if want == 200 {
		return response["data"].(map[string]any)
	}
	return response
}
func followupAdmin() types.Caller {
	return types.Caller{TenantID: 1, UserID: "operator", Role: types.TenantRoleAdmin}
}
func followupPayload(analysis, route, action, previous string) map[string]any {
	return map[string]any{"analysis_id": analysis, "route": route, "action": action, "reason": "checked the source and documented the limitation", "previous_id": previous, "actor_id": "spoofed"}
}
func TestFollowupClosesOnlyQueueAndReopenRestoresIt(t *testing.T) {
	_, repo, engine := workbenchFixture(t)
	seedWorkbench(t, repo, "case", "low", "human_review", "g", 0)
	saveWorkbenchReport(t, repo, "case-a", Report{Verdict: "supported", SourceCheck: "unresolved", Risk: "low"})
	require.NoError(t, repo.db.Model(&Run{}).Where("id = ?", "case").Update("gaps", `["source_version_unknown"]`).Error)
	before := workbenchGET(t, engine, "/runs/case")
	original := before["run"].(map[string]any)
	closed := postFollowup(t, engine, followupAdmin(), "case", followupPayload("case-a", "source_verification", "accepted_limitation", ""), 200)
	require.Equal(t, "operator", closed["actor_id"])
	after := workbenchGET(t, engine, "/runs/case")
	run := after["run"].(map[string]any)
	require.Equal(t, original["analysis"], run["analysis"])
	require.Equal(t, original["gaps"], run["gaps"])
	require.Equal(t, original["coverage"], run["coverage"])
	require.Equal(t, original["review_state"], run["review_state"])
	require.Equal(t, original["pending_review_targets"], run["pending_review_targets"])
	require.Equal(t, false, run["source_followup_pending"])
	require.Len(t, after["followups"], 1)
	require.Equal(t, before["reviews"], after["reviews"])
	require.Equal(t, before["evidence"], after["evidence"])
	require.Equal(t, before["adoptions"], after["adoptions"])
	states := run["followups"].([]any)
	require.Len(t, states, 1)
	require.Equal(t, "closed", states[0].(map[string]any)["state"])
	require.Equal(t, closed, states[0].(map[string]any)["latest"])
	require.EqualValues(t, 0, workbenchGET(t, engine, "/runs?view=queue&route=source_verification")["total"])
	require.EqualValues(t, 1, workbenchGET(t, engine, "/runs?view=queue&route=human_review")["total"])
	stats := workbenchGET(t, engine, "/overview")
	require.Nil(t, stats["queue_counts"].(map[string]any)["source_verification"])
	require.EqualValues(t, 1, stats["source_checks"].(map[string]any)["unresolved"])
	list := workbenchGET(t, engine, "/runs")["items"].([]any)[0].(map[string]any)
	group := workbenchGET(t, engine, "/groups")["items"].([]any)[0].(map[string]any)["representative"].(map[string]any)
	require.Equal(t, states, list["followups"])
	require.Equal(t, states, group["followups"])
	reopened := postFollowup(t, engine, followupAdmin(), "case", followupPayload("case-a", "source_verification", "reopened", closed["id"].(string)), 200)
	require.Equal(t, closed["id"], reopened["previous_id"])
	require.EqualValues(t, 1, workbenchGET(t, engine, "/runs?view=queue&route=source_verification")["total"])
	require.Len(t, workbenchGET(t, engine, "/runs/case")["followups"], 2)
}
func TestFollowupTechnicalCancellationDoesNotRewriteCapture(t *testing.T) {
	_, repo, engine := workbenchFixture(t)
	seedWorkbench(t, repo, "cancelled", "low", "technical_followup", "", 0)
	require.NoError(t, repo.db.Model(&Run{}).Where("id = ?", "cancelled").Update("status", "cancelled").Error)
	postFollowup(t, engine, followupAdmin(), "cancelled", followupPayload("cancelled-a", "technical_followup", "not_required", ""), 200)
	run := workbenchGET(t, engine, "/runs/cancelled")["run"].(map[string]any)
	require.Equal(t, "cancelled", run["status"])
	require.Equal(t, []any{}, run["pending_items"])
	require.EqualValues(t, 0, workbenchGET(t, engine, "/overview")["pending_runs"])
}
func TestFollowupHTTPRejectsUnauthorizedAndInvalidActions(t *testing.T) {
	_, repo, engine := workbenchFixture(t)
	seedWorkbench(t, repo, "case", "low", "source_verification", "", 0)
	saveWorkbenchReport(t, repo, "case-a", Report{SourceCheck: "unresolved"})
	input := followupPayload("case-a", "source_verification", "resolved", "")
	postFollowup(t, engine, types.Caller{TenantID: 1, UserID: "member", Role: types.TenantRoleViewer}, "case", input, 403)
	postFollowup(t, engine, types.Caller{TenantID: 2, UserID: "admin", Role: types.TenantRoleAdmin}, "case", input, 404)
	seedWorkbench(t, repo, "private", "low", "source_verification", "", 0)
	postFollowup(t, engine, followupAdmin(), "private", followupPayload("private-a", "source_verification", "resolved", ""), 404)
	for _, change := range []map[string]any{{"reason": " "}, {"reason": strings.Repeat("字", 1334)}, {"route": "human_review"}, {"route": "technical_followup"}, {"action": "confirmed"}, {"action": "reopened"}} {
		invalid := followupPayload("case-a", "source_verification", "resolved", "")
		for k, v := range change {
			invalid[k] = v
		}
		postFollowup(t, engine, followupAdmin(), "case", invalid, 400)
	}
	require.Empty(t, workbenchGET(t, engine, "/runs/case")["followups"])
}
func TestFollowupVersionTaskAndPreviousIDGates(t *testing.T) {
	for _, taskStatus := range []string{"pending", "running"} {
		t.Run(taskStatus, func(t *testing.T) {
			_, repo, engine := workbenchFixture(t)
			seedWorkbench(t, repo, "case", "low", "source_verification", "", 0)
			saveWorkbenchReport(t, repo, "case-a", Report{SourceCheck: "unresolved"})
			require.NoError(t, repo.db.Create(&Task{ID: "new-task", TenantID: 1, RunID: "case", Version: 2, Status: taskStatus, CreatedAt: time.Now(), UpdatedAt: time.Now()}).Error)
			postFollowup(t, engine, followupAdmin(), "case", followupPayload("case-a", "source_verification", "resolved", ""), 409)
		})
	}
	_, repo, engine := workbenchFixture(t)
	seedWorkbench(t, repo, "case", "low", "source_verification", "", 0)
	saveWorkbenchReport(t, repo, "case-a", Report{SourceCheck: "unresolved"})
	closed := postFollowup(t, engine, followupAdmin(), "case", followupPayload("case-a", "source_verification", "resolved", ""), 200)
	postFollowup(t, engine, followupAdmin(), "case", followupPayload("case-a", "source_verification", "not_required", ""), 409)
	postFollowup(t, engine, followupAdmin(), "case", followupPayload("case-a", "source_verification", "reopened", "wrong"), 409)
	require.NoError(t, repo.db.Create(&Analysis{ID: "new-a", TenantID: 1, RunID: "case", Version: 2, Status: "succeeded", Route: "auto_record", Report: Report{SourceCheck: "unresolved"}, CreatedAt: time.Now()}).Error)
	postFollowup(t, engine, followupAdmin(), "case", followupPayload("case-a", "source_verification", "reopened", closed["id"].(string)), 409)
	current := workbenchGET(t, engine, "/runs/case")
	states := current["run"].(map[string]any)["followups"].([]any)
	require.Equal(t, "pending", states[0].(map[string]any)["state"])
	require.NotContains(t, states[0].(map[string]any), "latest")
	require.Len(t, current["followups"], 1)
	require.EqualValues(t, 1, workbenchGET(t, engine, "/runs?view=queue&route=source_verification")["total"])
	postFollowup(t, engine, followupAdmin(), "case", followupPayload("new-a", "source_verification", "resolved", closed["id"].(string)), 409)
	postFollowup(t, engine, followupAdmin(), "case", followupPayload("new-a", "source_verification", "resolved", ""), 200)
}

func TestFollowupConcurrentOperatorsCannotOverwriteEachOther(t *testing.T) {
	svc, repo, _ := workbenchFixture(t)
	seedWorkbench(t, repo, "case", "low", "source_verification", "", 0)
	saveWorkbenchReport(t, repo, "case-a", Report{SourceCheck: "unresolved"})
	ctx := types.WithCaller(context.Background(), followupAdmin())
	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			_, err := svc.AddFollowup(ctx, 1, "case", FollowupInput{AnalysisID: "case-a", Route: "source_verification", Action: "resolved", Reason: "investigated"})
			results <- err
		}()
	}
	close(start)
	first, second := <-results, <-results
	require.True(t, (first == nil && errors.Is(second, ErrStale)) || (second == nil && errors.Is(first, ErrStale)), "one decision wins; the other must refresh: %v / %v", first, second)
	var count int64
	require.NoError(t, repo.db.Model(&Followup{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestFollowupRejectsAPIPrincipalEvenWithAdminCaller(t *testing.T) {
	svc, repo, engine := workbenchFixture(t)
	seedWorkbench(t, repo, "case", "low", "source_verification", "", 0)
	ctx := types.WithCaller(context.Background(), followupAdmin())
	ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalAPITenant, ID: "test-key"})
	body := bytes.NewBufferString(`{"analysis_id":"case-a","route":"source_verification","action":"resolved","reason":"reviewed","previous_id":""}`)
	request := httptest.NewRequest("POST", "/ops/runs/case/followups", body).WithContext(ctx)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	require.Equal(t, 403, response.Code)
	_, err := svc.AddFollowup(ctx, 1, "case", FollowupInput{AnalysisID: "case-a", Route: "source_verification", Action: "resolved", Reason: "reviewed"})
	require.ErrorIs(t, err, ErrForbidden)
}

func TestFollowupSQLiteConcurrentWriterReturnsRefreshConflict(t *testing.T) {
	svc, repo, engine := workbenchFixture(t)
	seedWorkbench(t, repo, "case", "low", "technical_followup", "", 0)
	require.NoError(t, repo.db.Model(&Run{}).Where("id = ?", "case").Update("status", "cancelled").Error)
	raw, err := repo.db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(2)
	tx := repo.db.Begin()
	require.NoError(t, tx.Error)
	defer tx.Rollback()
	require.NoError(t, tx.Exec("UPDATE knowledge_ops_runs SET id = id WHERE id = ?", "case").Error)
	postFollowup(t, engine, followupAdmin(), "case", followupPayload("case-a", "technical_followup", "resolved", ""), 409)
	require.NoError(t, tx.Rollback().Error)
	ctx := types.WithCaller(context.Background(), followupAdmin())
	_, err = svc.AddFollowup(ctx, 1, "case", FollowupInput{AnalysisID: "case-a", Route: "technical_followup", Action: "resolved", Reason: "investigated"})
	require.NoError(t, err, "retry after releasing SQLite write contention succeeds")
}
