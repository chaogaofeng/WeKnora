package capture

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

func TestToolCandidatesKeepSourceProvenanceAndUnknownVersion(t *testing.T) {
	r := &memoryRecorder{}
	ctx := WithRecorder(context.Background(), r)
	RecordToolResult(ctx, "call", "read_document", &types.ToolResult{Success: true, Data: map[string]any{
		"knowledge_id": "doc", "knowledge_base_id": "kb", "chunks": []map[string]any{{"chunk_id": "chunk", "content": "raw candidate"}},
	}})
	if len(r.sources) != 1 || r.sources[0].SentToModel || r.sources[0].Version != "unknown" || r.sources[0].KnowledgeID != "doc" || r.sources[0].KBID != "kb" {
		t.Fatalf("wrong candidates: %+v", r.sources)
	}
}
func TestUnknownToolDataNeverBecomesEvidence(t *testing.T) {
	r := &memoryRecorder{}
	ctx := WithRecorder(context.Background(), r)
	RecordToolResult(ctx, "call", "external_tool", &types.ToolResult{Success: true, Data: map[string]any{"results": []map[string]any{{"chunk_id": "secret", "content": "credential"}}}})
	if len(r.sources) != 0 || len(r.gaps) == 0 {
		t.Fatalf("unsupported source falsely captured: %+v", r)
	}
}

func TestReadDocumentActualDataContractCarriesKB(t *testing.T) {
	r := &memoryRecorder{}
	ctx := WithRecorder(context.Background(), r)
	RecordToolResult(ctx, "call", "read_document", &types.ToolResult{Success: true, Data: map[string]any{"knowledge_id": "doc", "chunks": []map[string]any{{"chunk_id": "chunk", "knowledge_base": "kb", "content": "body"}}}})
	if len(r.sources) != 1 || r.sources[0].KBID != "kb" {
		t.Fatalf("missing read_document KB provenance: %+v", r.sources)
	}
}

func TestWikiSummaryGapRequiresAnActuallyRenderedSummary(t *testing.T) {
	for _, tc := range []struct {
		name, links string
		send, want  bool
	}{
		{"unconsumed tool result", "[[neighbor]] (context summary)", false, false},
		{"bare link", "[[neighbor]]", true, false},
		{"already seen", "[[neighbor]] (summary omitted, already seen)", true, false},
		{"several omitted links", "[[a]] (summary omitted, already seen), [[b]] (summary omitted, already seen), [[c]]", true, false},
		{"mixed links", "[[a]] (summary omitted, already seen), [[b]] (context summary)", true, true},
		{"multiline summary", "[[neighbor]] (first line\nsecond line)", true, true},
		{"inline summary", "[[neighbor]] (context summary)", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &memoryRecorder{}
			ctx := WithRecorder(context.Background(), r)
			body := fmt.Sprintf(`<wiki_page><knowledge_base_id>kb</knowledge_base_id><link>[[concept/main|Main]]</link><content>body</content><links_to>%s</links_to></wiki_page>`, tc.links)
			RecordToolResult(ctx, "call", "wiki_read_page", &types.ToolResult{Success: true, Output: body, SourceSnapshots: []types.ToolSourceSnapshot{{Kind: "wiki", KBID: "kb", PageID: "page", Slug: "concept/main", Version: "1"}}})
			if tc.send {
				RecordModelInput(ctx, []chat.Message{{Role: "tool", Name: "wiki_read_page", ToolCallID: "call", Content: body}}, nil)
			}
			if got := slices.Contains(r.gaps, "wiki_linked_summary_versions_unknown"); got != tc.want {
				t.Fatalf("gap=%v, want %v; gaps=%v", got, tc.want, r.gaps)
			}
		})
	}
}

func TestWikiSummaryOmittedFromActualContextDoesNotCreateVersionGap(t *testing.T) {
	r := &memoryRecorder{}
	ctx := WithRecorder(context.Background(), r)
	body := `<wiki_page><knowledge_base_id>kb</knowledge_base_id><link>[[main|Main]]</link><content>body</content><linked_from>[[neighbor]] (context summary)</linked_from></wiki_page>`
	RecordToolResult(ctx, "call", "wiki_read_page", &types.ToolResult{Success: true, Output: body, SourceSnapshots: []types.ToolSourceSnapshot{{Kind: "wiki", KBID: "kb", PageID: "page", Slug: "main", Version: "1"}}})
	pruned := strings.Replace(body, "[[neighbor]] (context summary)", "[[neighbor]]", 1)
	RecordModelInput(ctx, []chat.Message{{Role: "tool", Name: "wiki_read_page", ToolCallID: "call", Content: pruned}}, nil)
	if slices.Contains(r.gaps, "wiki_linked_summary_versions_unknown") {
		t.Fatal("a summary removed before the model call cannot introduce a version gap")
	}
	RecordModelInput(ctx, []chat.Message{{Role: "tool", Name: "wiki_read_page", ToolCallID: "call", Content: body}}, nil)
	if !slices.Contains(r.gaps, "wiki_linked_summary_versions_unknown") {
		t.Fatal("an inbound summary actually sent on a later round must retain its version limitation")
	}
}
