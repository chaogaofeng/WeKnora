package knowledgeops

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/knowledgeops/capture"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type unusedChat = chat.Chat
type verifierFake struct {
	unusedChat
	t         *testing.T
	malformed bool
}

func (f verifierFake) GetModelName() string { return "fixture-model" }
func (f verifierFake) Chat(ctx context.Context, m []chat.Message, o *chat.ChatOptions) (*types.ChatResponse, error) {
	require.Empty(f.t, o.Tools)
	require.Equal(f.t, "none", o.ToolChoice)
	if f.malformed {
		return &types.ChatResponse{Content: "not json"}, nil
	}
	var body struct {
		Evidence []Evidence `json:"evidence"`
	}
	require.NoError(f.t, json.Unmarshal([]byte(m[1].Content), &body))
	id := ""
	for _, e := range body.Evidence {
		if e.SentToModel && e.Kind != "conversation" {
			id = e.ID
			break
		}
	}
	report := Report{Verdict: "supported", Scope: "captured source support only", Risk: "low", SourceCheck: "not_checked", Claims: []Claim{{Text: "回答", Position: "answer", Verdict: "supported", EvidenceIDs: []string{id}, Reason: "fixture evidence"}}}
	b, _ := json.Marshal(report)
	return &types.ChatResponse{Content: string(b)}, nil
}
func enabledTestService(t *testing.T) (*Service, *Repository) {
	repo := testStore(t)
	s := DefaultSettings(1)
	s.Enabled = true
	s.ModelID = "fixture"
	require.NoError(t, repo.SaveSettings(context.Background(), 1, s))
	return NewService(repo, NewAnalyzer(func(context.Context, string) (chat.Chat, error) { return verifierFake{t: t}, nil }, nil), func(context.Context, Run, []Evidence) error { return nil }), repo
}
func TestThreeModesEndToEndWithVersionedIndependentReviews(t *testing.T) {
	for _, mode := range []string{"rag-qa", "wiki-qa", "hybrid-rag-wiki"} {
		t.Run(mode, func(t *testing.T) {
			svc, repo := enabledTestService(t)
			ctx := context.Background()
			rec := svc.Start(ctx, capture.Start{TenantID: 1, MessageID: uuid.NewString(), QAType: mode, Channel: "web", Question: "如何使用", StartedAt: time.Now()})
			require.NotNil(t, rec)
			rec.RecordInput(ctx, capture.Input{ModelID: "actual", Messages: []capture.InputMessage{{Role: "user", Content: "Q"}}})
			rec.RecordSources(ctx, []capture.Source{{Kind: "wiki", KBID: "kb", PageID: "page", Version: "1", Content: "回答", SentToModel: true}})
			rec.Finish(ctx, capture.Completion{Status: "completed", Answer: "回答"})
			require.NoError(t, svc.RunOnce(ctx))
			var run Run
			require.NoError(t, repo.db.First(&run).Error)
			require.Equal(t, "actual", run.ModelID)
			d, err := svc.Detail(ctx, 1, run.ID)
			require.NoError(t, err)
			require.Len(t, d.Analyses, 1)
			a := d.Analyses[0]
			require.Equal(t, "succeeded", a.Status)
			if mode == "rag-qa" {
				require.Equal(t, "human_review", a.Route)
			} else {
				require.Equal(t, "source_verification", a.Route)
			}
			review := Review{RunID: run.ID, AnalysisID: a.ID, Target: "answer", Verdict: "supported", Action: "confirmed", Reason: "人工核对", ReviewerID: "u"}
			require.NoError(t, repo.AddReview(ctx, 1, &review))
			require.NoError(t, repo.Retry(ctx, 1, run.ID))
			require.NoError(t, svc.RunOnce(ctx))
			d, err = svc.Detail(ctx, 1, run.ID)
			require.NoError(t, err)
			require.Len(t, d.Analyses, 2)
			require.Len(t, d.Reviews, 1)
			require.Equal(t, "unreviewed", reviewState(d))
			review.ID = ""
			require.ErrorIs(t, repo.AddReview(ctx, 1, &review), ErrStale)
		})
	}
}
func TestMalformedAnalysisIsVisibleFailureNotSuccess(t *testing.T) {
	svc, repo := enabledTestService(t)
	svc.analyzer.models = func(context.Context, string) (chat.Chat, error) { return verifierFake{t: t, malformed: true}, nil }
	ctx := context.Background()
	rec := svc.Start(ctx, capture.Start{TenantID: 1, MessageID: "m", StartedAt: time.Now()})
	rec.Finish(ctx, capture.Completion{Status: "completed", Answer: "A"})
	require.NoError(t, svc.RunOnce(ctx))
	var a Analysis
	require.NoError(t, repo.db.First(&a).Error)
	require.Equal(t, "failed", a.Status)
	require.Equal(t, "technical_followup", a.Route)
	require.Equal(t, "unverifiable", a.Report.Verdict)
}
func TestInterruptedCaptureRecoveredOnceAndRecentUntouched(t *testing.T) {
	svc, repo := enabledTestService(t)
	ctx := context.Background()
	_ = svc
	for i, age := range []time.Duration{2 * time.Hour, time.Minute} {
		r := Run{ID: fmt.Sprint(i), TenantID: 1, MessageID: fmt.Sprint(i), Status: "running", StartedAt: time.Now().Add(-age), UpdatedAt: time.Now().Add(-age)}
		require.NoError(t, repo.CreateRun(ctx, &r))
	}
	require.NoError(t, repo.RecoverInterrupted(ctx, time.Now()))
	require.NoError(t, repo.RecoverInterrupted(ctx, time.Now()))
	old, err := repo.Run(ctx, 1, "0")
	require.NoError(t, err)
	require.Equal(t, "failed", old.Status)
	require.Contains(t, old.Gaps, "capture_interrupted")
	recent, _ := repo.Run(ctx, 1, "1")
	require.Equal(t, "running", recent.Status)
	var n int64
	repo.db.Model(&Task{}).Count(&n)
	require.EqualValues(t, 1, n)
}
func TestHTTPPermissionsHideCountsAndBodies(t *testing.T) {
	svc, repo := enabledTestService(t)
	ctx := context.Background()
	r := Run{ID: "private", TenantID: 1, MessageID: "m", Question: "private secret", StartedAt: time.Now()}
	require.NoError(t, repo.CreateRun(ctx, &r))
	svc.access = func(context.Context, Run, []Evidence) error { return ErrForbidden }
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	svc.RegisterHTTP(engine.Group("/ops"))
	for _, path := range []string{"/ops/runs", "/ops/overview", "/ops/runs/private"} {
		request := httptest.NewRequest("GET", path, nil).WithContext(types.WithCaller(ctx, types.Caller{TenantID: 1, UserID: "u", Role: types.TenantRoleAdmin}))
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, request)
		require.NotContains(t, w.Body.String(), "private secret")
		if strings.HasSuffix(path, "private") {
			require.Equal(t, 404, w.Code)
		} else {
			require.Equal(t, 200, w.Code)
			require.Contains(t, w.Body.String(), `"total":0`)
		}
	}
	req := httptest.NewRequest("GET", "/ops/settings", nil).WithContext(types.WithCaller(ctx, types.Caller{TenantID: 1, UserID: "u", Role: types.TenantRoleViewer}))
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	require.Equal(t, 403, w.Code)
}
func TestReviewLatestDecisionControlsQueue(t *testing.T) {
	d := Detail{Run: Run{Analysis: &Analysis{ID: "a"}}, Reviews: []Review{{AnalysisID: "a", Target: "answer", Action: "needs_more_evidence"}, {AnalysisID: "a", Target: "answer", Action: "confirmed"}}}
	require.Equal(t, "needs_more_evidence", reviewState(d))
}

func TestHighRiskFailureRemainsBusinessReviewAndTechnicalFilter(t *testing.T) {
	svc, repo := enabledTestService(t)
	svc.analyzer.models = func(context.Context, string) (chat.Chat, error) { return verifierFake{t: t, malformed: true}, nil }
	ctx := context.Background()
	rec := svc.Start(ctx, capture.Start{TenantID: 1, MessageID: "risk", Question: "融资额度上限是多少", StartedAt: time.Now()})
	rec.Finish(ctx, capture.Completion{Status: "completed", Answer: "额度100万"})
	require.NoError(t, svc.RunOnce(ctx))
	var a Analysis
	require.NoError(t, repo.db.First(&a).Error)
	require.Equal(t, "failed", a.Status)
	require.Equal(t, "human_review", a.Route)
	require.Equal(t, "high", a.Report.Risk)
	engine := gin.New()
	svc.RegisterHTTP(engine.Group("/ops"))
	req := httptest.NewRequest("GET", "/ops/runs?route=technical_followup", nil).WithContext(types.WithCaller(ctx, types.Caller{TenantID: 1, UserID: "u", Role: types.TenantRoleAdmin}))
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"total":1`)
	v := Review{RunID: a.RunID, AnalysisID: a.ID, Target: "answer", Verdict: "unverifiable", Action: "confirmed", Reason: "业务暂不可核验", ReviewerID: "u"}
	require.NoError(t, repo.AddReview(ctx, 1, &v))
	req = httptest.NewRequest("GET", "/ops/runs?view=queue&route=technical_followup", nil).WithContext(types.WithCaller(ctx, types.Caller{TenantID: 1, UserID: "u", Role: types.TenantRoleAdmin}))
	w = httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"total":1`)
}
func TestOverviewCountsActualPendingTaskAfterRetry(t *testing.T) {
	svc, repo := enabledTestService(t)
	ctx := context.Background()
	rec := svc.Start(ctx, capture.Start{TenantID: 1, MessageID: "pending", StartedAt: time.Now()})
	rec.Finish(ctx, capture.Completion{Status: "completed"})
	require.NoError(t, svc.RunOnce(ctx))
	var run Run
	require.NoError(t, repo.db.First(&run).Error)
	require.NoError(t, repo.Retry(ctx, 1, run.ID))
	_ = svc.Start(ctx, capture.Start{TenantID: 1, MessageID: "still-capturing", StartedAt: time.Now()})
	engine := gin.New()
	svc.RegisterHTTP(engine.Group("/ops"))
	req := httptest.NewRequest("GET", "/ops/overview", nil).WithContext(types.WithCaller(ctx, types.Caller{TenantID: 1, UserID: "u", Role: types.TenantRoleAdmin}))
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"pending_tasks":1`)
}
