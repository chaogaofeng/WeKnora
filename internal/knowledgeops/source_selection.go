package knowledgeops

import (
	"context"
	"strings"
	"unicode"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// These terms only choose inspection order; matching words never establish
// source consistency. Keep the SQL portable and all data in bound parameters.
func sourceSearchTerms(run Run) []string {
	terms, seen := []string{}, map[string]bool{}
	for _, text := range []string{run.Question, run.Answer} {
		words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
		candidates := []string{}
		for _, word := range words {
			runes := []rune(word)
			for start := 0; start < len(runes); {
				end := start + 1
				han := unicode.Is(unicode.Han, runes[start])
				for end < len(runes) && unicode.Is(unicode.Han, runes[end]) == han {
					end++
				}
				if han {
					for i := start; i+1 < end; i++ {
						candidates = append(candidates, string(runes[i:i+2]))
					}
				} else if end-start >= 2 {
					candidates = append(candidates, string(runes[start:end]))
				}
				start = end
			}
		}
		unique := []string{}
		for _, term := range candidates {
			if seen[term] || strings.Contains("|什么|哪些|如何|一下|介绍|以及|是否|怎么|", "|"+term+"|") {
				continue
			}
			seen[term] = true
			unique = append(unique, term)
		}
		// Sample across the answer, so later claims are not all discarded.
		for i, n := 0, min(16, len(unique)); i < n; i++ {
			terms = append(terms, unique[i*len(unique)/n])
		}
	}
	return terms
}

func sourceChunkOrder(terms []string) clause.OrderBy {
	parts, args := []string{}, []any{}
	for _, term := range terms {
		parts = append(parts, "CASE WHEN LOWER(content) LIKE ? THEN 1 ELSE 0 END")
		args = append(args, "%"+term+"%")
	}
	order := "chunk_index ASC, id ASC"
	if len(parts) > 0 {
		order = "(" + strings.Join(parts, " + ") + ") DESC, " + order
	}
	return clause.OrderBy{Expression: clause.Expr{SQL: order, Vars: args}}
}

// originalChunks excludes model-generated summaries, captions and graph/Wiki
// derivatives: those cannot independently verify the original document.
// Empty legacy types are text, matching Wiki reconstruction and citation reads.
func (a *RuntimeAdapter) originalChunks(ctx context.Context, doc types.Knowledge) *gorm.DB {
	return a.DB.WithContext(ctx).Model(&types.Chunk{}).
		Where("knowledge_id = ? AND knowledge_base_id = ? AND tenant_id = ? AND is_enabled = ?", doc.ID, doc.KnowledgeBaseID, doc.TenantID, true).
		Where("chunk_type IN ?", []string{"", types.ChunkTypeText, types.ChunkTypeParentText, types.ChunkTypeImageOCR, types.ChunkTypeFAQ})
}

// sourceChunks applies one budget per real document. Explicit references come
// first, then neighboring qualifications; document-level sources also expand
// ranked batches. Relevance selects passages but never establishes consistency.
func (a *RuntimeAdapter) sourceChunks(ctx context.Context, doc types.Knowledge, refs []string, wholeDocument bool, terms []string, limit int) ([]types.Chunk, bool, error) {
	base := func() *gorm.DB { return a.originalChunks(ctx, doc) }
	out, ids := []types.Chunk{}, []string{}
	seen := map[string]bool{}
	incomplete := false
	add := func(c types.Chunk) {
		if seen[c.ID] {
			return
		}
		if len(out) == limit {
			incomplete = true
			return
		}
		out = append(out, c)
		ids = append(ids, c.ID)
		seen[c.ID] = true
	}
	neighbors := func(seeds []types.Chunk) ([]types.Chunk, error) {
		if len(seeds) == 0 {
			return nil, nil
		}
		indices := []int{}
		for _, c := range seeds {
			indices = append(indices, c.ChunkIndex-1, c.ChunkIndex+1)
		}
		var result []types.Chunk
		// Neighbors may be outside explicit refs, but never outside the document.
		err := base().Select("id,chunk_index").Where("chunk_index IN ?", indices).Order("chunk_index,id").Limit(limit + 1).Find(&result).Error
		if len(result) > limit {
			incomplete = true
		}
		return result, err
	}
	if len(refs) > 0 {
		var seeds []types.Chunk
		if err := base().Select("id,chunk_index").Where("id IN ?", refs).Clauses(sourceChunkOrder(terms)).Limit(limit + 1).Find(&seeds).Error; err != nil {
			return out, incomplete, err
		}
		if len(seeds) > limit {
			incomplete = true
			seeds = seeds[:limit]
		}
		// Reserve the budget for all explicit citations before spending it on context.
		for _, seed := range seeds {
			add(seed)
		}
		nearby, err := neighbors(seeds)
		if err != nil {
			return out, incomplete, err
		}
		for _, c := range nearby {
			add(c)
		}
	}
	if !wholeDocument {
		return out, incomplete, nil
	}
	for offset := 0; len(out) < limit; offset += sourceChunkBatchSize {
		var seeds []types.Chunk
		// Retain identifiers only; bodies are fetched under the global char budget.
		if err := base().Select("id,chunk_index").Clauses(sourceChunkOrder(terms)).Offset(offset).Limit(sourceChunkBatchSize).Find(&seeds).Error; err != nil {
			return out, incomplete, err
		}
		if len(seeds) == 0 {
			break
		}
		nearby, err := neighbors(seeds)
		if err != nil {
			return out, incomplete, err
		}
		for _, seed := range seeds {
			add(seed)
			for _, neighbor := range nearby {
				if neighbor.ChunkIndex == seed.ChunkIndex-1 || neighbor.ChunkIndex == seed.ChunkIndex+1 {
					add(neighbor)
				}
			}
		}
		if len(seeds) < sourceChunkBatchSize {
			break
		}
	}
	var remaining []types.Chunk
	q := base().Select("id")
	if len(ids) > 0 {
		q = q.Where("id NOT IN ?", ids)
	}
	err := q.Limit(1).Find(&remaining).Error
	return out, incomplete || len(remaining) > 0, err
}
