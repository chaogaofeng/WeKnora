package knowledgeops

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/access"
	werrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// FAQAdoptionService only exposes listing. Explicit publication uses the
// identity-bound optional capability below; diagnosis cannot mutate FAQ or Wiki.
type FAQAdoptionService interface {
	ListFAQEntries(context.Context, string, *types.Pagination, []string, int64, string, string, string, *bool) (*types.PageResult, error)
}

// Optional capability implemented by the production knowledge service. Its
// private implementation owns creation recovery, indexing and atomic accounting.
type faqAdoptionWriter interface {
	CreateOrResumeFAQEntryForAdoption(context.Context, string, *types.FAQEntryPayload, string) (*types.FAQEntry, error)
}
type AdoptionAdapter struct {
	DB     *gorm.DB
	FAQ    FAQAdoptionService
	Shares access.KBShareLookup
}

func (a *AdoptionAdapter) target(ctx context.Context, id string, expectedTenant uint64) (context.Context, *types.KnowledgeBase, error) {
	if a == nil || a.DB == nil || a.FAQ == nil {
		return ctx, nil, ErrInvalid
	}
	var kb types.KnowledgeBase
	if err := a.DB.WithContext(ctx).First(&kb, "id = ? AND type = ? AND is_temporary = ?", id, types.KnowledgeBaseTypeFAQ, false).Error; err != nil {
		return ctx, nil, ErrForbidden
	}
	if expectedTenant != 0 && kb.TenantID != expectedTenant {
		return ctx, nil, ErrForbidden
	}
	grant, err := access.ResolveKB(ctx, access.KBRequest{Caller: types.CallerFromContext(ctx)}, &kb, types.OrgRoleEditor, a.Shares, nil)
	if err != nil {
		return ctx, nil, ErrForbidden
	}
	return grant.Context(ctx), &kb, nil
}
func (a *AdoptionAdapter) targets(ctx context.Context) ([]AdoptionTarget, error) {
	if a == nil || a.DB == nil || a.FAQ == nil {
		return nil, ErrInvalid
	}
	out := []AdoptionTarget{}
	last := ""
	for {
		var batch []types.KnowledgeBase
		q := a.DB.WithContext(ctx).Where("type = ? AND is_temporary = ?", types.KnowledgeBaseTypeFAQ, false)
		if last != "" {
			q = q.Where("id > ?", last)
		}
		if err := q.Order("id").Limit(100).Find(&batch).Error; err != nil {
			return nil, err
		}
		if len(batch) == 0 {
			return out, nil
		}
		for _, kb := range batch {
			last = kb.ID
			if _, err := access.ResolveKB(ctx, access.KBRequest{Caller: types.CallerFromContext(ctx)}, &kb, types.OrgRoleEditor, a.Shares, nil); err == nil {
				out = append(out, AdoptionTarget{ID: kb.ID, Name: kb.Name})
			}
		}
	}
}

// Legacy list results can contain default-status chunks. Exact content alone
// cannot prove indexing succeeded; verify the persisted chunk status as well.
func (a *AdoptionAdapter) matchingEntry(ctx context.Context, value Adoption) (*types.FAQEntry, bool, error) {
	for page := 1; ; page++ {
		result, err := a.FAQ.ListFAQEntries(ctx, value.KBID, &types.Pagination{Page: page, PageSize: 100}, nil, 0, value.Question, "standard_question", "asc", nil)
		if err != nil {
			return nil, false, err
		}
		if result == nil {
			return nil, false, fmt.Errorf("FAQ list returned no result")
		}
		entries, ok := result.Data.([]*types.FAQEntry)
		if !ok {
			return nil, false, fmt.Errorf("FAQ list returned invalid entries")
		}
		for _, entry := range entries {
			if entry == nil || entry.KnowledgeBaseID != value.KBID || strings.TrimSpace(entry.StandardQuestion) != value.Question {
				continue
			}
			if entry.ID > 0 && entry.IsEnabled && len(entry.Answers) == 1 && strings.TrimSpace(entry.Answers[0]) == value.Answer {
				var indexed int64
				if err := a.DB.WithContext(ctx).Model(&types.Chunk{}).Where("id = ? AND seq_id = ? AND tenant_id = ? AND knowledge_base_id = ? AND knowledge_id = ? AND chunk_type = ? AND status = ? AND is_enabled = ?", entry.ChunkID, entry.ID, value.TargetTenantID, value.KBID, entry.KnowledgeID, types.ChunkTypeFAQ, types.ChunkStatusIndexed, true).
					Where("index_status IN ?", []string{"ready", ""}).Count(&indexed).Error; err != nil {
					return nil, false, err
				}
				if indexed == 1 {
					return entry, false, nil
				}
				return nil, true, nil
			}
			return nil, true, nil
		}
		if len(entries) == 0 || int64(page*100) >= result.Total {
			return nil, false, nil
		}
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
	}
}
func (a *AdoptionAdapter) publish(ctx context.Context, value Adoption, reauthorize func(context.Context) (context.Context, error)) (*types.FAQEntry, bool, string, error) {
	entry, conflict, err := a.matchingEntry(ctx, value)
	if err != nil {
		return nil, false, "faq_lookup_failed", err
	}
	if conflict {
		return nil, false, "faq_question_conflict", fmt.Errorf("existing FAQ content differs or is disabled")
	}
	ctx, err = reauthorize(ctx)
	if err != nil {
		return nil, false, "faq_publish_failed", err
	}
	if entry != nil {
		return entry, true, "", nil
	}
	writer, ok := a.FAQ.(faqAdoptionWriter)
	if !ok {
		return nil, false, "faq_publish_failed", fmt.Errorf("resumable FAQ publication is unavailable")
	}
	entry, err = writer.CreateOrResumeFAQEntryForAdoption(ctx, value.KBID, &types.FAQEntryPayload{StandardQuestion: value.Question, Answers: []string{value.Answer}}, value.ID)
	if err == nil && entry != nil && entry.ID > 0 && entry.KnowledgeBaseID == value.KBID {
		return entry, false, "", nil
	}
	// Creation may have committed/indexed while its response was lost. A fresh
	// exact lookup is the only permitted recovery; different answers stay put.
	recovered, conflict, lookupErr := a.matchingEntry(ctx, value)
	if lookupErr == nil && recovered != nil {
		return recovered, true, "", nil
	}
	if conflict {
		return nil, false, "faq_question_conflict", fmt.Errorf("existing FAQ content differs or is disabled")
	}
	var appErr *werrors.AppError
	if errors.As(err, &appErr) && appErr.Code == werrors.ErrConflict {
		return nil, false, "faq_question_conflict", err
	}
	if err == nil {
		err = fmt.Errorf("FAQ creation returned no indexed entry")
	}
	return nil, false, "faq_publish_failed", err
}
