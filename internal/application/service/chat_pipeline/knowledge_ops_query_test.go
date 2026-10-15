package chatpipeline

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/knowledgeops/capture"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

func TestSkippedRetrievalIsRecordedBeforeFollowingStages(t *testing.T) {
	r := &queryOpsRecorder{}
	ctx := capture.WithRecorder(context.Background(), r)
	cm := &types.ChatManage{PipelineState: types.PipelineState{Intent: types.IntentFollowUp, RewriteQuery: "复述第三点"}}
	require.Nil(t, (&PluginSearchParallel{}).OnEvent(ctx, types.CHUNK_SEARCH_PARALLEL, cm, func() *PluginError {
		require.Len(t, r.queries, 1)
		body, err := json.Marshal(r.queries[0])
		require.NoError(t, err)
		var event map[string]any
		require.NoError(t, json.Unmarshal(body, &event))
		require.Equal(t, "retrieval", event["kind"])
		require.Equal(t, "skipped", event["status"])
		require.Equal(t, "follow_up", event["intent"])
		return nil
	}))
}

type queryOpsRecorder struct {
	opsRecorder
	queries []capture.QueryEvent
}

func (r *queryOpsRecorder) RecordQuery(_ context.Context, event capture.QueryEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.queries = append(r.queries, event)
}

type queryOpsModel struct {
	openStreamChat
	output string
	err    error
}

func (m queryOpsModel) GetModelID() string { return "rewriter" }
func (m queryOpsModel) Chat(context.Context, []chat.Message, *chat.ChatOptions) (*types.ChatResponse, error) {
	return &types.ChatResponse{Content: m.output}, m.err
}

func TestQueryUnderstandingCaptureIncludesSingleTurnAndFallbackBeforeNextStage(t *testing.T) {
	for _, tc := range []struct {
		name, output, status, query string
		disabled                    bool
		err                         error
		history                     bool
		omitHistory                 bool
	}{
		{name: "single turn", output: `{"rewrite_query":"上海 2025 准入条件","intent":"kb_search"}`, status: "rewritten", query: "上海 2025 准入条件"},
		{name: "multi turn", output: `{"rewrite_query":"上海 2025 准入条件","intent":"kb_search"}`, status: "rewritten", query: "上海 2025 准入条件", history: true},
		{name: "template excludes history", output: `{"rewrite_query":"条件？","intent":"kb_search"}`, status: "unchanged", query: "条件？", history: true, omitHistory: true},
		{name: "unchanged", output: `{"rewrite_query":"条件？","intent":"kb_search"}`, status: "unchanged", query: "条件？"},
		{name: "disabled", disabled: true, status: "disabled", query: "条件？"},
		{name: "invalid response", output: "not structured", status: "failed", query: "条件？"},
		{name: "model failed", err: errors.New("api_key=secret"), status: "failed", query: "条件？"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &queryOpsRecorder{}
			ctx := capture.WithRecorder(context.Background(), r)
			cm := &types.ChatManage{PipelineRequest: types.PipelineRequest{Query: "条件？", EnableRewrite: !tc.disabled}}
			if tc.history {
				cm.History = []*types.History{{Query: "上海 2025 准入", Answer: "先了解条件"}}
			}
			prompt := "{{conversation}}\n{{query}}"
			if tc.omitHistory {
				prompt = "{{query}}"
			}
			p := &PluginQueryUnderstand{config: &config.Config{Conversation: &config.ConversationConfig{RewritePromptUser: prompt}}, modelService: &stubModelService{model: &queryOpsModel{output: tc.output, err: tc.err}}}
			require.Nil(t, p.OnEvent(ctx, types.QUERY_UNDERSTAND, cm, func() *PluginError {
				require.Len(t, r.queries, 1, "capture must run before completion can finish the run")
				return nil
			}))
			require.Equal(t, "条件？", cm.Query)
			require.Equal(t, tc.query, cm.RewriteQuery)
			e := r.queries[0]
			require.Equal(t, tc.status, e.Status)
			require.Equal(t, tc.query, e.Query)
			require.NotContains(t, e.ErrorCode, "secret")
			if tc.history && !tc.omitHistory {
				require.Equal(t, 1, e.HistoryTurns)
			} else {
				require.Zero(t, e.HistoryTurns)
			}
		})
	}
}

func TestRAGRetrievalCapturesQueryEvenWhenSearchFails(t *testing.T) {
	r := &queryOpsRecorder{}
	ctx := capture.WithRecorder(context.Background(), r)
	p := &PluginSearch{knowledgeBaseService: &failingSearchKnowledgeBaseService{err: errors.New("offline")}}
	cm := &types.ChatManage{PipelineRequest: types.PipelineRequest{Query: "原始提问", SearchTargets: types.SearchTargets{{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: "kb-1"}}}, PipelineState: types.PipelineState{RewriteQuery: "实际检索词"}}
	_, err := p.searchByTargets(ctx, cm)
	require.Error(t, err)
	require.NotEmpty(t, r.queries)
	require.Equal(t, "实际检索词", r.queries[0].Query)
	require.Equal(t, "search", r.queries[0].Kind)
}

type panickingQueryOpsRecorder struct{ opsRecorder }

func (*panickingQueryOpsRecorder) RecordQuery(context.Context, capture.QueryEvent) {
	panic("capture unavailable")
}

// Missing or failing capture must not alter the rewrite, intent, or downstream
// return value. This exercises the real pipeline, not just the observer helper.
func TestQueryObservationCannotChangePipelineOutcome(t *testing.T) {
	for _, tc := range []struct {
		name string
		rec  capture.Recorder
	}{
		{"disabled", nil},
		{"recorder without query support", &opsRecorder{}},
		{"enabled", &queryOpsRecorder{}},
		{"capture panics", &panickingQueryOpsRecorder{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := capture.WithRecorder(context.Background(), tc.rec)
			cm := &types.ChatManage{PipelineRequest: types.PipelineRequest{Query: "原始问题", EnableRewrite: true}}
			p := &PluginQueryUnderstand{
				config:       &config.Config{Conversation: &config.ConversationConfig{RewritePromptUser: "{{query}}"}},
				modelService: &stubModelService{model: &queryOpsModel{output: `{"rewrite_query":"实际检索问题","intent":"kb_search"}`}},
			}
			downstreamErr := &PluginError{}
			nextCalls := 0
			err := p.OnEvent(ctx, types.QUERY_UNDERSTAND, cm, func() *PluginError {
				nextCalls++
				require.Equal(t, "原始问题", cm.Query)
				require.Equal(t, "实际检索问题", cm.RewriteQuery)
				require.EqualValues(t, "kb_search", cm.Intent)
				return downstreamErr
			})
			require.Same(t, downstreamErr, err)
			require.Equal(t, 1, nextCalls)
		})
	}
}

type unavailableQueryHistory struct{ interfaces.MessageService }

func (unavailableQueryHistory) GetRecentMessagesBySession(context.Context, string, int) ([]*types.Message, error) {
	return nil, errors.New("history unavailable")
}

func TestQueryObservationRetainsHistoryFailureWithoutBlockingRewrite(t *testing.T) {
	r := &queryOpsRecorder{}
	ctx := capture.WithRecorder(context.Background(), r)
	cm := &types.ChatManage{PipelineRequest: types.PipelineRequest{Query: "原始问题", EnableRewrite: true, MaxRounds: 3}}
	p := &PluginQueryUnderstand{
		config:         &config.Config{Conversation: &config.ConversationConfig{RewritePromptUser: "{{conversation}}\n{{query}}"}},
		messageService: unavailableQueryHistory{},
		modelService:   &stubModelService{model: &queryOpsModel{output: `{"rewrite_query":"实际检索问题","intent":"kb_search"}`}},
	}
	require.Nil(t, p.OnEvent(ctx, types.QUERY_UNDERSTAND, cm, func() *PluginError { return nil }))
	require.Equal(t, "实际检索问题", cm.RewriteQuery)
	require.Len(t, r.queries, 1)
	require.Equal(t, "rewritten", r.queries[0].Status)
	require.Equal(t, "unavailable", r.queries[0].HistoryStatus)
	require.Zero(t, r.queries[0].HistoryTurns)
}
