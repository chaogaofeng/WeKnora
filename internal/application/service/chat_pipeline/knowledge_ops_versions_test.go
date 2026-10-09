package chatpipeline

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/knowledgeops/capture"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestMergedSourceVersionsKeepEveryContributingRevision(t *testing.T) {
	ctx := capture.WithRecorder(context.Background(), &opsRecorder{})
	k := &types.Knowledge{ID: "doc", KnowledgeBaseID: "kb"}
	first := chunk2SearchResult(&types.Chunk{ID: "first", KnowledgeID: "doc", KnowledgeBaseID: "kb", ContentRevision: 0}, k)
	second := chunk2SearchResult(&types.Chunk{ID: "second", KnowledgeID: "doc", KnowledgeBaseID: "kb", ContentRevision: 3}, k)
	recordMergedChild(ctx, "doc", first, second, "test")
	require.Equal(t, `chunks:{"first":0,"second":3}`, capture.SearchSources([]*types.SearchResult{first}, "")[0].Version)
	// Persisted references have no retrieval-time revision contract. Never infer
	// revision zero from a default int, or use today's revision for old content.
	data, err := json.Marshal(second)
	require.NoError(t, err)
	var restored types.SearchResult
	require.NoError(t, json.Unmarshal(data, &restored))
	recordMergedChild(ctx, "doc", first, &restored, "test")
	require.Equal(t, "unknown", capture.SearchSources([]*types.SearchResult{first}, "")[0].Version)
}

func TestParentAndNeighborExpansionRetainsVersionsAndPermissionBindings(t *testing.T) {
	k := &types.Knowledge{ID: "doc", KnowledgeBaseID: "kb"}
	base := &types.Chunk{ID: "base", KnowledgeID: "doc", KnowledgeBaseID: "kb", Content: "base body", ContentRevision: 1, ChunkType: types.ChunkTypeText, ParentChunkID: "parent"}
	parent := &types.Chunk{ID: "parent", KnowledgeID: "doc", KnowledgeBaseID: "kb", Content: "parent body", ContentRevision: 2, ChunkType: types.ChunkTypeParentText}
	rec := &opsRecorder{}
	ctx := capture.WithRecorder(context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1)), rec)
	p := &PluginMerge{chunkRepo: &expandChunkRepo{chunks: map[string]*types.Chunk{"parent": parent}}}
	out := p.resolveParentChunks(ctx, &types.ChatManage{}, []*types.SearchResult{chunk2SearchResult(base, k)})
	require.Equal(t, `chunks:{"base":1,"parent":2}`, capture.SearchSources(out, "")[0].Version)
	require.Contains(t, out[0].Content, "parent body")
	require.Contains(t, out[0].Content, "base body")
	require.Equal(t, "parent", rec.sources[0].ChunkID)

	base.ParentChunkID, base.NextChunkID = "", "next"
	next := &types.Chunk{ID: "next", KnowledgeID: "doc", KnowledgeBaseID: "kb", Content: "next body", ContentRevision: 3, ChunkType: types.ChunkTypeText}
	p.chunkRepo = &expandChunkRepo{chunks: map[string]*types.Chunk{"base": base, "next": next}}
	out = p.expandShortContextWithNeighbors(ctx, &types.ChatManage{}, []*types.SearchResult{chunk2SearchResult(base, k)})
	require.Equal(t, `chunks:{"base":1,"next":3}`, capture.SearchSources(out, "")[0].Version)
	require.Contains(t, out[0].Content, "next body")
	found := false
	for _, source := range rec.sources {
		if source.ChunkID == "next" && source.Kind == "source_dependency" {
			found = true
		}
	}
	require.True(t, found, "newly included neighbor content must retain a permission binding")
}
