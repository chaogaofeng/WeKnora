package chatpipeline

import (
	"context"
	"github.com/Tencent/WeKnora/internal/knowledgeops/capture"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestKnowledgeOpsKeepsParentAndGrandparentPermissionBindings(t *testing.T) {
	for _, kind := range []types.ChunkType{types.ChunkTypeText, types.ChunkTypeImageOCR} {
		t.Run(string(kind), func(t *testing.T) {
			r := &opsRecorder{}
			ctx := capture.WithRecorder(types.WithExecutionTenant(context.Background(), 1), r)
			repo := &expandChunkRepo{chunks: map[string]*types.Chunk{
				"text":   {ID: "text", KnowledgeID: "doc", KnowledgeBaseID: "kb", ParentChunkID: "parent", ChunkType: types.ChunkTypeText, Content: "text body"},
				"parent": {ID: "parent", KnowledgeID: "doc", KnowledgeBaseID: "kb", ChunkType: types.ChunkTypeParentText, Content: "parent body"},
			}}
			hit := &types.SearchResult{ID: "hit", KnowledgeID: "doc", KnowledgeBaseID: "kb", ParentChunkID: "parent", ChunkType: string(kind), Content: "hit body"}
			if kind == types.ChunkTypeImageOCR {
				hit.ParentChunkID = "text"
			}
			(&PluginMerge{chunkRepo: repo}).resolveParentChunks(ctx, &types.ChatManage{}, []*types.SearchResult{hit})
			ids := []string{}
			for _, s := range r.sources {
				ids = append(ids, s.ChunkID)
				require.Empty(t, s.Content)
				require.False(t, s.SentToModel)
			}
			require.Contains(t, ids, "parent")
			if kind == types.ChunkTypeImageOCR {
				require.Contains(t, ids, "text")
			}
		})
	}
}

func TestKnowledgeOpsKeepsTransitiveMergedPermissionBindings(t *testing.T) {
	r := &opsRecorder{}
	ctx := capture.WithRecorder(context.Background(), r)
	first := &types.SearchResult{ID: "c1", KnowledgeID: "doc", KnowledgeBaseID: "kb", ChunkIndex: 1, Content: "first"}
	next := &types.SearchResult{ID: "c2", KnowledgeID: "doc", KnowledgeBaseID: "kb", ChunkIndex: 2, Content: "second plus neighbor", SubChunkID: []string{"c3"}}
	got := (&PluginMerge{}).mergeSequentialChunks(ctx, "doc", []*types.SearchResult{first, next})
	require.Len(t, got, 1)
	ids := []string{}
	for _, s := range r.sources {
		ids = append(ids, s.ChunkID)
		require.Empty(t, s.Content)
		require.False(t, s.SentToModel)
	}
	require.Contains(t, ids, "c2")
	require.Contains(t, ids, "c3")
}
