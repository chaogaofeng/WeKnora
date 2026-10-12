package knowledgeops

import (
	"context"
	"os"
	"strconv"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
)

const sourceLookupTimeout = 15 * time.Second
const sourceChunkBatchSize = 8
const sourceSelectionStrategy = "references_then_relevance_with_neighbors_v1"

// SourceLookupPolicy is deployment configuration, separate from answer-time
// capture settings. Persist a snapshot with each diagnosis, never with knowledge.
type SourceLookupPolicy struct {
	MaxChars             int `json:"max_chars"`
	MaxChunksPerDocument int `json:"max_chunks_per_document"`
	MaxDocumentsPerPage  int `json:"max_documents_per_page"`
	MaxLinkedPages       int `json:"max_linked_pages"`
}

func DefaultSourceLookupPolicy() SourceLookupPolicy {
	return SourceLookupPolicy{MaxChars: 48000, MaxChunksPerDocument: 32, MaxDocumentsPerPage: 8, MaxLinkedPages: 32}
}

func (p SourceLookupPolicy) normalized() SourceLookupPolicy {
	d := DefaultSourceLookupPolicy()
	bound := func(v, fallback, lo, hi int) int {
		if v < lo || v > hi {
			return fallback
		}
		return v
	}
	p.MaxChars = bound(p.MaxChars, d.MaxChars, 2000, 120000)
	p.MaxChunksPerDocument = bound(p.MaxChunksPerDocument, d.MaxChunksPerDocument, 2, 128)
	p.MaxDocumentsPerPage = bound(p.MaxDocumentsPerPage, d.MaxDocumentsPerPage, 1, 32)
	p.MaxLinkedPages = bound(p.MaxLinkedPages, d.MaxLinkedPages, 1, 128)
	return p
}

func SourceLookupPolicyFromEnv() SourceLookupPolicy {
	p := DefaultSourceLookupPolicy()
	for key, target := range map[string]*int{
		"KNOWLEDGE_OPS_SOURCE_MAX_CHARS":               &p.MaxChars,
		"KNOWLEDGE_OPS_SOURCE_MAX_CHUNKS_PER_DOCUMENT": &p.MaxChunksPerDocument,
		"KNOWLEDGE_OPS_SOURCE_MAX_DOCUMENTS_PER_PAGE":  &p.MaxDocumentsPerPage,
		"KNOWLEDGE_OPS_SOURCE_MAX_LINKED_PAGES":        &p.MaxLinkedPages,
	} {
		if raw := os.Getenv(key); raw != "" {
			v, err := strconv.Atoi(raw)
			if err != nil {
				logger.Warnf(context.Background(), "Invalid %s; using default source lookup limit", key)
				continue
			}
			*target = v
		}
	}
	normalized := p.normalized()
	if normalized != p {
		logger.Warnf(context.Background(), "Out-of-range knowledge-ops source lookup limits replaced by defaults")
	}
	return normalized
}
