package knowledgeops

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/Tencent/WeKnora/internal/types"
)

const humanReviewedSourceScope = "human_reviewed"

// Independent pages have no original document to compare. Metadata is only a
// lookup hint: the succeeded publication must attest this exact page revision.
// Captured-version and access checks run in sourcePages before this function.
func (a *RuntimeAdapter) independentHumanPage(ctx context.Context, page types.WikiPage) (bool, error) {
	if page.PageType != types.WikiPageTypeSynthesis || page.Status != types.WikiPageStatusPublished || page.DeletedAt.Valid || page.Version < 1 || page.LastEditSource != types.WikiEditSourceUser || len(page.SourceRefs) != 0 || len(page.ChunkRefs) != 0 {
		return false, nil
	}
	var provenance wikiAdoptionProvenance
	if json.Unmarshal(page.PageMetadata, &provenance) != nil || provenance.Kind != wikiAdoptionKind || provenance.AdoptionID == "" || page.ID != wikiAdoptionPageID(provenance.AdoptionID) {
		return false, nil
	}
	// A request can commit the page before its final lookup is canceled. Keep
	// its immutable origin identity even if that attempt never saved success.
	var count int64
	err := a.DB.WithContext(ctx).Model(&Adoption{}).
		Where("id = ? AND target_type = ? AND target_tenant_id = ? AND kb_id = ? AND question = ? AND answer = ?", provenance.AdoptionID, "wiki", page.TenantID, page.KnowledgeBaseID, page.Title, page.Content).
		Count(&count).Error
	if err != nil || count != 1 {
		return false, err
	}
	// A later accepted run may successfully reuse the exact page without
	// rewriting its metadata or the interrupted origin's publication snapshot.
	count = 0
	err = a.DB.WithContext(ctx).Model(&Adoption{}).
		Where("status = ? AND target_type = ? AND target_tenant_id = ? AND kb_id = ?", "succeeded", "wiki", page.TenantID, page.KnowledgeBaseID).
		Where("wiki_page_id = ? AND wiki_slug = ? AND wiki_version = ? AND question = ? AND answer = ?", page.ID, page.Slug, page.Version, page.Title, page.Content).
		Count(&count).Error
	return count > 0 && err == nil, err
}

// This exception only removes the Wiki/no-original-comparison fallback. It
// never suppresses a known gap, a real comparison problem, or answer review.
func humanReviewedSourceOnly(run Run, report Report) bool {
	audit := report.SourceAudit
	if report.SourceCheck != "not_checked" || audit == nil || audit.ComparisonScope != humanReviewedSourceScope || len(audit.HumanPageIDs) == 0 || len(audit.Issues) != 0 || len(run.Coverage.ProvenanceGaps) != 0 {
		return false
	}
	for _, gap := range run.Gaps {
		if slices.Contains([]string{"source_version_unknown", "wiki_linked_summary_versions_unknown", "source_lookup_incomplete"}, gap) {
			return false
		}
	}
	return true
}
