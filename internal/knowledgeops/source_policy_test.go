package knowledgeops

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSourceLookupPolicyAcceptsBoundedDeploymentOverrides(t *testing.T) {
	t.Setenv("KNOWLEDGE_OPS_SOURCE_MAX_CHARS", "64000")
	t.Setenv("KNOWLEDGE_OPS_SOURCE_MAX_CHUNKS_PER_DOCUMENT", "48")
	t.Setenv("KNOWLEDGE_OPS_SOURCE_MAX_DOCUMENTS_PER_PAGE", "12")
	t.Setenv("KNOWLEDGE_OPS_SOURCE_MAX_LINKED_PAGES", "64")
	p := SourceLookupPolicyFromEnv()
	require.Equal(t, SourceLookupPolicy{MaxChars: 64000, MaxChunksPerDocument: 48, MaxDocumentsPerPage: 12, MaxLinkedPages: 64}, p)
	for _, invalid := range []string{"oops", "-1", "999999999"} {
		t.Setenv("KNOWLEDGE_OPS_SOURCE_MAX_CHARS", invalid)
		t.Setenv("KNOWLEDGE_OPS_SOURCE_MAX_CHUNKS_PER_DOCUMENT", invalid)
		t.Setenv("KNOWLEDGE_OPS_SOURCE_MAX_DOCUMENTS_PER_PAGE", invalid)
		t.Setenv("KNOWLEDGE_OPS_SOURCE_MAX_LINKED_PAGES", invalid)
		require.Equal(t, DefaultSourceLookupPolicy(), SourceLookupPolicyFromEnv())
	}
}
