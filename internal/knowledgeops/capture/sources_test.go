package capture

import (
	"context"
	"testing"

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
