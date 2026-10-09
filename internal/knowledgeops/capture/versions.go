package capture

import (
	"context"
	"encoding/json"
	"maps"

	"github.com/Tencent/WeKnora/internal/types"
)

func revisionVersion(revisions map[string]int) string {
	if len(revisions) == 0 || len(revisions) > 512 {
		return "unknown"
	}
	for id, revision := range revisions {
		if id == "" || revision < 0 {
			return "unknown"
		}
	}
	body, _ := json.Marshal(revisions) // Stable ordering; source IDs remain inspectable.
	return "chunks:" + string(body)
}

// SearchVersion describes retrieval-time chunk revisions, including merged contributors.
func SearchVersion(result *types.SearchResult) string {
	if result == nil {
		return "unknown"
	}
	if len(result.SourceRevisions) == 0 && !result.ContentRewritten && len(result.SubChunkID) == 0 && result.Metadata["version"] != "" {
		return result.Metadata["version"]
	}
	return revisionVersion(resultRevisions(result))
}

func resultRevisions(result *types.SearchResult) map[string]int {
	revisions := maps.Clone(result.SourceRevisions)
	if revisions == nil {
		revisions = map[string]int{}
	}
	for _, id := range append([]string{result.ID}, result.SubChunkID...) {
		if _, known := revisions[id]; !known {
			revisions[id] = -1
		}
	}
	return revisions
}

func mergeSourceRevisions(target, source *types.SearchResult) {
	if target == nil || source == nil {
		return
	}
	revisions := resultRevisions(target)
	for id, revision := range resultRevisions(source) {
		if old, found := revisions[id]; found && old != revision {
			revision = -1 // Mixed/unknown revisions of one chunk cannot be claimed as exact.
		}
		revisions[id] = revision
	}
	if len(revisions) > 512 {
		revisions = map[string]int{target.ID: -1}
	}
	target.SourceRevisions = revisions
}

// RecordChunkExpansion carries the actual parent/neighbor revisions alongside
// their existing permission bindings; it never fetches current chunks later.
func RecordChunkExpansion(ctx context.Context, result *types.SearchResult, chunks ...*types.Chunk) {
	for _, chunk := range chunks {
		if chunk != nil {
			mergeSourceRevisions(result, &types.SearchResult{ID: chunk.ID, SourceRevisions: map[string]int{chunk.ID: chunk.ContentRevision}})
		}
	}
	RecordChunkDependencies(ctx, chunks...)
}

// RecordChunkReplacement is used when the body is entirely rebuilt from freshly
// read chunks (FAQ or short-context expansion), rather than appended to an old hit.
func RecordChunkReplacement(ctx context.Context, result *types.SearchResult, chunks ...*types.Chunk) {
	result.SourceRevisions = map[string]int{}
	for _, chunk := range chunks {
		if chunk != nil {
			result.SourceRevisions[chunk.ID] = chunk.ContentRevision
		}
	}
	RecordChunkDependencies(ctx, chunks...)
}

// ChunkVersion is only for a chunk read alongside the content being returned.
func ChunkVersion(chunk *types.Chunk) string {
	if chunk == nil {
		return "unknown"
	}
	return revisionVersion(map[string]int{chunk.ID: chunk.ContentRevision})
}
