package tools

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestAgentSearchAndReadCarryActualChunkRevisions(t *testing.T) {
	tool := &SearchKnowledgeTool{}
	result := tool.formatOutput(context.Background(), []*searchResultWithMeta{{SearchResult: &types.SearchResult{ID: "c", Content: "A", SourceRevisions: map[string]int{"c": 0}}, KnowledgeBaseID: "kb"}}, []string{"kb"}, "Q", SearchModeHybrid)
	rows := result.Data["results"].([]map[string]interface{})
	require.Equal(t, `chunks:{"c":0}`, rows[0]["version"])
	read := chunkDataMap(1, &types.Chunk{ID: "c", ContentRevision: 4})
	require.Equal(t, `chunks:{"c":4}`, read["version"])
}
