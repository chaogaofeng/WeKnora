package capture

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func summaryResult() *types.ToolResult {
	inline := "[[neighbor]] (摘要包括 [[other|其他概念]]，并非整个页面正文)"
	return &types.ToolResult{
		Success: true,
		Output:  fmt.Sprintf(`<wiki_page><knowledge_base_id>kb</knowledge_base_id><link>[[main|主页面]]</link><links_to>%s</links_to><content>主页面正文</content></wiki_page>`, inline),
		SourceSnapshots: []types.ToolSourceSnapshot{
			{Kind: "wiki", KBID: "kb", PageID: "main-id", Slug: "main", Version: "2"},
			{Kind: "wiki_summary", KBID: "kb", PageID: "neighbor-id", Slug: "neighbor", Version: "7", Title: "关联页", ParentPageID: "main-id", Rendered: inline, Truncated: true},
		},
	}
}

func TestWikiSummaryBindsOnlyActualMatchingInput(t *testing.T) {
	for _, tc := range []struct {
		name        string
		mutate      func(*types.ToolResult, *chat.Message)
		wantSources int
		wantGap     bool
	}{
		{name: "versioned inline summary", wantSources: 1},
		{name: "bare and omitted links before known summary", mutate: func(_ *types.ToolResult, m *chat.Message) {
			m.Content = strings.ReplaceAll(m.Content, "<links_to>", "<links_to>[[bare]], [[seen]] (summary omitted, already seen), ")
		}, wantSources: 1},
		{name: "truncated summary has unbalanced parentheses", mutate: func(r *types.ToolResult, m *chat.Message) {
			inline := "[[neighbor]] (摘要 (未结束的引用…)"
			m.Content = strings.ReplaceAll(m.Content, r.SourceSnapshots[1].Rendered, inline)
			r.SourceSnapshots[1].Rendered = inline
		}, wantSources: 1},
		{name: "summary omitted", mutate: func(r *types.ToolResult, m *chat.Message) {
			m.Content = strings.ReplaceAll(m.Content, r.SourceSnapshots[1].Rendered, "[[neighbor]]")
		}},
		{name: "page omitted", mutate: func(_ *types.ToolResult, m *chat.Message) { m.Content = "page omitted" }},
		{name: "summary changed", mutate: func(_ *types.ToolResult, m *chat.Message) {
			m.Content = strings.ReplaceAll(m.Content, "摘要包括", "不同摘要包括")
		}, wantGap: true},
		{name: "version missing", mutate: func(r *types.ToolResult, _ *chat.Message) { r.SourceSnapshots[1].Version = "" }, wantSources: 1, wantGap: true},
		{name: "wrong parent", mutate: func(r *types.ToolResult, _ *chat.Message) { r.SourceSnapshots[1].ParentPageID = "another-page" }, wantGap: true},
		{name: "wrong KB", mutate: func(r *types.ToolResult, _ *chat.Message) { r.SourceSnapshots[1].KBID = "other-kb" }, wantGap: true},
		{name: "missing snapshot", mutate: func(r *types.ToolResult, _ *chat.Message) { r.SourceSnapshots = r.SourceSnapshots[:1] }, wantGap: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &memoryRecorder{}
			ctx := WithRecorder(context.Background(), r)
			result := summaryResult()
			m := chat.Message{Role: "tool", Name: "wiki_read_page", ToolCallID: "call", Content: result.Output}
			if tc.mutate != nil {
				tc.mutate(result, &m)
			}
			RecordToolResult(ctx, "call", "wiki_read_page", result)
			for _, source := range r.sources {
				require.NotEqual(t, "wiki_summary", source.Kind, "unconsumed summaries must not become stored evidence")
			}
			RecordModelInput(ctx, []chat.Message{m}, nil)
			var summaries []Source
			for _, source := range r.sources {
				if source.Kind == "wiki_summary" && source.SentToModel {
					summaries = append(summaries, source)
				}
			}
			require.Len(t, summaries, tc.wantSources)
			require.Equal(t, tc.wantGap, slices.Contains(r.gaps, "wiki_linked_summary_versions_unknown"), "%v", r.gaps)
			if len(summaries) > 0 {
				require.Equal(t, "neighbor-id", summaries[0].PageID)
				require.Equal(t, result.SourceSnapshots[1].Rendered, summaries[0].Content)
				require.NotContains(t, summaries[0].Content, "主页面正文")
				require.True(t, summaries[0].Truncated)
				require.False(t, summaries[0].PostHoc)
			}
		})
	}
}

func TestWikiSummaryVersionsStayWithTheirToolCallAfterContextEncoding(t *testing.T) {
	r := &memoryRecorder{}
	ctx := WithRecorder(context.Background(), r)
	encode := strings.NewReplacer("main", "w1", "neighbor", "w2", "kb", "k1").Replace
	var messages []chat.Message
	for _, version := range []string{"7", "8"} {
		result := summaryResult()
		result.SourceSnapshots[1].Version = version
		callID := "call-" + version
		RecordToolResult(ctx, callID, "wiki_read_page", result)
		messages = append(messages, chat.Message{Role: "tool", Name: "wiki_read_page", ToolCallID: callID, Content: encode(result.Output)})
	}
	RecordModelInput(ctx, messages, encode)
	versions := map[string]string{}
	for _, source := range r.sources {
		if source.Kind == "wiki_summary" && source.SentToModel {
			versions[source.ToolCallID] = source.Version
			require.Equal(t, encode(summaryResult().SourceSnapshots[1].Rendered), source.Content)
		}
	}
	require.Equal(t, map[string]string{"call-7": "7", "call-8": "8"}, versions)
	require.Empty(t, r.gaps)
}

func TestWikiSummariesCanContainEachOthersRenderedText(t *testing.T) {
	for _, includeA := range []bool{true, false} {
		r := &memoryRecorder{}
		ctx := WithRecorder(context.Background(), r)
		result := summaryResult()
		a, b := "[[a]] (A)", "[[b]] (见 [[a]] (A))"
		links := b
		if includeA {
			links = a + ", " + b
		}
		result.Output = `<wiki_page><knowledge_base_id>kb</knowledge_base_id><link>[[main|主页面]]</link><links_to>` + links + `</links_to></wiki_page>`
		result.SourceSnapshots = []types.ToolSourceSnapshot{
			result.SourceSnapshots[0],
			{Kind: "wiki_summary", KBID: "kb", ParentPageID: "main-id", PageID: "a", Slug: "a", Version: "1", Rendered: a},
			{Kind: "wiki_summary", KBID: "kb", ParentPageID: "main-id", PageID: "b", Slug: "b", Version: "2", Rendered: b},
		}
		RecordToolResult(ctx, "call", "wiki_read_page", result)
		RecordModelInput(ctx, []chat.Message{{Role: "tool", Name: "wiki_read_page", ToolCallID: "call", Content: result.Output}}, nil)
		got := map[string]string{}
		for _, source := range r.sources {
			if source.Kind == "wiki_summary" && source.SentToModel {
				got[source.PageID] = source.Content
			}
		}
		want := map[string]string{"b": b}
		if includeA {
			want["a"] = a
		}
		require.Equal(t, want, got)
		require.Empty(t, r.gaps)
	}
}

func TestWikiSummaryStopsBindingAfterAnUnmatchedOuterSummary(t *testing.T) {
	r := &memoryRecorder{}
	ctx := WithRecorder(context.Background(), r)
	result := summaryResult()
	result.Output = `<wiki_page><knowledge_base_id>kb</knowledge_base_id><link>[[main|主页面]]</link><links_to>[[known]] (Known), [[b]] (changed, [[a]] (A), end), [[c]] (C)</links_to></wiki_page>`
	result.SourceSnapshots = []types.ToolSourceSnapshot{
		result.SourceSnapshots[0],
		{Kind: "wiki_summary", KBID: "kb", ParentPageID: "main-id", PageID: "known", Version: "4", Rendered: "[[known]] (Known)"},
		{Kind: "wiki_summary", KBID: "kb", ParentPageID: "main-id", PageID: "a", Version: "1", Rendered: "[[a]] (A)"},
		{Kind: "wiki_summary", KBID: "kb", ParentPageID: "main-id", PageID: "b", Version: "2", Rendered: "[[b]] (old, [[a]] (A), end)"},
		{Kind: "wiki_summary", KBID: "kb", ParentPageID: "main-id", PageID: "c", Version: "3", Rendered: "[[c]] (C)"},
	}
	RecordToolResult(ctx, "call", "wiki_read_page", result)
	RecordModelInput(ctx, []chat.Message{{Role: "tool", Name: "wiki_read_page", ToolCallID: "call", Content: result.Output}}, nil)
	var pages []string
	for _, source := range r.sources {
		if source.Kind == "wiki_summary" && source.SentToModel {
			pages = append(pages, source.PageID)
		}
	}
	// Summary prose can contain arbitrary punctuation. Once B's boundary is
	// uncertain, neither its nested A nor any later entry may be inferred.
	require.Equal(t, []string{"known"}, pages)
	require.Contains(t, r.gaps, "wiki_linked_summary_versions_unknown")
}
