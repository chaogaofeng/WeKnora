package modelcontext

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestEvidenceTextEncodingDoesNotAuthorizeCandidates(t *testing.T) {
	r := NewRegistry(true)
	raw := `<web url="https://example.com/source"/> <kb chunk_id="candidate-chunk"/>`
	encoded := r.CompactEvidenceText(raw)
	require.Equal(t, `<ref id="w1"/> <ref id="c1"/>`, encoded)
	require.Equal(t, " ", r.DecodeOutputText(encoded), "capture matching cannot grant citation eligibility")
	actual := r.ModelToolResultForTool("wiki_read_page", &types.ToolResult{Success: true, Output: raw})
	require.Equal(t, encoded, actual)
	require.Contains(t, r.DecodeOutputText(actual), `chunk_id="candidate-chunk"`)
	require.Contains(t, r.DecodeOutputText(actual), `url="https://example.com/source"`)
}
