package knowledgeops

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
)

// RuntimeAdapter is the sole read-only bridge to existing application resources.
// It never depends on SessionService, avoiding a capture/analysis dependency cycle.
type RuntimeAdapter struct {
	DB       *gorm.DB
	Sessions interfaces.SessionRepository
	Members  interfaces.TenantMemberRepository
	Shares   access.KBShareLookup
	Agents   access.AgentShareLookup
}
type workerContextKey struct{}

func (a *RuntimeAdapter) ExecutionContext(ctx context.Context, run Run) (context.Context, error) {
	caller := types.Caller{TenantID: run.TenantID, Role: types.TenantRoleViewer}
	if strings.HasPrefix(run.ActorID, "web_user:") {
		caller.UserID = strings.TrimPrefix(run.ActorID, "web_user:")
		member, err := a.Members.Get(ctx, caller.UserID, run.TenantID)
		if err != nil || member == nil || member.Status != types.TenantMemberStatusActive {
			return nil, ErrForbidden
		}
		var user types.User
		if err := a.DB.WithContext(ctx).First(&user, "id = ? AND is_active = ?", caller.UserID, true).Error; err != nil {
			return nil, ErrForbidden
		}
		caller.Role = member.Role
	}
	ctx = context.WithValue(ctx, types.UserIDContextKey, caller.UserID)
	ctx = types.WithCaller(ctx, caller)
	ctx = types.WithExecutionTenant(ctx, run.TenantID)
	return context.WithValue(ctx, workerContextKey{}, true), nil
}
func (a *RuntimeAdapter) Check(ctx context.Context, run Run, evidence []Evidence) error {
	// Until capture finishes, or if identity persistence was interrupted, a
	// missing row may be a permission dependency of another retained excerpt.
	if run.Status == "running" || len(run.Gaps) >= 64 {
		return ErrForbidden
	}
	for _, gap := range run.Gaps {
		switch gap {
		case "evidence_persist_failed", "capture_io_budget", "capture_source_count_budget", "capture_candidate_count_budget", "capture_callback_failed", "capture_interrupted":
			return ErrForbidden
		}
	}
	caller := types.CallerFromContext(ctx)
	if caller.TenantID != run.TenantID {
		return ErrForbidden
	}
	session, err := a.Sessions.GetByID(ctx, run.TenantID, run.SessionID)
	if err != nil || session == nil {
		return ErrForbidden
	}
	worker, _ := ctx.Value(workerContextKey{}).(bool)
	if !worker {
		im, err := a.Sessions.GetIMPlatform(ctx, run.TenantID, run.SessionID)
		if err != nil {
			return ErrForbidden
		}
		channel := types.SessionRequiresAdminConsoleRead(session, im)
		if channel {
			if !caller.Role.HasPermission(types.TenantRoleAdmin) {
				return ErrForbidden
			}
		} else if caller.UserID == "" || session.UserID != caller.UserID {
			return ErrForbidden
		}
	} else if caller.UserID != "" && session.UserID != caller.UserID {
		return ErrForbidden
	}
	checked := map[string]bool{}
	for _, e := range evidence {
		if e.KBID == "" {
			if e.Kind != "web" || e.ChunkID != "" || e.PageID != "" || e.KnowledgeID != "" {
				return ErrForbidden
			}
			continue
		}
		if !checked[e.KBID] {
			if _, err := a.kb(ctx, run, e.KBID); err != nil {
				return ErrForbidden
			}
			checked[e.KBID] = true
		}
		var n int64
		if e.KnowledgeID != "" {
			if err := a.DB.WithContext(ctx).Model(&types.Knowledge{}).Where("id = ? AND knowledge_base_id = ?", e.KnowledgeID, e.KBID).Count(&n).Error; err != nil || n != 1 {
				return ErrForbidden
			}
		}
		if e.ChunkID != "" {
			if err := a.DB.WithContext(ctx).Model(&types.Chunk{}).Where("id = ? AND knowledge_base_id = ? AND is_enabled = ?", e.ChunkID, e.KBID, true).Count(&n).Error; err != nil || n != 1 {
				return ErrForbidden
			}
		}
		if e.PageID != "" {
			if err := a.DB.WithContext(ctx).Model(&types.WikiPage{}).Where("id = ? AND knowledge_base_id = ? AND status = ?", e.PageID, e.KBID, "published").Count(&n).Error; err != nil || n != 1 {
				return ErrForbidden
			}
		}
	}
	return nil
}
func (a *RuntimeAdapter) kb(ctx context.Context, run Run, id string) (*types.KnowledgeBase, error) {
	var kb types.KnowledgeBase
	if err := a.DB.WithContext(ctx).First(&kb, "id = ?", id).Error; err != nil {
		return nil, err
	}
	caller := types.CallerFromContext(ctx)
	worker, _ := ctx.Value(workerContextKey{}).(bool)
	// Machine capability snapshots are intentionally not persisted. Without a
	// fresh human identity, never broaden a background read to another tenant.
	if worker && caller.UserID == "" && kb.TenantID != run.TenantID {
		return nil, ErrForbidden
	}
	_, err := access.ResolveKB(ctx, access.KBRequest{Caller: caller, AgentID: run.AgentID}, &kb, types.OrgRoleViewer, a.Shares, a.Agents)
	return &kb, err
}

// Sources compares captured Wiki pages with their current linked source chunks.
// Every supplement is explicitly post-hoc; version drift and partial coverage
// remain unresolved and can never repair missing generation-time evidence.
func (a *RuntimeAdapter) Sources(ctx context.Context, run Run, evidence []Evidence) ([]Evidence, error) {
	out := []Evidence{}
	seen := map[string]bool{}
	pageVersions := map[string]string{}
	budget := sourceCharLimit
	issues := []string{}
	addIssue := func(code string) {
		if !slices.Contains(issues, code) {
			issues = append(issues, code)
		}
	}
	sourceError := func(code string) error {
		addIssue(code)
		return &SourceReadError{Issues: issues, CharLimit: sourceCharLimit}
	}
	for _, e := range evidence {
		if !e.SentToModel || e.PostHoc || e.PageID == "" {
			continue
		}
		if version, found := pageVersions[e.PageID]; found {
			if version != e.Version {
				addIssue("source_page_version_changed")
			}
			continue
		}
		pageVersions[e.PageID] = e.Version
		kb, err := a.kb(ctx, run, e.KBID)
		if err != nil {
			return out, sourceError("source_access_denied")
		}
		var page types.WikiPage
		if err = a.DB.WithContext(ctx).First(&page, "id = ? AND knowledge_base_id = ? AND tenant_id = ? AND status = ?", e.PageID, kb.ID, kb.TenantID, "published").Error; err != nil {
			return out, sourceError("source_lookup_failed")
		}
		if e.Version != strconv.Itoa(page.Version) {
			addIssue("source_page_version_changed")
			continue
		}
		ids := page.SourceKnowledgeIDs()
		if len(ids) == 0 {
			addIssue("source_linkage_missing")
			continue
		}
		allRefsChecked := true
		if len(ids) > 8 {
			allRefsChecked = false
			ids = ids[:8]
			addIssue("source_document_limit")
		}
		foundRefs := map[string]bool{}
		for _, id := range ids {
			var doc types.Knowledge
			if err = a.DB.WithContext(ctx).First(&doc, "id = ? AND knowledge_base_id = ? AND tenant_id = ?", id, kb.ID, kb.TenantID).Error; err != nil {
				allRefsChecked = false
				addIssue("source_document_lookup_failed")
				continue
			}
			var chunks []types.Chunk
			query := a.DB.WithContext(ctx).Where("knowledge_id = ? AND knowledge_base_id = ? AND tenant_id = ? AND is_enabled = ?", id, kb.ID, kb.TenantID, true)
			if len(page.ChunkRefs) > 0 {
				query = query.Where("id IN ?", []string(page.ChunkRefs))
			} else if page.PageType != types.WikiPageTypeSummary {
				// Summary pages are document-level synopses and intentionally have
				// no chunk refs. Their bounded document scan still reports real limits.
				addIssue("source_chunk_refs_missing")
			}
			if err = query.Order("chunk_index,id").Limit(9).Find(&chunks).Error; err != nil {
				return out, sourceError("source_lookup_failed")
			}
			if len(chunks) == 0 {
				addIssue("source_chunks_missing")
			}
			if len(chunks) > 8 {
				allRefsChecked = false
				chunks = chunks[:8]
				addIssue("source_chunk_limit")
			}
			for _, c := range chunks {
				foundRefs[c.ID] = true
				if seen[c.ID] {
					continue
				}
				seen[c.ID] = true
				body := []rune(c.Content)
				truncated := false
				if len(body) > budget {
					body = body[:budget]
					truncated = true
					addIssue("source_char_budget_exhausted")
				}
				if len(body) == 0 {
					if !truncated {
						addIssue("source_chunks_missing")
					}
					continue
				}
				budget -= len(body)
				out = append(out, Evidence{Kind: "source_document", KBID: kb.ID, KnowledgeID: id, ChunkID: c.ID, Title: doc.Title, Version: strconv.Itoa(c.ContentRevision), Content: string(body), PostHoc: true, Truncated: truncated})
			}
		}
		// Limits and lookup failures leave unvisited references unknown. Only a
		// complete scan of linked sources can establish that a reference is missing.
		if allRefsChecked {
			for _, ref := range page.ChunkRefs {
				if !foundRefs[ref] {
					addIssue("source_chunks_missing")
				}
			}
		}
	}
	if len(issues) > 0 {
		return out, &SourceReadError{Issues: issues, CharLimit: sourceCharLimit}
	}
	return out, nil
}
