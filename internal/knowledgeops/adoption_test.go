package knowledgeops

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// The fake replaces only the embedding/indexing boundary; admission, permissions,
// snapshots, leases and idempotency all use the real SQLite repository.
type adoptionFAQFake struct {
	db           *gorm.DB
	onList       func()
	listOnce     sync.Once
	mu           sync.Mutex
	entries      []*types.FAQEntry
	fail         error
	responseLost bool
	start        chan struct{}
	release      chan struct{}
}

func (f *adoptionFAQFake) ListFAQEntries(ctx context.Context, kb string, p *types.Pagination, tags []string, legacy int64, keyword, field, order string, enabled *bool) (*types.PageResult, error) {
	f.listOnce.Do(func() {
		if f.onList != nil {
			f.onList()
		}
	})
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []*types.FAQEntry{}
	for _, entry := range f.entries {
		if entry.KnowledgeBaseID == kb {
			out = append(out, entry)
		}
	}
	return types.NewPageResult(int64(len(out)), p, out), nil
}
func (f *adoptionFAQFake) CreateFAQEntry(ctx context.Context, kb string, p *types.FAQEntryPayload) (*types.FAQEntry, error) {
	tenantID, _ := types.TenantIDFromContext(ctx)
	if !access.HasKBGrant(ctx, kb, tenantID, types.OrgRoleEditor) {
		return nil, ErrForbidden
	}
	if f.start != nil {
		close(f.start)
		<-f.release
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return nil, f.fail
	}
	for _, e := range f.entries {
		if e.KnowledgeBaseID == kb && e.StandardQuestion == p.StandardQuestion {
			return nil, errors.New("already exists")
		}
	}
	entry := &types.FAQEntry{ID: 100000001 + int64(len(f.entries)), KnowledgeID: "faq-container", ChunkID: uuid.NewString(), KnowledgeBaseID: kb, StandardQuestion: p.StandardQuestion, Answers: p.Answers, IsEnabled: true}
	if err := f.db.Exec("INSERT INTO chunks(id,seq_id,tenant_id,knowledge_base_id,knowledge_id,chunk_type,status,is_enabled) VALUES (?,?,?,?,?,?,2,true)", entry.ChunkID, entry.ID, tenantID, kb, entry.KnowledgeID, types.ChunkTypeFAQ).Error; err != nil {
		return nil, err
	}
	f.entries = append(f.entries, entry)
	if f.responseLost {
		return nil, errors.New("response lost after indexing")
	}
	return entry, nil
}
func adoptionFixture(t *testing.T) (*Service, *adoptionFAQFake, context.Context, Run, Analysis, Review) {
	t.Helper()
	repo := testStore(t)
	require.NoError(t, repo.db.Exec("CREATE TABLE chunks (id TEXT PRIMARY KEY, seq_id INTEGER, tenant_id INTEGER, knowledge_base_id TEXT, knowledge_id TEXT, chunk_type TEXT, status INTEGER, is_enabled BOOLEAN, index_status TEXT NOT NULL DEFAULT 'ready', deleted_at DATETIME)").Error)
	require.NoError(t, repo.db.Exec("CREATE TABLE sessions (id TEXT PRIMARY KEY,tenant_id INTEGER,title TEXT,parent_session_id TEXT,deleted_at DATETIME)").Error)
	require.NoError(t, repo.db.Exec("CREATE TABLE knowledge_bases (id TEXT PRIMARY KEY, name TEXT, type TEXT, tenant_id INTEGER, is_temporary BOOLEAN, deleted_at DATETIME)").Error)
	require.NoError(t, repo.db.Exec("INSERT INTO knowledge_bases(id,name,type,tenant_id,is_temporary) VALUES ('faq','FAQ','faq',1,false),('wiki','Wiki','wiki',1,false),('foreign','Other','faq',2,false)").Error)
	now := time.Now().UTC()
	run := Run{ID: uuid.NewString(), TenantID: 1, ActorID: "web_user:source", SessionID: "session", MessageID: uuid.NewString(), Question: "source question", Answer: "source answer", Status: "completed", StartedAt: now, FinishedAt: &now}
	require.NoError(t, repo.CreateRun(context.Background(), &run))
	analysis := Analysis{ID: uuid.NewString(), TenantID: 1, RunID: run.ID, Version: 1, Status: "succeeded", Report: Report{Verdict: "unsupported"}, CreatedAt: now}
	require.NoError(t, repo.db.Create(&analysis).Error)
	review := Review{ID: uuid.NewString(), TenantID: 1, RunID: run.ID, AnalysisID: analysis.ID, Target: "answer", Action: "accepted", Verdict: "usable", Reason: "Checked whole answer", ReviewerID: "reviewer", CreatedAt: now}
	require.NoError(t, repo.db.Create(&review).Error)
	ctx := types.WithCaller(context.Background(), types.Caller{TenantID: 1, UserID: "publisher", Role: types.TenantRoleAdmin})
	faq := &adoptionFAQFake{db: repo.db}
	svc := NewService(repo, nil, func(context.Context, Run, []Evidence) error { return nil })
	svc.SetAdoptionAdapter(&AdoptionAdapter{DB: repo.db, FAQ: faq})
	return svc, faq, ctx, run, analysis, review
}
func adoptionInput(a Analysis, r Review) AdoptionInput {
	return AdoptionInput{AnalysisID: a.ID, ReviewID: r.ID, KBID: "faq", Question: " Edited standard question ", Answer: " Edited approved answer "}
}
func TestAdoptionPersistsEditedSnapshotAndDeduplicatesRetries(t *testing.T) {
	svc, _, ctx, run, a, r := adoptionFixture(t)
	input := adoptionInput(a, r)
	got, err := svc.Adopt(ctx, 1, run.ID, input)
	require.NoError(t, err)
	require.Equal(t, "succeeded", got.Status)
	require.Equal(t, "Edited standard question", got.Question)
	require.Equal(t, "Edited approved answer", got.Answer)
	require.EqualValues(t, 100000001, got.EntryID)
	require.Equal(t, "faq-container", got.KnowledgeID)
	require.Equal(t, "session", got.SessionID)
	require.Equal(t, run.MessageID, got.MessageID)
	require.Equal(t, "publisher", got.ActorID)
	require.Equal(t, "web_user:source", got.SourceActorID)
	again, err := svc.Adopt(ctx, 1, run.ID, input)
	require.NoError(t, err)
	require.Equal(t, got.ID, again.ID)
	again, err = svc.RetryAdoption(ctx, 1, run.ID, got.ID)
	require.NoError(t, err)
	require.Equal(t, got.EntryID, again.EntryID)
	var count int64
	require.NoError(t, svc.repo.db.Model(&Adoption{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	current, err := svc.repo.Run(ctx, 1, run.ID)
	require.NoError(t, err)
	require.Equal(t, "source answer", current.Answer)
	input.Answer = "different"
	_, err = svc.Adopt(ctx, 1, run.ID, input)
	require.ErrorIs(t, err, ErrStale)
	detail, err := svc.Detail(ctx, 1, run.ID)
	require.NoError(t, err)
	require.Len(t, detail.Adoptions, 1)
}
func TestAdoptionRequiresLatestAcceptedReviewAndFreshSourceAccess(t *testing.T) {
	for _, scenario := range []string{"machine_supported", "legacy_confirmed", "invalid_acceptance", "new_review", "new_analysis", "pending_analysis", "source_revoked", "target_wiki", "foreign_target", "wrong_tenant", "viewer"} {
		t.Run(scenario, func(t *testing.T) {
			svc, _, ctx, run, a, r := adoptionFixture(t)
			input := adoptionInput(a, r)
			expected := ErrStale
			switch scenario {
			case "machine_supported":
				require.NoError(t, svc.repo.db.Delete(&r).Error)
				expected = ErrInvalid
			case "invalid_acceptance":
				require.NoError(t, svc.repo.db.Model(&r).Update("verdict", "supported").Error)
				expected = ErrInvalid
			case "legacy_confirmed":
				require.NoError(t, svc.repo.db.Model(&r).Updates(map[string]any{"action": "confirmed", "verdict": "supported"}).Error)
				expected = ErrInvalid
			case "new_review":
				other := r
				other.ID = uuid.NewString()
				other.Action = "rejected"
				other.CreatedAt = other.CreatedAt.Add(time.Second)
				require.NoError(t, svc.repo.db.Create(&other).Error)
			case "new_analysis":
				newer := a
				newer.ID = uuid.NewString()
				newer.Version = 2
				require.NoError(t, svc.repo.db.Create(&newer).Error)
			case "pending_analysis":
				require.NoError(t, svc.repo.Enqueue(ctx, run, 2))
			case "source_revoked":
				svc.access = func(context.Context, Run, []Evidence) error { return ErrForbidden }
				expected = ErrForbidden
			case "target_wiki":
				input.KBID = "wiki"
				expected = ErrForbidden
			case "foreign_target":
				input.KBID = "foreign"
				expected = ErrForbidden
			case "wrong_tenant":
				ctx = types.WithCaller(ctx, types.Caller{TenantID: 2, UserID: "publisher", Role: types.TenantRoleAdmin})
				expected = ErrForbidden
			case "viewer":
				ctx = types.WithCaller(ctx, types.Caller{TenantID: 1, UserID: "publisher", Role: types.TenantRoleViewer})
				expected = ErrForbidden
			}
			_, err := svc.Adopt(ctx, 1, run.ID, input)
			require.ErrorIs(t, err, expected)
			var count int64
			require.NoError(t, svc.repo.db.Model(&Adoption{}).Count(&count).Error)
			require.Zero(t, count)
		})
	}
}
func TestAdoptionFailureRetryAndLostResponseReuseAnIndexedEntry(t *testing.T) {
	svc, faq, ctx, run, a, r := adoptionFixture(t)
	faq.fail = errors.New("embedding unavailable: secret internal detail")
	got, err := svc.Adopt(ctx, 1, run.ID, adoptionInput(a, r))
	require.NoError(t, err)
	require.Equal(t, "failed", got.Status)
	require.NotEmpty(t, got.Error)
	require.NotContains(t, got.Error, "secret")
	faq.fail = nil
	faq.responseLost = true
	got, err = svc.RetryAdoption(ctx, 1, run.ID, got.ID)
	require.NoError(t, err)
	require.Equal(t, "succeeded", got.Status)
	require.True(t, got.Reused)
	require.EqualValues(t, 100000001, got.EntryID)
}
func TestAdoptionNeverOverwritesDifferentAnswerOrReusesDisabledEntry(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(map[bool]string{true: "different answer", false: "disabled"}[enabled], func(t *testing.T) {
			svc, faq, ctx, run, a, r := adoptionFixture(t)
			answer := "different answer"
			if !enabled {
				answer = "Edited approved answer"
			}
			faq.entries = []*types.FAQEntry{{ID: 7, KnowledgeBaseID: "faq", StandardQuestion: "Edited standard question", Answers: []string{answer}, IsEnabled: enabled}}
			got, err := svc.Adopt(ctx, 1, run.ID, adoptionInput(a, r))
			require.NoError(t, err)
			require.Equal(t, "failed", got.Status)
			require.Equal(t, "faq_question_conflict", got.Error)
			require.Zero(t, got.EntryID)
		})
	}
}
func TestAdoptionLeasePreventsConcurrentPublicationAndRecoversAfterExpiry(t *testing.T) {
	svc, faq, ctx, run, a, r := adoptionFixture(t)
	faq.start = make(chan struct{})
	faq.release = make(chan struct{})
	result := make(chan Adoption, 1)
	errResult := make(chan error, 1)
	go func() { got, err := svc.Adopt(ctx, 1, run.ID, adoptionInput(a, r)); result <- got; errResult <- err }()
	<-faq.start
	got, err := svc.Adopt(ctx, 1, run.ID, adoptionInput(a, r))
	require.NoError(t, err)
	require.Equal(t, "publishing", got.Status)
	close(faq.release)
	first := <-result
	require.NoError(t, <-errResult)
	require.Equal(t, "succeeded", first.Status)
	expired := time.Now().UTC().Add(-time.Minute)
	require.NoError(t, svc.repo.db.Model(&Adoption{}).Where("id = ?", first.ID).Updates(map[string]any{"status": "publishing", "lease_until": expired, "lease_token": "crashed"}).Error)
	recovered, err := svc.RetryAdoption(ctx, 1, run.ID, first.ID)
	require.NoError(t, err)
	require.Equal(t, "succeeded", recovered.Status)
	require.True(t, recovered.Reused)
	require.Equal(t, first.EntryID, recovered.EntryID)
}
func TestAdoptionTargetsOnlyReturnWritableFAQAndRecheckOnRetry(t *testing.T) {
	svc, faq, ctx, run, a, r := adoptionFixture(t)
	targets, err := svc.AdoptionTargets(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, []AdoptionTarget{{ID: "faq", Name: "FAQ"}}, targets)
	faq.fail = errors.New("index unavailable")
	got, err := svc.Adopt(ctx, 1, run.ID, adoptionInput(a, r))
	require.NoError(t, err)
	require.Equal(t, "failed", got.Status)
	require.NoError(t, svc.repo.db.Exec("UPDATE knowledge_bases SET tenant_id=2 WHERE id='faq'").Error)
	_, err = svc.RetryAdoption(ctx, 1, run.ID, got.ID)
	require.ErrorIs(t, err, ErrForbidden)
}

// Legacy FAQ list results also contain default-status chunks; publication must
// independently verify an indexed row before reporting successful reuse.
func TestAdoptionDoesNotReportUnindexedExistingFAQAsSucceeded(t *testing.T) {
	svc, faq, ctx, run, a, r := adoptionFixture(t)
	faq.entries = []*types.FAQEntry{{ID: 8, ChunkID: "unindexed", KnowledgeID: "faq-container", KnowledgeBaseID: "faq", StandardQuestion: "Edited standard question", Answers: []string{"Edited approved answer"}, IsEnabled: true}}
	got, err := svc.Adopt(ctx, 1, run.ID, adoptionInput(a, r))
	require.NoError(t, err)
	require.Equal(t, "failed", got.Status)
	require.Zero(t, got.EntryID)
}

func TestAdoptionReusesOnlySearchableIndexedFAQ(t *testing.T) {
	for _, status := range []string{"ready", "", "processing", "failed"} {
		t.Run("index_"+status, func(t *testing.T) {
			svc, faq, ctx, run, a, r := adoptionFixture(t)
			entry := &types.FAQEntry{ID: 8, ChunkID: "indexed", KnowledgeID: "faq-container", KnowledgeBaseID: "faq", StandardQuestion: "Edited standard question", Answers: []string{"Edited approved answer"}, IsEnabled: true}
			faq.entries = []*types.FAQEntry{entry}
			require.NoError(t, svc.repo.db.Exec("INSERT INTO chunks(id,seq_id,tenant_id,knowledge_base_id,knowledge_id,chunk_type,status,is_enabled,index_status) VALUES (?,?,?,?,?,?,2,true,?)", entry.ChunkID, entry.ID, 1, entry.KnowledgeBaseID, entry.KnowledgeID, types.ChunkTypeFAQ, status).Error)
			got, err := svc.Adopt(ctx, 1, run.ID, adoptionInput(a, r))
			require.NoError(t, err)
			if status == "ready" || status == "" {
				require.Equal(t, "succeeded", got.Status)
				require.True(t, got.Reused)
				require.Equal(t, entry.ID, got.EntryID)
			} else {
				require.Equal(t, "failed", got.Status, "an entry blocked by the search gate cannot be reported as reusable")
				require.Equal(t, "faq_question_conflict", got.Error)
				require.Zero(t, got.EntryID)
			}
		})
	}
}

func TestAdoptionRechecksApprovalAndSourceAfterFAQReadBeforeCreation(t *testing.T) {
	for _, change := range []string{"review", "source", "target"} {
		t.Run(change, func(t *testing.T) {
			svc, faq, ctx, run, a, r := adoptionFixture(t)
			faq.onList = func() {
				switch change {
				case "review":
					other := r
					other.ID = uuid.NewString()
					other.Action = "rejected"
					other.CreatedAt = other.CreatedAt.Add(time.Second)
					require.NoError(t, svc.repo.db.Create(&other).Error)
				case "source":
					svc.access = func(context.Context, Run, []Evidence) error { return ErrForbidden }
				case "target":
					require.NoError(t, svc.repo.db.Exec("UPDATE knowledge_bases SET tenant_id=2 WHERE id='faq'").Error)
				}
			}
			got, err := svc.Adopt(ctx, 1, run.ID, adoptionInput(a, r))
			require.NoError(t, err)
			require.Equal(t, "failed", got.Status)
			want := "access_revoked"
			if change == "review" {
				want = "approval_changed"
			}
			require.Equal(t, want, got.Error)
			var entries int64
			require.NoError(t, svc.repo.db.Table("chunks").Count(&entries).Error)
			require.Zero(t, entries)
		})
	}
}

type adoptionShareFake struct{ permission types.OrgMemberRole }

func (s *adoptionShareFake) CheckTenantKBPermission(context.Context, string, uint64, types.TenantRole) (types.OrgMemberRole, bool, error) {
	return s.permission, true, nil
}
func TestAdoptionAllowsExplicitEditorSharesAndRejectsViewerShares(t *testing.T) {
	for _, permission := range []types.OrgMemberRole{types.OrgRoleViewer, types.OrgRoleEditor} {
		t.Run(string(permission), func(t *testing.T) {
			svc, _, ctx, run, a, r := adoptionFixture(t)
			svc.adoption.Shares = &adoptionShareFake{permission: permission}
			input := adoptionInput(a, r)
			input.KBID = "foreign"
			got, err := svc.Adopt(ctx, 1, run.ID, input)
			if permission == types.OrgRoleViewer {
				require.ErrorIs(t, err, ErrForbidden)
				return
			}
			require.NoError(t, err)
			require.Equal(t, "succeeded", got.Status)
			require.EqualValues(t, 2, got.TargetTenantID)
			require.Equal(t, "publisher", got.ActorID)
			var entryTenant uint64
			require.NoError(t, svc.repo.db.Table("chunks").Select("tenant_id").Where("seq_id = ?", got.EntryID).Scan(&entryTenant).Error)
			require.EqualValues(t, 2, entryTenant)
			targets, err := svc.AdoptionTargets(ctx, 1)
			require.NoError(t, err)
			require.Contains(t, targets, AdoptionTarget{ID: "foreign", Name: "Other"})
		})
	}
}

func (f *adoptionFAQFake) CreateOrResumeFAQEntryForAdoption(ctx context.Context, kb string, payload *types.FAQEntryPayload, _ string) (*types.FAQEntry, error) {
	return f.CreateFAQEntry(ctx, kb, payload)
}
