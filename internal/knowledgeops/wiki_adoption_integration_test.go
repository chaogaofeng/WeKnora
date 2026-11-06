package knowledgeops

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agenttools "github.com/Tencent/WeKnora/internal/agent/tools"
	apprepo "github.com/Tencent/WeKnora/internal/application/repository"
	appservice "github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const integrationWikiQuestion = "How does approved Meridian guidance work?"
const integrationWikiAnswer = "## Approved Meridian guidance\n\nKeep **the confirmed wording** and the `approval_code` field.\n\n- First check the requested region.\n- Then apply the published conditions."

// Catches publishing a generated/rewritten answer, missing human attribution,
// fake document lineage, or a page that the existing read tool cannot retrieve.
func TestWikiAdoptionIntegrationCreatesHumanPageReadableByNativeTool(t *testing.T) {
	svc, wiki, ctx, run, input := wikiAdoptionIntegrationFixture(t)
	got, err := svc.Adopt(ctx, 1, run.ID, input)
	require.NoError(t, err)
	require.Equal(t, "succeeded", got.Status, got.Error)
	require.Equal(t, "wiki", got.TargetType)
	page, err := wiki.GetPageBySlug(ctx, "wiki", got.WikiSlug)
	require.NoError(t, err)
	require.Equal(t, integrationWikiQuestion, page.Title)
	require.Equal(t, integrationWikiAnswer, page.Content)
	require.Equal(t, types.WikiPageTypeSynthesis, page.PageType)
	require.Equal(t, types.WikiPageStatusPublished, page.Status)
	require.Equal(t, types.WikiEditSourceUser, page.LastEditSource)
	require.Equal(t, "publisher", page.LastEditorID)
	require.Equal(t, 1, page.Version)
	require.Equal(t, page.ID, got.WikiPageID)
	require.Equal(t, page.Version, got.WikiVersion)
	require.Empty(t, page.SourceRefs)
	require.Empty(t, page.ChunkRefs)
	var provenance map[string]string
	require.NoError(t, json.Unmarshal(page.PageMetadata, &provenance))
	require.Equal(t, "knowledge_ops_adoption", provenance["kind"])
	require.Equal(t, got.ID, provenance["adoption_id"])

	tool := agenttools.NewWikiReadPageTool(wiki, nil, agenttools.NewWikiScopesFromKBIDs([]string{"wiki"}), nil)
	result := executeWikiIntegrationTool(t, ctx, tool, map[string]any{"slugs": []string{got.WikiSlug}})
	require.True(t, result.Success, result.Error)
	require.Contains(t, result.Output, integrationWikiQuestion)
	require.Contains(t, result.Output, integrationWikiAnswer)
	require.Len(t, result.SourceSnapshots, 1)
	require.Equal(t, got.WikiPageID, result.SourceSnapshots[0].PageID)
	require.Equal(t, "wiki", result.SourceSnapshots[0].KBID)
	require.Equal(t, "1", result.SourceSnapshots[0].Version)
	require.Empty(t, result.SourceSnapshots[0].KnowledgeID)

	// Repeating publication must not create a second native page or revision.
	again, err := svc.Adopt(ctx, 1, run.ID, input)
	require.NoError(t, err)
	require.Equal(t, got.ID, again.ID)
	require.Equal(t, got.WikiPageID, again.WikiPageID)
	var count int64
	require.NoError(t, svc.repo.db.Model(&types.WikiPage{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

// Catches treating an independent synthesis page as belonging to a selected
// document/tag, or using cached whole-KB routing as wider read authorization.
func TestWikiAdoptionIntegrationReadHonorsDocumentTagAndKnowledgeBaseScope(t *testing.T) {
	svc, wiki, ctx, run, input := wikiAdoptionIntegrationFixture(t)
	got, err := svc.Adopt(ctx, 1, run.ID, input)
	require.NoError(t, err)
	require.Equal(t, "succeeded", got.Status, got.Error)
	routes := agenttools.NewWikiRouteResolver()
	args := map[string]any{"slugs": []string{got.WikiSlug}}
	wholeKB := agenttools.NewWikiReadPageTool(wiki, nil, []agenttools.WikiScope{{KnowledgeBaseID: "wiki"}}, routes)
	require.True(t, executeWikiIntegrationTool(t, ctx, wholeKB, args).Success)
	for _, tc := range []struct {
		name  string
		scope agenttools.WikiScope
	}{
		{"document", agenttools.WikiScope{KnowledgeBaseID: "wiki", KnowledgeIDs: []string{"selected-document"}}},
		{"tag", agenttools.WikiScope{KnowledgeBaseID: "wiki", TagIDs: []string{"selected-tag"}}},
		{"other_knowledge_base", agenttools.WikiScope{KnowledgeBaseID: "other-wiki"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tool := agenttools.NewWikiReadPageTool(wiki, nil, []agenttools.WikiScope{tc.scope}, routes)
			result := executeWikiIntegrationTool(t, ctx, tool, args)
			require.False(t, result.Success)
			require.NotContains(t, result.Output, integrationWikiAnswer)
			require.Empty(t, result.SourceSnapshots)
		})
	}
}

// The real writer commits before this transport boundary loses the response.
// Replacing DB-truth recovery with response-only success would fail this test.
func TestWikiAdoptionIntegrationRecoversLostServiceResponse(t *testing.T) {
	svc, wiki, ctx, run, input := wikiAdoptionIntegrationFixture(t)
	svc.adoption.Wiki = wikiAdoptionLostResponse{writer: wiki}
	got, err := svc.Adopt(ctx, 1, run.ID, input)
	require.NoError(t, err)
	require.Equal(t, "succeeded", got.Status, got.Error)
	require.True(t, got.Reused)
	recovered, err := svc.RetryAdoption(ctx, 1, run.ID, got.ID)
	require.NoError(t, err)
	require.Equal(t, got.WikiPageID, recovered.WikiPageID)
	page, err := wiki.GetPageByID(ctx, got.WikiPageID)
	require.NoError(t, err)
	require.Equal(t, integrationWikiAnswer, page.Content)
	var count int64
	require.NoError(t, svc.repo.db.Model(&types.WikiPage{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

// Catches manufacturing original-source corroboration from human acceptance.
// Captured Wiki text can support an answer; it is not an original-source check.
func TestWikiAdoptionIntegrationDoesNotAssertOriginalSourceConsistency(t *testing.T) {
	svc, _, ctx, run, input := wikiAdoptionIntegrationFixture(t)
	got, err := svc.Adopt(ctx, 1, run.ID, input)
	require.NoError(t, err)
	require.Equal(t, "succeeded", got.Status, got.Error)
	evidence := []Evidence{{ID: "captured-page", Kind: "wiki", KBID: "wiki", PageID: got.WikiPageID, Version: "1", Content: integrationWikiAnswer, SentToModel: true}}
	supplement, sourceErr := (&RuntimeAdapter{DB: svc.repo.db}).Sources(ctx, run, evidence)
	require.Empty(t, supplement)
	require.False(t, sourceReadIncomplete(sourceErr))
	require.Empty(t, sourceAuditForAttempt(sourceErr).Issues)
	report := Report{SourceCheck: "consistent"}
	constrainSourceCheck(run, &report, append(evidence, supplement...))
	require.Equal(t, "not_checked", report.SourceCheck)
	require.False(t, hasSourceComparisonEvidence(append(evidence, supplement...)))
}

// PostgreSQL is required by the native repository's ~* search operator. Each
// opt-in invocation owns a fresh schema; it never writes application tables.
func TestPostgresWikiAdoptionIntegrationUsesNativeSearchAndRead(t *testing.T) {
	db := wikiAdoptionPostgresDatabase(t)
	for _, name := range []string{"000115_knowledge_ops", "000116_knowledge_ops_history", "000117_knowledge_ops_queries", "000118_knowledge_ops_adoptions", "000119_knowledge_ops_followups", "000120_knowledge_ops_calibration", "000121_knowledge_ops_wiki_adoptions"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "migrations", "versioned", name+".up.sql"))
		require.NoError(t, err)
		for _, statement := range strings.Split(string(data), ";") {
			if strings.TrimSpace(statement) != "" {
				require.NoError(t, db.Exec(statement).Error)
			}
		}
	}
	require.NoError(t, db.Exec("CREATE TABLE knowledge_bases (id TEXT PRIMARY KEY, name TEXT, type TEXT, tenant_id BIGINT, is_temporary BOOLEAN, indexing_strategy JSON, deleted_at TIMESTAMP)").Error)
	require.NoError(t, db.Exec(`INSERT INTO knowledge_bases(id,name,type,tenant_id,is_temporary,indexing_strategy) VALUES ('wiki','Wiki','document',1,false,'{"wiki_enabled":true}')`).Error)
	wiki := wikiAdoptionRealService(t, db)
	repo := NewRepository(db)
	now := time.Now().UTC()
	run := Run{ID: uuid.NewString(), TenantID: 1, ActorID: "web_user:source", SessionID: "session", MessageID: uuid.NewString(), Question: "source question", Answer: "source answer", Status: "completed", StartedAt: now, FinishedAt: &now}
	require.NoError(t, repo.CreateRun(context.Background(), &run))
	analysis := Analysis{ID: uuid.NewString(), TenantID: 1, RunID: run.ID, Version: 1, Status: "succeeded", Report: Report{Verdict: "unsupported"}, CreatedAt: now}
	require.NoError(t, db.Create(&analysis).Error)
	review := Review{ID: uuid.NewString(), TenantID: 1, RunID: run.ID, AnalysisID: analysis.ID, Target: "answer", Action: "accepted", Verdict: "usable", Reason: "human verified", ReviewerID: "reviewer", CreatedAt: now}
	require.NoError(t, db.Create(&review).Error)
	ctx := types.WithCaller(context.WithValue(context.Background(), types.UserIDContextKey, "publisher"), types.Caller{TenantID: 1, UserID: "publisher", Role: types.TenantRoleAdmin})
	svc := NewService(repo, nil, func(context.Context, Run, []Evidence) error { return nil })
	svc.SetAdoptionAdapter(&AdoptionAdapter{DB: db, Wiki: wiki})
	input := AdoptionInput{TargetType: "wiki", KBID: "wiki", AnalysisID: analysis.ID, ReviewID: review.ID, Question: integrationWikiQuestion, Answer: integrationWikiAnswer}
	got, err := svc.Adopt(ctx, 1, run.ID, input)
	require.NoError(t, err)
	require.Equal(t, "succeeded", got.Status, got.Error)

	routes := agenttools.NewWikiRouteResolver()
	scopes := agenttools.NewWikiScopesFromKBIDs([]string{"wiki"})
	search := agenttools.NewWikiSearchTool(wiki, nil, scopes, routes)
	result := executeWikiIntegrationTool(t, ctx, search, map[string]any{"query": "Meridian", "regex": false})
	require.True(t, result.Success, result.Error)
	require.Contains(t, result.Output, integrationWikiQuestion)
	require.Len(t, result.SourceSnapshots, 1)
	require.Equal(t, got.WikiPageID, result.SourceSnapshots[0].PageID)
	read := agenttools.NewWikiReadPageTool(wiki, nil, scopes, routes)
	result = executeWikiIntegrationTool(t, ctx, read, map[string]any{"slugs": []string{got.WikiSlug}})
	require.True(t, result.Success, result.Error)
	require.Contains(t, result.Output, integrationWikiAnswer)
	for _, scope := range []agenttools.WikiScope{
		{KnowledgeBaseID: "wiki", KnowledgeIDs: []string{"selected-document"}},
		{KnowledgeBaseID: "wiki", TagIDs: []string{"selected-tag"}},
		{KnowledgeBaseID: "other-wiki"},
	} {
		search := agenttools.NewWikiSearchTool(wiki, nil, []agenttools.WikiScope{scope}, routes)
		result := executeWikiIntegrationTool(t, ctx, search, map[string]any{"query": "Meridian", "regex": false})
		require.True(t, result.Success, result.Error)
		require.NotContains(t, result.Output, integrationWikiQuestion)
		require.Empty(t, result.SourceSnapshots)
	}
	result = executeWikiIntegrationTool(t, ctx, search, map[string]any{"query": "Meridian", "knowledge_base_ids": []string{"other-wiki"}})
	require.False(t, result.Success)
	require.Empty(t, result.SourceSnapshots)
}

func wikiAdoptionIntegrationFixture(t *testing.T) (*Service, interfaces.WikiPageService, context.Context, Run, AdoptionInput) {
	t.Helper()
	svc, _, ctx, run, analysis, review := adoptionFixture(t)
	if !svc.repo.db.Migrator().HasColumn(&types.KnowledgeBase{}, "indexing_strategy") {
		require.NoError(t, svc.repo.db.Exec("ALTER TABLE knowledge_bases ADD COLUMN indexing_strategy JSON").Error)
	}
	require.NoError(t, svc.repo.db.Model(&types.KnowledgeBase{}).Where("id = ?", "wiki").UpdateColumn("indexing_strategy", types.IndexingStrategy{WikiEnabled: true}).Error)
	wiki := wikiAdoptionRealService(t, svc.repo.db)
	svc.adoption.Wiki = wiki
	ctx = context.WithValue(ctx, types.UserIDContextKey, "publisher")
	input := AdoptionInput{TargetType: "wiki", KBID: "wiki", AnalysisID: analysis.ID, ReviewID: review.ID, Question: integrationWikiQuestion, Answer: integrationWikiAnswer}
	return svc, wiki, ctx, run, input
}

func wikiAdoptionRealService(t *testing.T, db *gorm.DB) interfaces.WikiPageService {
	t.Helper()
	require.NoError(t, db.AutoMigrate(&types.WikiPage{}))
	// Match native migrations, whose unique key is KB + slug, not the model's
	// legacy slug-only AutoMigrate annotation.
	require.NoError(t, db.Exec("DROP INDEX idx_kb_slug").Error)
	require.NoError(t, db.Exec("CREATE UNIQUE INDEX idx_kb_slug ON wiki_pages(knowledge_base_id,slug) WHERE deleted_at IS NULL").Error)
	return appservice.NewWikiPageService(apprepo.NewWikiPageRepository(db), nil, nil, nil, nil)
}

func executeWikiIntegrationTool(t *testing.T, ctx context.Context, tool types.Tool, args map[string]any) *types.ToolResult {
	t.Helper()
	data, err := json.Marshal(args)
	require.NoError(t, err)
	result, err := tool.Execute(ctx, data)
	require.NoError(t, err)
	require.NotNil(t, result)
	return result
}

type wikiAdoptionLostResponse struct{ writer WikiAdoptionService }

func (w wikiAdoptionLostResponse) CreatePage(ctx context.Context, page *types.WikiPage) (*types.WikiPage, error) {
	if _, err := w.writer.CreatePage(ctx, page); err != nil {
		return nil, err
	}
	return nil, errors.New("response lost after real Wiki page commit")
}

func wikiAdoptionPostgresDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("KNOWLEDGE_OPS_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set KNOWLEDGE_OPS_TEST_POSTGRES_DSN to a disposable PostgreSQL instance")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	rawAdmin, err := admin.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, rawAdmin.Close()) })
	schema := "knowledge_ops_wiki_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	require.NoError(t, admin.Exec("CREATE SCHEMA "+schema).Error)
	t.Cleanup(func() { require.NoError(t, admin.Exec("DROP SCHEMA "+schema+" CASCADE").Error) })
	scopedDSN := dsn + " search_path=" + schema
	if u, parseErr := url.Parse(dsn); parseErr == nil && (u.Scheme == "postgres" || u.Scheme == "postgresql") {
		q := u.Query()
		q.Set("search_path", schema)
		u.RawQuery = q.Encode()
		scopedDSN = u.String()
	}
	db, err := gorm.Open(postgres.Open(scopedDSN), &gorm.Config{})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, raw.Close()) })
	var activeSchema string
	require.NoError(t, db.Raw("SELECT current_schema()").Scan(&activeSchema).Error)
	require.Equal(t, schema, activeSchema, "never run integration writes against application tables")
	return db
}
