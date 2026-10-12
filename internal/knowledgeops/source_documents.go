package knowledgeops

import (
	"context"
	"slices"

	"github.com/Tencent/WeKnora/internal/types"
)

type sourceDocument struct {
	doc    types.Knowledge
	refs   []string
	whole  bool
	chunks []types.Chunk
}

// sourceDocuments merges all page references before selecting passages, so
// repeated links cannot multiply the per-document budget or hide explicit refs.
func (a *RuntimeAdapter) sourceDocuments(ctx context.Context, pages []types.WikiPage, terms []string, policy SourceLookupPolicy) ([]*sourceDocument, []string) {
	type documentKey struct {
		tenant uint64
		kb, id string
	}
	type pageCheck struct {
		refs        []string
		found       map[string]bool
		refsChecked bool
	}
	documents := []*sourceDocument{}
	byKey := map[documentKey]*sourceDocument{}
	checks := []pageCheck{}
	issues := []string{}
	addIssue := func(code string) {
		if !slices.Contains(issues, code) {
			issues = append(issues, code)
		}
	}
	for _, page := range pages {
		ids := page.SourceKnowledgeIDs()
		if len(ids) == 0 {
			addIssue("source_linkage_missing")
			continue
		}
		check := pageCheck{refs: page.ChunkRefs, found: map[string]bool{}, refsChecked: true}
		if len(ids) > policy.MaxDocumentsPerPage {
			ids = ids[:policy.MaxDocumentsPerPage]
			check.refsChecked = false
			addIssue("source_document_limit")
		}
		for _, id := range ids {
			key := documentKey{page.TenantID, page.KnowledgeBaseID, id}
			plan, exists := byKey[key]
			if !exists {
				var doc types.Knowledge
				if err := a.DB.WithContext(ctx).First(&doc, "id = ? AND knowledge_base_id = ? AND tenant_id = ?", id, page.KnowledgeBaseID, page.TenantID).Error; err == nil {
					plan = &sourceDocument{doc: doc}
					documents = append(documents, plan)
				}
				byKey[key] = plan
			}
			if plan == nil {
				check.refsChecked = false
				addIssue("source_document_lookup_failed")
				continue
			}
			if len(page.ChunkRefs) == 0 {
				plan.whole = true
				// Summary pages intentionally use document-level references.
				if page.PageType != types.WikiPageTypeSummary {
					addIssue("source_chunk_refs_missing")
				}
			} else {
				// A page can cite several documents while its citation pass only
				// succeeded for some of them. Check each page/document pair before
				// merging: another page's citation must not narrow this one's scope.
				var matching []types.Chunk
				// Inspect citation identities separately from the bounded body
				// read: an expansion limit cannot undo a known unavailable ref.
				err := a.originalChunks(ctx, plan.doc).Select("id").Where("id IN ?", []string(page.ChunkRefs)).Find(&matching).Error
				if err != nil {
					check.refsChecked = false
					addIssue("source_lookup_failed")
				} else if len(matching) == 0 {
					plan.whole = true
					addIssue("source_chunk_refs_missing")
				}
				for _, c := range matching {
					check.found[c.ID] = true
				}
			}
			for _, ref := range page.ChunkRefs {
				if !slices.Contains(plan.refs, ref) {
					plan.refs = append(plan.refs, ref)
				}
			}
		}
		checks = append(checks, check)
	}
	for _, plan := range documents {
		chunks, remaining, err := a.sourceChunks(ctx, plan.doc, plan.refs, plan.whole, terms, policy.MaxChunksPerDocument)
		plan.chunks = chunks
		if err != nil {
			addIssue("source_lookup_failed")
		} else if len(chunks) == 0 {
			addIssue("source_chunks_missing")
		}
		if remaining {
			addIssue("source_chunk_limit")
		}
	}
	for _, check := range checks {
		// Unvisited documents or failed identity queries leave refs unknown.
		// A body budget limit alone does not make checked identities unknown.
		if check.refsChecked {
			for _, ref := range check.refs {
				if !check.found[ref] {
					addIssue("source_chunks_missing")
				}
			}
		}
	}
	return documents, issues
}
