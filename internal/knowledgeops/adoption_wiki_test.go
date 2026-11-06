package knowledgeops

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Ignoring target_type would expose FAQ destinations in the Wiki picker.
func TestAdoptionHTTPRejectsUnknownTargetType(t *testing.T) {
	svc, _, ctx, _, _, _ := adoptionFixture(t)
	status, response := adoptionRequest(t, adoptionRouter(svc, ctx), http.MethodGet, "/knowledge-ops/adoption-targets?target_type=unknown", nil)
	require.Equal(t, http.StatusBadRequest, status)
	require.Equal(t, "invalid_input", response["error"])
}

// A Wiki target must use its capability flag, never just its historical type.
func TestAdoptionHTTPWikiTargetsRequireEnabledCapability(t *testing.T) {
	svc, _, ctx, _, _, _ := adoptionFixture(t)
	require.NoError(t, svc.repo.db.Exec("ALTER TABLE knowledge_bases ADD COLUMN indexing_strategy TEXT").Error)
	require.NoError(t, svc.repo.db.Exec(`INSERT INTO knowledge_bases(id,name,type,tenant_id,is_temporary,indexing_strategy) VALUES ('document-wiki','Document Wiki','document',1,false,CAST('{"wiki_enabled":true}' AS BLOB)),('temporary-wiki','Temporary','document',1,true,CAST('{"wiki_enabled":true}' AS BLOB)),('foreign-wiki','Foreign','document',2,false,CAST('{"wiki_enabled":true}' AS BLOB))`).Error)
	status, response := adoptionRequest(t, adoptionRouter(svc, ctx), http.MethodGet, "/knowledge-ops/adoption-targets?target_type=wiki", nil)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, []any{map[string]any{"id": "document-wiki", "name": "Document Wiki"}}, response["data"].(map[string]any)["items"])
}

// Treating the request as legacy FAQ blocks explicit Wiki publication.
func TestAdoptionHTTPPublishesWikiIndependentlyOfFAQ(t *testing.T) {
	svc, _, ctx, run, a, r := adoptionFixture(t)
	require.NoError(t, svc.repo.db.Exec("ALTER TABLE knowledge_bases ADD COLUMN indexing_strategy TEXT").Error)
	require.NoError(t, svc.repo.db.Exec(`UPDATE knowledge_bases SET indexing_strategy=CAST('{"wiki_enabled":true}' AS BLOB) WHERE id='wiki'`).Error)
	wikiFixtureWriter(t, svc)
	faq, err := svc.Adopt(ctx, 1, run.ID, adoptionInput(a, r))
	require.NoError(t, err)
	require.Equal(t, "succeeded", faq.Status)
	body := map[string]any{"analysis_id": a.ID, "review_id": r.ID, "kb_id": "wiki", "target_type": "wiki", "question": " Wiki question ", "answer": " Wiki approved answer "}
	status, response := adoptionRequest(t, adoptionRouter(svc, ctx), http.MethodPost, "/knowledge-ops/runs/"+run.ID+"/adoptions", body)
	require.Equal(t, http.StatusOK, status)
	data := response["data"].(map[string]any)
	require.Equal(t, "succeeded", data["status"])
	require.Equal(t, "wiki", data["target_type"])
	require.NotEmpty(t, data["wiki_page_id"])
	require.NotEmpty(t, data["wiki_slug"])
	require.EqualValues(t, 1, data["wiki_version"])
	require.NotEqual(t, faq.ID, data["id"])
}

// Only the external Wiki service boundary is substituted here. Storage,
// uniqueness, ownership checks, permissions and publication state are real.
// The package-external integration test additionally exercises CreatePage.
type adoptionWikiWriter struct {
	db           *gorm.DB
	fail         error
	responseLost bool
	beforeCreate func(*types.WikiPage)
	skipCreate   bool
}

func (w *adoptionWikiWriter) CreatePage(ctx context.Context, page *types.WikiPage) (*types.WikiPage, error) {
	tenant, _ := types.TenantIDFromContext(ctx)
	if !access.HasKBGrant(ctx, page.KnowledgeBaseID, tenant, types.OrgRoleEditor) || tenant != page.TenantID {
		return nil, ErrForbidden
	}
	if w.beforeCreate != nil {
		w.beforeCreate(page)
	}
	if w.fail != nil {
		return nil, w.fail
	}
	if w.skipCreate {
		return page, nil
	}
	if err := w.db.WithContext(ctx).Create(page).Error; err != nil {
		return nil, err
	}
	if w.responseLost {
		return nil, errors.New("response lost")
	}
	return page, nil
}
func wikiFixtureWriter(t *testing.T, svc *Service) *adoptionWikiWriter {
	t.Helper()
	require.NoError(t, svc.repo.db.AutoMigrate(&types.WikiPage{}))
	// Match the real migrations' per-KB uniqueness, rather than the model's
	// legacy slug-only AutoMigrate index.
	require.NoError(t, svc.repo.db.Exec("DROP INDEX idx_kb_slug").Error)
	require.NoError(t, svc.repo.db.Exec("CREATE UNIQUE INDEX idx_kb_slug ON wiki_pages(knowledge_base_id,slug) WHERE deleted_at IS NULL").Error)
	writer := &adoptionWikiWriter{db: svc.repo.db}
	svc.adoption.Wiki = writer
	return writer
}

func wikiAdoptionFixture(t *testing.T) (*Service, *adoptionWikiWriter, context.Context, Run, Analysis, Review) {
	t.Helper()
	svc, _, ctx, run, a, r := adoptionFixture(t)
	require.NoError(t, svc.repo.db.Exec("ALTER TABLE knowledge_bases ADD COLUMN indexing_strategy TEXT").Error)
	require.NoError(t, svc.repo.db.Exec(`UPDATE knowledge_bases SET indexing_strategy=CAST('{"wiki_enabled":true}' AS BLOB) WHERE id IN ('wiki','foreign')`).Error)
	return svc, wikiFixtureWriter(t, svc), ctx, run, a, r
}
func wikiAdoptionInput(a Analysis, r Review) AdoptionInput {
	return AdoptionInput{TargetType: "wiki", AnalysisID: a.ID, ReviewID: r.ID, KBID: "wiki", Question: " 标准问题 ", Answer: " 人工核准答案 "}
}

// Missing deterministic identity or ignoring actual storage would duplicate a
// page after an interrupted publisher or report a fabricated response as saved.
func TestWikiAdoptionRecoveryUsesActualPublishedPage(t *testing.T) {
	for _, scenario := range []string{"normal", "lost_response", "failed_then_retry", "success_without_save"} {
		t.Run(scenario, func(t *testing.T) {
			svc, writer, ctx, run, a, r := wikiAdoptionFixture(t)
			writer.responseLost = scenario == "lost_response"
			writer.skipCreate = scenario == "success_without_save"
			if scenario == "failed_then_retry" {
				writer.fail = errors.New("private model server detail")
			}
			got, err := svc.Adopt(ctx, 1, run.ID, wikiAdoptionInput(a, r))
			require.NoError(t, err)
			if scenario == "success_without_save" {
				require.Equal(t, "failed", got.Status)
				require.Equal(t, "wiki_publish_failed", got.Error)
				require.Empty(t, got.WikiPageID)
				return
			}
			if scenario == "failed_then_retry" {
				require.Equal(t, "failed", got.Status)
				require.Equal(t, "wiki_publish_failed", got.Error)
				writer.fail = nil
				got, err = svc.RetryAdoption(ctx, 1, run.ID, got.ID)
				require.NoError(t, err)
			}
			require.Equal(t, "succeeded", got.Status)
			require.Equal(t, "标准问题", got.Question)
			require.Equal(t, "人工核准答案", got.Answer)
			require.Equal(t, "wiki", got.TargetType)
			require.Equal(t, scenario == "lost_response", got.Reused)
			var page types.WikiPage
			require.NoError(t, svc.repo.db.First(&page, "id = ?", got.WikiPageID).Error)
			require.Equal(t, "标准问题", page.Title)
			require.Equal(t, "人工核准答案", page.Content)
			require.Equal(t, "synthesis", page.PageType)
			require.Equal(t, "published", page.Status)
			require.Empty(t, page.SourceRefs)
			require.Empty(t, page.ChunkRefs)
			require.JSONEq(t, `{"kind":"knowledge_ops_adoption","adoption_id":"`+got.ID+`"}`, string(page.PageMetadata))
			require.NotContains(t, string(page.PageMetadata), run.SessionID)
			retry, err := svc.Adopt(ctx, 1, run.ID, wikiAdoptionInput(a, r))
			require.NoError(t, err)
			require.Equal(t, got.ID, retry.ID)
			require.Equal(t, got.WikiPageID, retry.WikiPageID)
			expired := time.Now().UTC().Add(-time.Minute)
			require.NoError(t, svc.repo.db.Model(&Adoption{}).Where("id = ?", got.ID).Updates(map[string]any{"status": "publishing", "lease_until": expired, "lease_token": "crashed"}).Error)
			retry, err = svc.RetryAdoption(ctx, 1, run.ID, got.ID)
			require.NoError(t, err)
			require.Equal(t, "succeeded", retry.Status)
			require.True(t, retry.Reused)
			require.Equal(t, got.WikiPageID, retry.WikiPageID)
			var count int64
			require.NoError(t, svc.repo.db.Model(&types.WikiPage{}).Count(&count).Error)
			require.EqualValues(t, 1, count)
		})
	}
}

// A reserved slug is a conflict unless DB content and durable provenance both
// prove it is the same published Ops Q/A. None of these cases may be overwritten.
func TestWikiAdoptionRefusesChangedArchivedDeletedAndForeignPages(t *testing.T) {
	for _, scenario := range []string{"answer", "title", "draft", "archived", "deleted", "wrong_type", "forged_metadata", "forged_owner", "source_refs", "duplicate_deleted"} {
		t.Run(scenario, func(t *testing.T) {
			svc, _, ctx, run, a, r := wikiAdoptionFixture(t)
			input := wikiAdoptionInput(a, r)
			got, err := svc.Adopt(ctx, 1, run.ID, input)
			require.NoError(t, err)
			require.Equal(t, "succeeded", got.Status)
			patch := map[string]any{}
			switch scenario {
			case "answer":
				patch["content"] = "changed answer"
			case "title":
				patch["title"] = "changed question"
			case "draft":
				patch["status"] = "draft"
			case "archived":
				patch["status"] = "archived"
			case "deleted":
				patch["deleted_at"] = time.Now()
			case "wrong_type":
				patch["page_type"] = "summary"
			case "forged_metadata":
				patch["page_metadata"] = []byte(`{"kind":"manual"}`)
			case "forged_owner":
				patch["page_metadata"] = []byte(`{"kind":"knowledge_ops_adoption","adoption_id":"no-such-adoption"}`)
			case "source_refs":
				patch["source_refs"] = []byte(`["invented-source"]`)
			case "duplicate_deleted":
				var page types.WikiPage
				require.NoError(t, svc.repo.db.First(&page, "id = ?", got.WikiPageID).Error)
				page.ID = uuid.NewString()
				page.DeletedAt = gorm.DeletedAt{Time: time.Now(), Valid: true}
				require.NoError(t, svc.repo.db.Create(&page).Error)
			}
			if len(patch) > 0 {
				require.NoError(t, svc.repo.db.Model(&types.WikiPage{}).Where("id = ?", got.WikiPageID).Updates(patch).Error)
			}
			require.NoError(t, svc.repo.db.Model(&Adoption{}).Where("id = ?", got.ID).Updates(map[string]any{"status": "failed", "lease_until": nil, "lease_token": ""}).Error)
			retry, err := svc.RetryAdoption(ctx, 1, run.ID, got.ID)
			require.NoError(t, err)
			require.Equal(t, "failed", retry.Status)
			require.Equal(t, "wiki_question_conflict", retry.Error)
			var page types.WikiPage
			require.NoError(t, svc.repo.db.Unscoped().First(&page, "id = ?", got.WikiPageID).Error)
			if scenario == "answer" {
				require.Equal(t, "changed answer", page.Content)
			}
			if scenario == "deleted" {
				require.True(t, page.DeletedAt.Valid)
			}
		})
	}
}

// New analysis/run identities must not generate different slugs for the same
// question; the first page stays authoritative and a changed answer conflicts.
func TestWikiAdoptionDeduplicatesAcrossRunsAndConflictsOnDifferentAnswer(t *testing.T) {
	for _, same := range []bool{true, false} {
		t.Run(fmt.Sprint(same), func(t *testing.T) {
			svc, _, ctx, run, a, r := wikiAdoptionFixture(t)
			first, err := svc.Adopt(ctx, 1, run.ID, wikiAdoptionInput(a, r))
			require.NoError(t, err)
			otherRun, otherAnalysis, otherReview := cloneWikiAdoptionSource(t, svc, run, a, r)
			input := wikiAdoptionInput(otherAnalysis, otherReview)
			if !same {
				input.Answer = "different approved answer"
			}
			second, err := svc.Adopt(ctx, 1, otherRun.ID, input)
			require.NoError(t, err)
			if same {
				require.Equal(t, "succeeded", second.Status)
				require.True(t, second.Reused)
				require.Equal(t, first.WikiPageID, second.WikiPageID)
			} else {
				require.Equal(t, "failed", second.Status)
				require.Equal(t, "wiki_question_conflict", second.Error)
				require.Empty(t, second.WikiPageID)
			}
			var count int64
			require.NoError(t, svc.repo.db.Model(&types.WikiPage{}).Count(&count).Error)
			require.EqualValues(t, 1, count)
		})
	}
}
func cloneWikiAdoptionSource(t *testing.T, svc *Service, run Run, a Analysis, r Review) (Run, Analysis, Review) {
	t.Helper()
	run.ID, run.MessageID = uuid.NewString(), uuid.NewString()
	require.NoError(t, svc.repo.CreateRun(context.Background(), &run))
	a.ID, a.RunID = uuid.NewString(), run.ID
	require.NoError(t, svc.repo.db.Create(&a).Error)
	r.ID, r.RunID, r.AnalysisID = uuid.NewString(), run.ID, a.ID
	require.NoError(t, svc.repo.db.Create(&r).Error)
	return run, a, r
}

func TestWikiAdoptionValidatesImmutableInputBeforeAdmission(t *testing.T) {
	for _, scenario := range []string{"unknown_target", "long_title", "stripped_citation", "kb_citation", "empty_answer"} {
		t.Run(scenario, func(t *testing.T) {
			svc, _, ctx, run, a, r := wikiAdoptionFixture(t)
			input := wikiAdoptionInput(a, r)
			switch scenario {
			case "unknown_target":
				input.TargetType = "document"
			case "long_title":
				input.Question = strings.Repeat("问", 513)
			case "stripped_citation":
				input.Answer = "literal `[c123]` would be silently stripped by the writer"
			case "kb_citation":
				input.Answer = `answer <kb doc="private"/>`
			case "empty_answer":
				input.Answer = "  "
			}
			_, err := svc.Adopt(ctx, 1, run.ID, input)
			require.ErrorIs(t, err, ErrInvalid)
			var count int64
			require.NoError(t, svc.repo.db.Model(&Adoption{}).Count(&count).Error)
			require.Zero(t, count)
		})
	}
	svc, _, ctx, run, a, r := wikiAdoptionFixture(t)
	input := wikiAdoptionInput(a, r)
	input.Question = strings.Repeat("问", 512)
	got, err := svc.Adopt(ctx, 1, run.ID, input)
	require.NoError(t, err)
	require.Equal(t, "succeeded", got.Status, "title limit counts characters, not bytes")
}

// Run-version checks and revoked source/target access must apply to Wiki too.
func TestWikiAdoptionRejectsMissingReviewStaleAnalysisAndLostAccess(t *testing.T) {
	for _, scenario := range []string{"missing_review", "unusable_review", "new_review", "new_analysis", "pending_analysis", "source", "disabled", "temporary", "foreign", "viewer_share"} {
		t.Run(scenario, func(t *testing.T) {
			svc, _, ctx, run, a, r := wikiAdoptionFixture(t)
			input := wikiAdoptionInput(a, r)
			expected := ErrForbidden
			switch scenario {
			case "missing_review":
				require.NoError(t, svc.repo.db.Delete(&r).Error)
				expected = ErrInvalid
			case "unusable_review":
				require.NoError(t, svc.repo.db.Model(&r).Update("verdict", "unusable").Error)
				expected = ErrInvalid
			case "new_review":
				other := r
				other.ID = uuid.NewString()
				other.CreatedAt = other.CreatedAt.Add(time.Second)
				require.NoError(t, svc.repo.db.Create(&other).Error)
				expected = ErrStale
			case "new_analysis":
				other := a
				other.ID = uuid.NewString()
				other.Version = 2
				require.NoError(t, svc.repo.db.Create(&other).Error)
				expected = ErrStale
			case "pending_analysis":
				require.NoError(t, svc.repo.Enqueue(ctx, run, 2))
				expected = ErrStale
			case "source":
				svc.access = func(context.Context, Run, []Evidence) error { return ErrForbidden }
			case "disabled":
				require.NoError(t, svc.repo.db.Exec("UPDATE knowledge_bases SET indexing_strategy=NULL WHERE id='wiki'").Error)
			case "temporary":
				require.NoError(t, svc.repo.db.Exec("UPDATE knowledge_bases SET is_temporary=true WHERE id='wiki'").Error)
			case "foreign":
				input.KBID = "foreign"
			case "viewer_share":
				input.KBID = "foreign"
				svc.adoption.Shares = &adoptionShareFake{permission: types.OrgRoleViewer}
			}
			_, err := svc.Adopt(ctx, 1, run.ID, input)
			require.ErrorIs(t, err, expected)
			var count int64
			require.NoError(t, svc.repo.db.Model(&types.WikiPage{}).Count(&count).Error)
			require.Zero(t, count)
		})
	}
}

func TestWikiAdoptionEditorShareWritesOnlyTargetTenant(t *testing.T) {
	svc, _, ctx, run, a, r := wikiAdoptionFixture(t)
	svc.adoption.Shares = &adoptionShareFake{permission: types.OrgRoleEditor}
	input := wikiAdoptionInput(a, r)
	input.KBID = "foreign"
	got, err := svc.Adopt(ctx, 1, run.ID, input)
	require.NoError(t, err)
	require.Equal(t, "succeeded", got.Status)
	var page types.WikiPage
	require.NoError(t, svc.repo.db.First(&page, "id = ?", got.WikiPageID).Error)
	require.EqualValues(t, 2, page.TenantID)
	svc.adoption.Shares = &adoptionShareFake{permission: types.OrgRoleViewer}
	_, err = svc.RetryAdoption(ctx, 1, run.ID, got.ID)
	require.ErrorIs(t, err, ErrForbidden)
}

func TestWikiAdoptionReauthorizesAfterPageLookup(t *testing.T) {
	for _, change := range []string{"review", "source", "target"} {
		t.Run(change, func(t *testing.T) {
			svc, _, ctx, run, a, r := wikiAdoptionFixture(t)
			var once sync.Once
			require.NoError(t, svc.repo.db.Callback().Query().After("gorm:query").Register("revoke_after_wiki_lookup", func(tx *gorm.DB) {
				if tx.Statement.Table != "wiki_pages" {
					return
				}
				once.Do(func() {
					switch change {
					case "review":
						other := r
						other.ID = uuid.NewString()
						other.CreatedAt = other.CreatedAt.Add(time.Second)
						other.Action = "rejected"
						require.NoError(t, svc.repo.db.Create(&other).Error)
					case "source":
						svc.access = func(context.Context, Run, []Evidence) error { return ErrForbidden }
					case "target":
						require.NoError(t, svc.repo.db.Exec("UPDATE knowledge_bases SET indexing_strategy=NULL WHERE id='wiki'").Error)
					}
				})
			}))
			got, err := svc.Adopt(ctx, 1, run.ID, wikiAdoptionInput(a, r))
			require.NoError(t, err)
			require.Equal(t, "failed", got.Status)
			want := "access_revoked"
			if change == "review" {
				want = "approval_changed"
			}
			require.Equal(t, want, got.Error)
			var count int64
			require.NoError(t, svc.repo.db.Model(&types.WikiPage{}).Count(&count).Error)
			require.Zero(t, count)
		})
	}
}

func TestWikiAdoptionConcurrentRunsRecoverTheSingleWinner(t *testing.T) {
	svc, writer, ctx, run, a, r := wikiAdoptionFixture(t)
	otherRun, otherA, otherR := cloneWikiAdoptionSource(t, svc, run, a, r)
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	writer.beforeCreate = func(*types.WikiPage) {
		if calls.Add(1) == 1 {
			close(started)
			<-release
		}
	}
	result := make(chan Adoption, 1)
	errs := make(chan error, 1)
	go func() { got, err := svc.Adopt(ctx, 1, run.ID, wikiAdoptionInput(a, r)); result <- got; errs <- err }()
	<-started
	pending, err := svc.Adopt(ctx, 1, run.ID, wikiAdoptionInput(a, r))
	require.NoError(t, err)
	require.Equal(t, "publishing", pending.Status)
	winner, err := svc.Adopt(ctx, 1, otherRun.ID, wikiAdoptionInput(otherA, otherR))
	close(release)
	require.NoError(t, err)
	require.Equal(t, "succeeded", winner.Status)
	recovered := <-result
	require.NoError(t, <-errs)
	require.Equal(t, "succeeded", recovered.Status)
	require.True(t, recovered.Reused)
	require.Equal(t, winner.WikiPageID, recovered.WikiPageID)
	var count int64
	require.NoError(t, svc.repo.db.Model(&types.WikiPage{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestWikiAdoptionLookupFailureCannotPublish(t *testing.T) {
	svc, _, ctx, run, a, r := wikiAdoptionFixture(t)
	require.NoError(t, svc.repo.db.Callback().Query().Before("gorm:query").Register("fail_wiki_lookup", func(tx *gorm.DB) {
		if tx.Statement.Table == "wiki_pages" {
			tx.AddError(errors.New("private database credential detail"))
		}
	}))
	got, err := svc.Adopt(ctx, 1, run.ID, wikiAdoptionInput(a, r))
	require.NoError(t, err)
	require.Equal(t, "failed", got.Status)
	require.Equal(t, "wiki_lookup_failed", got.Error)
	var count int64
	require.NoError(t, svc.repo.db.Raw("SELECT COUNT(*) FROM wiki_pages").Scan(&count).Error)
	require.Zero(t, count)
}
