package knowledgeops

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/knowledgeops/capture"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestQueryContextPersistsWithoutReplacingQuestionOrBecomingEvidence(t *testing.T) {
	s, repo := enabledTestService(t)
	ctx := capture.Begin(context.Background(), s, capture.Start{TenantID: 1, MessageID: "query-context", Question: "条件？", StartedAt: time.Now()})
	capture.RecordQuery(ctx, capture.QueryEvent{Kind: "rewrite", Query: "上海 2025 准入条件", Status: "rewritten", HistoryTurns: 1})
	capture.RecordQuery(ctx, capture.QueryEvent{Kind: "search", Query: "上海 准入", Tool: "wiki_search", ToolCallID: "c1", Round: 1, Status: "attempted"})
	capture.RecordQuery(ctx, capture.QueryEvent{Kind: "search", Query: "上海 准入", Tool: "wiki_search", ToolCallID: "c2", Round: 2, Status: "attempted"})
	capture.Finish(ctx, "请满足条件", nil)
	// A late callback must never rewrite the diagnostic input after completion.
	capture.RecordQuery(ctx, capture.QueryEvent{Kind: "rewrite", Query: "late mutation", Status: "rewritten"})
	var run Run
	require.NoError(t, repo.db.Where("message_id = ?", "query-context").Take(&run).Error)
	require.Equal(t, "条件？", run.Question)
	body, err := json.Marshal(run)
	require.NoError(t, err)
	var stored struct {
		QueryContext *capture.QuerySnapshot `json:"query_context"`
	}
	require.NoError(t, json.Unmarshal(body, &stored))
	require.NotNil(t, stored.QueryContext)
	require.Equal(t, "上海 2025 准入条件", stored.QueryContext.Rewrite.Query)
	require.Len(t, stored.QueryContext.Searches, 2, "distinct agent calls must remain distinct")
	var n int64
	require.NoError(t, repo.db.Model(&Evidence{}).Where("run_id = ?", run.ID).Count(&n).Error)
	require.Zero(t, n)
}

type queryVerifier struct {
	fakeChat
	t *testing.T
}

func (m *queryVerifier) Chat(_ context.Context, messages []chat.Message, _ *chat.ChatOptions) (*types.ChatResponse, error) {
	var body struct {
		Question     string                 `json:"question"`
		QueryContext *capture.QuerySnapshot `json:"query_context"`
		Evidence     []Evidence             `json:"evidence"`
	}
	require.NoError(m.t, json.Unmarshal([]byte(messages[1].Content), &body))
	require.Equal(m.t, "上海 2025 条件", body.Question)
	require.NotNil(m.t, body.QueryContext)
	require.Equal(m.t, "北京 2024 条件", body.QueryContext.Rewrite.Query)
	require.Empty(m.t, body.Evidence, "queries cannot be passed as source evidence")
	return &types.ChatResponse{Content: `{"verdict":"unverifiable","source_check":"not_checked","risk":"low","scope":"query drift"}`}, nil
}

func TestAnalyzerReceivesOriginalAndRecordedQuerySeparately(t *testing.T) {
	// Decode the public snapshot format so absence of the new field is a
	// behavioral failure, rather than a test that only fails to compile.
	var run Run
	require.NoError(t, json.Unmarshal([]byte(`{"question":"上海 2025 条件","query_context":{"version":1,"rewrite":{"kind":"rewrite","status":"rewritten","query":"北京 2024 条件"}}}`), &run))
	m := &queryVerifier{t: t}
	a := NewAnalyzer(func(context.Context, string) (chat.Chat, error) { return m, nil }, nil)
	_, err := a.Analyze(context.Background(), run, nil, DefaultSettings(1))
	require.NoError(t, err)
}

func TestQueryContextBudgetDoesNotInvalidateCapturedBodies(t *testing.T) {
	s, repo := enabledTestService(t)
	ctx := capture.Begin(context.Background(), s, capture.Start{TenantID: 1, MessageID: "query-budget", StartedAt: time.Now()})
	capture.RecordModelInput(ctx, []chat.Message{{Role: "user", Content: "q"}}, nil)
	capture.RecordSources(ctx, []capture.Source{{Kind: "chunk", KBID: "kb", ChunkID: "c", Version: "1", SentToModel: true, Content: "body"}})
	for i := 0; i < 100; i++ {
		capture.RecordQuery(ctx, capture.QueryEvent{Kind: "search", Tool: "wiki_search", Query: fmt.Sprint(i), Status: "attempted"})
	}
	capture.Finish(ctx, "body", nil)
	var run Run
	require.NoError(t, repo.db.Where("message_id = ?", "query-budget").Take(&run).Error)
	body, _ := json.Marshal(run)
	var stored struct {
		QueryContext *capture.QuerySnapshot `json:"query_context"`
	}
	require.NoError(t, json.Unmarshal(body, &stored))
	require.NotNil(t, stored.QueryContext)
	require.True(t, stored.QueryContext.Truncated)
	require.Len(t, stored.QueryContext.Searches, 64)
	require.Equal(t, "complete", run.CaptureStatus)
}

func TestEscapedQueryMetadataCannotBlockEvidenceDiagnosis(t *testing.T) {
	s, repo := enabledTestService(t)
	ctx := capture.Begin(context.Background(), s, capture.Start{TenantID: 1, MessageID: "escaped-queries", StartedAt: time.Now()})
	for i := 0; i < 3; i++ {
		capture.RecordQuery(ctx, capture.QueryEvent{Kind: "search", Query: strings.Repeat("<a>", 1300) + fmt.Sprint(i), Tool: "wiki_search", Status: "attempted"})
	}
	capture.Finish(ctx, "answer", nil)
	var run Run
	require.NoError(t, repo.db.Where("message_id = ?", "escaped-queries").Take(&run).Error)
	b, err := json.Marshal(run.QueryContext)
	require.NoError(t, err)
	require.LessOrEqual(t, len([]rune(string(b))), 32000)
	require.True(t, run.QueryContext.Truncated)
	model := &fakeChat{payload: `{"verdict":"supported","claims":[{"text":"answer","position":"answer","verdict":"supported","evidence_ids":["e"],"reason":"body"}],"source_check":"not_checked","risk":"low","scope":"captured body"}`}
	a := NewAnalyzer(func(context.Context, string) (chat.Chat, error) { return model, nil }, nil)
	_, err = a.Analyze(context.Background(), run, []Evidence{{ID: "e", Content: "answer", SentToModel: true}}, DefaultSettings(1))
	require.NoError(t, err)
}
