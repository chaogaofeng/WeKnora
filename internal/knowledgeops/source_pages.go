package knowledgeops

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
)

// Folder summaries are navigation/aggregation pages, not source documents.
// Follow only their recorded Wiki links, within the already-authorized KB.
// Linked pages that were not captured at answer time remain post-hoc scope:
// their current version cannot establish the answer-time source version.
const folderSourcePageLimit = 32

func (a *RuntimeAdapter) sourcePages(ctx context.Context, run Run, evidence []Evidence) ([]types.WikiPage, []string) {
	pages := []types.WikiPage{}
	issues := []string{}
	addIssue := func(code string) {
		if !slices.Contains(issues, code) {
			issues = append(issues, code)
		}
	}
	versions := map[string]string{}
	mixed := map[string]bool{}
	key := func(kbID, pageID string) string { return kbID + "/" + pageID }
	for _, e := range evidence {
		if !e.SentToModel || e.PostHoc || e.PageID == "" {
			continue
		}
		id := key(e.KBID, e.PageID)
		if version, ok := versions[id]; ok && version != e.Version {
			mixed[id] = true
		}
		versions[id] = e.Version
	}
	visited, active := map[string]bool{}, map[string]bool{}
	linkedReads := 0
	linkedPages := map[string]*types.WikiPage{}
	var visit func(types.WikiPage, bool)
	visit = func(page types.WikiPage, linked bool) {
		id := key(page.KnowledgeBaseID, page.ID)
		version, captured := versions[id]
		if mixed[id] || captured && version != strconv.Itoa(page.Version) {
			addIssue("source_page_version_changed")
			return
		}
		if linked && !captured {
			addIssue("source_folder_versions_unknown")
		}
		if active[id] {
			addIssue("source_folder_link_cycle")
			return
		}
		if visited[id] {
			return
		}
		visited[id], active[id] = true, true
		defer delete(active, id)
		if page.PageType != types.WikiPageTypeFolderSummary {
			pages = append(pages, page)
			return
		}
		links := []string{}
		for _, slug := range page.OutLinks {
			// Ignore self-navigation left by older folder generators. Never fetch
			// URLs, cross-KB references, or unrelated entity links as folder lineage.
			if slug == page.Slug || !(strings.HasPrefix(slug, "summary/") || strings.HasPrefix(slug, types.WikiFolderSummarySlugPrefix+"/")) || slices.Contains(links, slug) {
				continue
			}
			links = append(links, slug)
		}
		if len(links) == 0 {
			addIssue("source_folder_links_missing")
			return
		}
		for _, slug := range links {
			lookupKey := page.KnowledgeBaseID + "/" + strconv.FormatUint(page.TenantID, 10) + "/" + slug
			child, cached := linkedPages[lookupKey]
			if !cached {
				if linkedReads >= folderSourcePageLimit {
					addIssue("source_folder_page_limit")
					continue
				}
				linkedReads++
				var loaded types.WikiPage
				err := a.DB.WithContext(ctx).Where("slug = ? AND knowledge_base_id = ? AND tenant_id = ? AND status = ?", slug, page.KnowledgeBaseID, page.TenantID, types.WikiPageStatusPublished).First(&loaded).Error
				if err == nil && (loaded.PageType == types.WikiPageTypeSummary || loaded.PageType == types.WikiPageTypeFolderSummary) {
					child = &loaded
				}
				linkedPages[lookupKey] = child
			}
			if child == nil {
				addIssue("source_folder_link_unavailable")
				continue
			}
			visit(*child, true)
		}
	}
	checked := map[string]bool{}
	for _, e := range evidence {
		if !e.SentToModel || e.PostHoc || e.PageID == "" {
			continue
		}
		id := key(e.KBID, e.PageID)
		if checked[id] {
			continue
		}
		checked[id] = true
		kb, err := a.kb(ctx, run, e.KBID)
		if err != nil {
			addIssue("source_access_denied")
			continue
		}
		var page types.WikiPage
		if err = a.DB.WithContext(ctx).Where("id = ? AND knowledge_base_id = ? AND tenant_id = ? AND status = ?", e.PageID, kb.ID, kb.TenantID, types.WikiPageStatusPublished).First(&page).Error; err != nil {
			addIssue("source_lookup_failed")
			continue
		}
		visit(page, false)
	}
	return pages, issues
}
