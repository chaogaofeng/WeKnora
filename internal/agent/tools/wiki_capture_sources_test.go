package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestWikiCaptureSnapshotPreservesReadVersionAndDoesNotSerialize(t *testing.T) {
	page := newTestWikiPage("kb", "concept/topic")
	page.Version = 12
	page.ID = "page-id"
	page.Content = "full body"
	tool := NewWikiReadPageTool(&fakeWikiPageService{pages: map[string]*types.WikiPage{wikiPageKey("kb", page.Slug): page}}, nil, NewWikiScopesFromKBIDs([]string{"kb"}), nil)
	result := readPageOutput(t, context.Background(), tool, []string{page.Slug})
	require.Len(t, result.SourceSnapshots, 1)
	source := result.SourceSnapshots[0]
	require.Equal(t, "12", source.Version)
	require.Equal(t, "page-id", source.PageID)
	require.Equal(t, "kb", source.KBID)
	require.False(t, source.Truncated)
	raw, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "page-id")
	require.NotContains(t, string(raw), "SourceSnapshots")
}

func TestWikiCaptureIncludesInlineSummaryReadVersion(t *testing.T) {
	page := newTestWikiPage("kb", "concept/main")
	page.ID, page.Version = "main-id", 2
	page.OutLinks = []string{"concept/neighbor"}
	neighbor := newTestWikiPage("kb", "concept/neighbor")
	neighbor.ID, neighbor.Version, neighbor.Summary = "neighbor-id", 7, "关联页摘要"
	svc := &fakeWikiPageService{pages: map[string]*types.WikiPage{
		wikiPageKey("kb", page.Slug):     page,
		wikiPageKey("kb", neighbor.Slug): neighbor,
	}}
	tool := NewWikiReadPageTool(svc, nil, NewWikiScopesFromKBIDs([]string{"kb"}), nil)
	result := readPageOutput(t, context.Background(), tool, []string{page.Slug})
	require.Contains(t, result.Output, "[[concept/neighbor]] (关联页摘要)")
	require.Len(t, result.SourceSnapshots, 2)
	neighbor.Version = 8 // Later edits must not change the version of the read snapshot.
	require.Equal(t, "wiki_summary", result.SourceSnapshots[1].Kind)
	require.Equal(t, "neighbor-id", result.SourceSnapshots[1].PageID)
	require.Equal(t, "7", result.SourceSnapshots[1].Version)
	require.Equal(t, "main-id", result.SourceSnapshots[1].ParentPageID)
	require.Equal(t, "[[concept/neighbor]] (关联页摘要)", result.SourceSnapshots[1].Rendered)
	require.Len(t, svc.getCalls, 2, "capture must not re-fetch pages")
	raw, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "neighbor-id")
	again := readPageOutput(t, context.Background(), tool, []string{page.Slug})
	require.Contains(t, again.Output, "summary omitted, already seen")
	require.Len(t, again.SourceSnapshots, 1, "omitted summaries carry no new read snapshot")
}
