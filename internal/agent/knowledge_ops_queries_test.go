package agent

import (
	"context"
	"testing"

	agenttools "github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/knowledgeops/capture"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type opsQueryRecorder struct {
	opsInputRecorder
	queries []capture.QueryEvent
}

func (r *opsQueryRecorder) RecordQuery(_ context.Context, e capture.QueryEvent) {
	r.queries = append(r.queries, e)
}

func TestAgentQueryCaptureRunsOnlyAtExecutionAndKeepsCallIdentity(t *testing.T) {
	r := &opsQueryRecorder{}
	ctx := capture.WithRecorder(context.Background(), r)
	e := newTestEngine(t, &mockChat{})
	e.toolRegistry = agenttools.NewToolRegistry()
	tool := newCountingTool("wiki_search")
	e.toolRegistry.RegisterTool(tool)
	call := types.LLMToolCall{ID: "first", Function: types.FunctionCall{Name: "wiki_search", Arguments: `{"query":["条件 ","条件"],"reasoning":"not retained"}`}}
	result := e.runToolCall(ctx, call, 0, 0, 1, "s", "m")
	require.True(t, result.Result.Success)
	require.Len(t, r.queries, 2)
	require.Equal(t, "条件 ", r.queries[0].Query)
	require.Equal(t, "first", r.queries[0].ToolCallID)
	require.Equal(t, 1, r.queries[0].Round)
	call.ID = "blocked"
	call.UnresolvedHandles = []string{"d99"}
	e.runToolCall(ctx, call, 0, 1, 2, "s", "m")
	require.Len(t, r.queries, 2, "blocked calls must not be reported as executed retrieval")
}
