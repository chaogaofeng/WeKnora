package service

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/knowledgeops/capture"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestRetrievedChunkRevisionReachesKnowledgeOps(t *testing.T) {
	for _, revision := range []int{0, 3} {
		svc := &knowledgeBaseService{}
		result := svc.buildSearchResult(&types.Chunk{ID: "c", KnowledgeID: "doc", KnowledgeBaseID: "kb", ContentRevision: revision}, &types.Knowledge{ID: "doc", KnowledgeBaseID: "kb"}, 1, types.MatchTypeEmbedding, "")
		sources := capture.SearchSources([]*types.SearchResult{result}, "")
		require.Len(t, sources, 1)
		if revision == 0 {
			require.Equal(t, `chunks:{"c":0}`, sources[0].Version)
		} else {
			require.Equal(t, `chunks:{"c":3}`, sources[0].Version)
		}
	}
}
