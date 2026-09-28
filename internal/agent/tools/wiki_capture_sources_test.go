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
