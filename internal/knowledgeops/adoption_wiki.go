package knowledgeops

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// WikiAdoptionService deliberately excludes generation, updates and deletion.
// All publication content is the immutable, explicitly approved Ops snapshot.
type WikiAdoptionService interface {
	CreatePage(context.Context, *types.WikiPage) (*types.WikiPage, error)
}

// CreatePage strips these internal citations. Reject them before publication
// rather than letting the Wiki writer silently change the approved answer.
var wikiStrippedCitation = regexp.MustCompile(`[ \t]*\[c\d{3,}(?:\s*[,;]\s*c\d{3,})*\]`)

const wikiAdoptionKind = "knowledge_ops_adoption"

type wikiAdoptionProvenance struct {
	Kind       string `json:"kind"`
	AdoptionID string `json:"adoption_id"`
}

func wikiAdoptionSlug(question string) string {
	return fmt.Sprintf("ops-qa-%x", sha256.Sum256([]byte(strings.TrimSpace(question))))
}
func wikiAdoptionPageID(adoptionID string) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("knowledge-ops/wiki/"+adoptionID)).String()
}

// Read actual persisted content, including soft-deleted rows. A matching title
// alone is never enough to turn an unrelated page into a successful adoption.
func (a *AdoptionAdapter) matchingWikiPage(ctx context.Context, value Adoption) (*types.WikiPage, bool, error) {
	var pages []types.WikiPage
	err := a.DB.WithContext(ctx).Unscoped().Where("tenant_id = ? AND knowledge_base_id = ? AND slug = ?", value.TargetTenantID, value.KBID, wikiAdoptionSlug(value.Question)).Limit(2).Find(&pages).Error
	if err != nil {
		return nil, false, err
	}
	if len(pages) == 0 {
		return nil, false, nil
	}
	if len(pages) != 1 {
		return nil, true, nil
	}
	page := &pages[0]
	var provenance wikiAdoptionProvenance
	if page.DeletedAt.Valid || page.Status != types.WikiPageStatusPublished || page.PageType != types.WikiPageTypeSynthesis || page.Version < 1 || page.Title != value.Question || page.Content != value.Answer || len(page.SourceRefs) != 0 || len(page.ChunkRefs) != 0 || json.Unmarshal(page.PageMetadata, &provenance) != nil || provenance.Kind != wikiAdoptionKind || provenance.AdoptionID == "" || page.ID != wikiAdoptionPageID(provenance.AdoptionID) {
		return nil, true, nil
	}
	// Ownership is proved against the durable Ops snapshot, not a user-editable
	// metadata label. The owner can be another run publishing the same Q/A.
	var owner Adoption
	err = a.DB.WithContext(ctx).Where("id = ? AND target_type = ? AND target_tenant_id = ? AND kb_id = ?", provenance.AdoptionID, "wiki", value.TargetTenantID, value.KBID).First(&owner).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	if owner.Question != value.Question || owner.Answer != value.Answer {
		return nil, true, nil
	}
	return page, false, nil
}

func (a *AdoptionAdapter) publishWiki(ctx context.Context, value Adoption, reauthorize func(context.Context) (context.Context, error)) (*types.WikiPage, bool, string, error) {
	page, conflict, err := a.matchingWikiPage(ctx, value)
	if err != nil {
		return nil, false, "wiki_lookup_failed", err
	}
	if conflict {
		return nil, false, "wiki_question_conflict", fmt.Errorf("reserved Wiki page is not a matching published Ops page")
	}
	ctx, err = reauthorize(ctx)
	if err != nil {
		return nil, false, "wiki_publish_failed", err
	}
	if page != nil {
		return page, true, "", nil
	}
	if a.Wiki == nil {
		return nil, false, "wiki_publish_failed", fmt.Errorf("Wiki publication is unavailable")
	}
	metadata, _ := json.Marshal(wikiAdoptionProvenance{Kind: wikiAdoptionKind, AdoptionID: value.ID})
	candidate := &types.WikiPage{
		ID: wikiAdoptionPageID(value.ID), TenantID: value.TargetTenantID, KnowledgeBaseID: value.KBID,
		Slug: wikiAdoptionSlug(value.Question), Title: value.Question, Content: value.Answer,
		PageType: types.WikiPageTypeSynthesis, Status: types.WikiPageStatusPublished, Version: 1,
		PageMetadata: types.JSON(metadata),
	}
	_, createErr := a.Wiki.CreatePage(types.WithWikiEditSource(ctx, types.WikiEditSourceUser), candidate)
	// A concurrent writer or lost response can leave a committed page. Always
	// verify DB truth, even when CreatePage claims success, and never overwrite.
	page, conflict, err = a.matchingWikiPage(ctx, value)
	if err != nil {
		return nil, false, "wiki_lookup_failed", err
	}
	if conflict {
		return nil, false, "wiki_question_conflict", fmt.Errorf("reserved Wiki page is not a matching published Ops page")
	}
	if page != nil {
		return page, createErr != nil || page.ID != candidate.ID, "", nil
	}
	if createErr == nil {
		createErr = fmt.Errorf("Wiki creation returned no published page")
	}
	return nil, false, "wiki_publish_failed", createErr
}
