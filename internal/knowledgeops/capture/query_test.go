package capture

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type queryMemoryRecorder struct {
	memoryRecorder
	events []QueryEvent
}

func (r *queryMemoryRecorder) RecordQuery(_ context.Context, event QueryEvent) {
	r.events = append(r.events, event)
}

func TestQueryCaptureSanitizesAndBoundsWithoutChangingEvidenceCoverage(t *testing.T) {
	r := &queryMemoryRecorder{}
	ctx := WithRecorder(context.Background(), r)
	RecordQuery(ctx, QueryEvent{Kind: "rewrite", Status: "rewritten", Query: "api_key=private-value " + strings.Repeat("中", 6000)})
	require.Len(t, r.events, 1)
	require.NotContains(t, r.events[0].Query, "private-value")
	require.True(t, r.events[0].Redacted)
	require.True(t, r.events[0].Truncated)
	require.LessOrEqual(t, len([]rune(r.events[0].Query)), 4000)
	require.Empty(t, r.gaps, "query metadata loss must not become a source-body capture gap")
	RecordQuery(context.Background(), QueryEvent{Kind: "search", Query: "disabled"})
	RecordQuery(WithRecorder(context.Background(), &memoryRecorder{}), QueryEvent{Kind: "search", Query: "older recorder"})
}

func TestRetrievalCaptureKeepsOnlyAllowlistedDecisionMetadata(t *testing.T) {
	r := &queryMemoryRecorder{}
	ctx := WithRecorder(context.Background(), r)
	RecordQuery(ctx, QueryEvent{Kind: "retrieval", Status: "skipped", Intent: "follow_up", Reason: "context_only_follow_up", Query: "unrelated input", Tool: "private", ModelID: "private"})
	require.Equal(t, []QueryEvent{{Kind: "retrieval", Status: "skipped", Intent: "follow_up", Reason: "context_only_follow_up"}}, r.events)
	RecordQuery(ctx, QueryEvent{Kind: "retrieval", Status: "required", Intent: strings.Repeat("x", 40000), Reason: strings.Repeat("x", 40000)})
	require.Equal(t, QueryEvent{Kind: "retrieval", Status: "required"}, r.events[1])
	RecordQuery(ctx, QueryEvent{Kind: "retrieval", Status: "invented"})
	require.Len(t, r.events, 2)
}

func TestToolQueryCaptureKeepsOnlySearchInputsAndRetainsRepeatedCalls(t *testing.T) {
	r := &queryMemoryRecorder{}
	ctx := WithRecorder(context.Background(), r)
	RecordToolQueries(ctx, "one", "wiki_search", map[string]any{"query": []any{" 上海 ", "北京", " 上海 "}, "queries": "北京", "reasoning": "private thought", "password": "hidden"}, 2)
	RecordToolQueries(ctx, "two", "wiki_search", map[string]any{"query": "上海"}, 3)
	RecordToolQueries(ctx, "sql", "database_query", map[string]any{"query": "secret sql"}, 3)
	require.Len(t, r.events, 3)
	require.Equal(t, []string{" 上海 ", "北京", "上海"}, []string{r.events[0].Query, r.events[1].Query, r.events[2].Query})
	require.Equal(t, "two", r.events[2].ToolCallID)
	require.Equal(t, 3, r.events[2].Round)
	require.Equal(t, "attempted", r.events[2].Status)
}
