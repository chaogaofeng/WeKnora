package capture

import (
	"context"
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
)

// SearchSources describes candidates only. The model-input hook separately
// identifies the exact rendered fragments the model actually received.
func SearchSources(results []*types.SearchResult, callID string) []Source {
	sources := make([]Source, 0, len(results))
	for _, r := range results {
		if r == nil {
			continue
		}
		kind := "chunk"
		chunkID := r.ID
		kbID, docID := r.KnowledgeBaseID, r.KnowledgeID
		if r.ChunkType == string(types.ChunkTypeWebSearch) || r.KnowledgeSource == "web_search" {
			kind = "web"
			chunkID = ""
			kbID = ""
			docID = ""
		}
		sources = append(sources, Source{ID: r.ID, Kind: kind, KBID: kbID, KnowledgeID: docID, ChunkID: chunkID, Content: r.Content, Title: r.KnowledgeTitle, Version: r.Metadata["version"], ToolCallID: callID})
	}
	return sources
}

// RecordToolResult consumes only explicitly supported provenance contracts.
// Arbitrary Data maps/tool args never enter the operations store.
func RecordToolResult(ctx context.Context, callID, name string, result *types.ToolResult) {
	if FromContext(ctx) == nil {
		return
	}
	if !supportedTool(name) {
		Gap(ctx, "unsupported_source:"+name)
		return
	}
	if result == nil || !result.Success {
		Gap(ctx, "tool_failed:"+name)
		return
	}
	sources := SearchSources(result.SearchResults, callID)
	for _, s := range result.SourceSnapshots {
		sources = append(sources, Source{ID: s.ID, Kind: s.Kind, KBID: s.KBID, KnowledgeID: s.KnowledgeID, ChunkID: s.ChunkID, PageID: s.PageID, Slug: s.Slug, Version: s.Version, Title: s.Title, ToolCallID: callID, Truncated: s.Truncated})
	}
	if len(sources) == 0 {
		rows := sourceRows(result.Data["results"])
		if len(rows) == 0 {
			rows = sourceRows(result.Data["chunks"])
		}
		for _, row := range rows {
			chunkID := field(row, "chunk_id")
			if chunkID == "" {
				chunkID = field(row, "faq_id")
			}
			kbID := field(row, "knowledge_base_id")
			if kbID == "" {
				kbID = field(row, "knowledge_base")
			}
			if kbID == "" {
				kbID = field(result.Data, "knowledge_base_id")
			}
			docID := field(row, "knowledge_id")
			if docID == "" {
				docID = field(result.Data, "knowledge_id")
			}
			kind, id := "chunk", chunkID
			if name == "web_search" || name == "web_fetch" {
				kind = "web"
				id = field(row, "url")
				chunkID = ""
			}
			if id == "" {
				continue
			}
			sources = append(sources, Source{ID: id, Kind: kind, KBID: kbID, KnowledgeID: docID, ChunkID: chunkID, Content: field(row, "content"), Title: field(row, "knowledge_title"), Version: field(row, "version"), ToolCallID: callID})
		}
	}
	// Wiki fragments are captured from the bounded result, rather than copying
	// whole current pages. An omitted page retains metadata only, as a candidate.
	for i := range sources {
		if sources[i].Slug != "" {
			for _, b := range sourceBlocks.FindAllString(result.Output, -1) {
				if strings.Contains(b, "[["+sources[i].Slug+"|") && (sources[i].KBID == "" || strings.Contains(b, "<knowledge_base_id>"+sources[i].KBID+"</knowledge_base_id>")) {
					sources[i].Content = b
					break
				}
			}
		}
	}
	RecordSources(ctx, sources)
	if len(sources) == 0 && strings.TrimSpace(result.Output) != "" {
		Gap(ctx, "source_provenance_unavailable:"+name)
	}
	if name == "wiki_read_page" && (strings.Contains(result.Output, "<links_to>[[") || strings.Contains(result.Output, "<linked_from>[[")) {
		Gap(ctx, "wiki_linked_summary_versions_unknown")
	}
}
func sourceRows(value any) []map[string]any {
	switch rows := value.(type) {
	case []map[string]any:
		return rows
	case []any:
		out := make([]map[string]any, 0, len(rows))
		for _, r := range rows {
			if m, ok := r.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	}
	return nil
}
func field(row map[string]any, key string) string {
	v := row[key]
	if v == nil {
		return ""
	}
	switch value := v.(type) {
	case string:
		return value
	case int:
		return fmt.Sprint(value)
	case float64:
		return fmt.Sprint(value)
	}
	return ""
}
