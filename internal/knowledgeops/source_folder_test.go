package knowledgeops

import (
	"context"
	"fmt"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func folderSourceFixture(t *testing.T) (*RuntimeAdapter, context.Context) {
	t.Helper()
	repo := testStore(t)
	for _, statement := range []string{
		`CREATE TABLE knowledge_bases (id TEXT PRIMARY KEY, tenant_id BIGINT, deleted_at TIMESTAMP)`,
		`CREATE TABLE wiki_pages (id TEXT PRIMARY KEY, knowledge_base_id TEXT, tenant_id BIGINT, slug TEXT, title TEXT, content TEXT, page_type TEXT, status TEXT, version INT, source_refs TEXT, chunk_refs TEXT, out_links TEXT, deleted_at TIMESTAMP)`,
		`CREATE TABLE knowledges (id TEXT PRIMARY KEY, knowledge_base_id TEXT, tenant_id BIGINT, title TEXT, deleted_at TIMESTAMP)`,
		`CREATE TABLE chunks (id TEXT PRIMARY KEY, knowledge_id TEXT, knowledge_base_id TEXT, tenant_id BIGINT, content TEXT, content_revision INT, chunk_index INT, chunk_type TEXT DEFAULT 'text', is_enabled BOOLEAN, deleted_at TIMESTAMP)`,
		`INSERT INTO knowledge_bases (id,tenant_id) VALUES ('kb',1),('other',2)`,
		`INSERT INTO knowledges (id,knowledge_base_id,tenant_id,title) VALUES ('doc','kb',1,'Original')`,
		`INSERT INTO chunks (id,knowledge_id,knowledge_base_id,tenant_id,content,content_revision,chunk_index,is_enabled) VALUES ('chunk','doc','kb',1,'Original body',2,0,true)`,
	} {
		require.NoError(t, repo.db.Exec(statement).Error)
	}
	// Byte slices mirror JSON values returned by the PG driver and StringArray scanner.
	require.NoError(t, repo.db.Exec(`INSERT INTO wiki_pages (id,knowledge_base_id,tenant_id,slug,title,page_type,status,version,out_links) VALUES ('folder','kb',1,'folder_summary/a','Folder','folder_summary','published',1,?)`, []byte(`["summary/doc"]`)).Error)
	require.NoError(t, repo.db.Exec(`INSERT INTO wiki_pages (id,knowledge_base_id,tenant_id,slug,title,page_type,status,version,source_refs,chunk_refs) VALUES ('summary','kb',1,'summary/doc','Summary','summary','published',3,?,?)`, []byte(`["doc|Original"]`), []byte(`[]`)).Error)
	return &RuntimeAdapter{DB: repo.db}, types.WithCaller(context.Background(), types.Caller{TenantID: 1, UserID: "owner", Role: types.TenantRoleAdmin})
}
func folderSourceInput() []Evidence {
	return []Evidence{{KBID: "kb", PageID: "folder", Version: "1", SentToModel: true}, {KBID: "kb", PageID: "summary", Version: "3", SentToModel: true}}
}

func TestFolderSourceFollowsLinkedPagesWithoutInventingMissingDocumentRefs(t *testing.T) {
	a, ctx := folderSourceFixture(t)
	out, err := a.Sources(ctx, Run{TenantID: 1}, folderSourceInput())
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, "chunk", out[0].ChunkID)
	require.True(t, out[0].PostHoc)
	require.False(t, out[0].SentToModel)
}
func TestFolderSourceKeepsUncapturedLinkedVersionsUnresolved(t *testing.T) {
	a, ctx := folderSourceFixture(t)
	out, err := a.Sources(ctx, Run{TenantID: 1}, folderSourceInput()[:1])
	require.Len(t, out, 1)
	var limit *SourceReadError
	require.ErrorAs(t, err, &limit)
	require.Contains(t, limit.Issues, "source_folder_versions_unknown")
	require.NotContains(t, limit.Issues, "source_linkage_missing")
}
func TestFolderSourceDoesNotReadUnavailableOrForeignLinkedPages(t *testing.T) {
	for _, mutation := range []string{"tenant_id=2", "knowledge_base_id='other'", "status='draft'", "deleted_at=CURRENT_TIMESTAMP"} {
		t.Run(mutation, func(t *testing.T) {
			a, ctx := folderSourceFixture(t)
			require.NoError(t, a.DB.Exec("UPDATE wiki_pages SET "+mutation+" WHERE id='summary'").Error)
			out, err := a.Sources(ctx, Run{TenantID: 1}, folderSourceInput()[:1])
			require.Empty(t, out)
			var limit *SourceReadError
			require.ErrorAs(t, err, &limit)
			require.Contains(t, limit.Issues, "source_folder_link_unavailable")
		})
	}
}
func TestFolderSourceContinuesIndependentPagesAfterBrokenLink(t *testing.T) {
	a, ctx := folderSourceFixture(t)
	require.NoError(t, a.DB.Exec(`UPDATE wiki_pages SET out_links=? WHERE id='folder'`, []byte(`["summary/missing","summary/doc"]`)).Error)
	out, err := a.Sources(ctx, Run{TenantID: 1}, folderSourceInput())
	require.Len(t, out, 1)
	var limit *SourceReadError
	require.ErrorAs(t, err, &limit)
	require.Contains(t, limit.Issues, "source_folder_link_unavailable")
}
func TestFolderSourceBoundsCyclesAndDoesNotTreatNavigationAsOriginal(t *testing.T) {
	for _, links := range []string{`[]`, `["folder_summary/a"]`} {
		t.Run(links, func(t *testing.T) {
			a, ctx := folderSourceFixture(t)
			require.NoError(t, a.DB.Exec(`UPDATE wiki_pages SET out_links=? WHERE id='folder'`, []byte(links)).Error)
			out, err := a.Sources(ctx, Run{TenantID: 1}, folderSourceInput()[:1])
			require.Empty(t, out)
			var limit *SourceReadError
			require.ErrorAs(t, err, &limit)
			require.Contains(t, limit.Issues, "source_folder_links_missing")
		})
	}
}
func TestFolderSourceTraversalIsBounded(t *testing.T) {
	a, ctx := folderSourceFixture(t)
	for i := 0; i < 70; i++ {
		require.NoError(t, a.DB.Exec(`INSERT INTO wiki_pages (id,knowledge_base_id,tenant_id,slug,page_type,status,version,out_links) VALUES (?,'kb',1,?,'folder_summary','published',1,?)`, fmt.Sprintf("f%d", i), fmt.Sprintf("folder_summary/f%d", i), []byte(fmt.Sprintf(`["folder_summary/f%d"]`, i+1))).Error)
	}
	require.NoError(t, a.DB.Exec(`UPDATE wiki_pages SET out_links=? WHERE id='folder'`, []byte(`["folder_summary/f0"]`)).Error)
	_, err := a.Sources(ctx, Run{TenantID: 1}, folderSourceInput()[:1])
	var limit *SourceReadError
	require.ErrorAs(t, err, &limit)
	require.Contains(t, limit.Issues, "source_folder_page_limit")
	require.NotContains(t, limit.Issues, "source_folder_link_unavailable", "reader must stop before the unreachable last link")
}

func TestFolderSourceSharedDescendantsDoNotConsumeTraversalBudgetTwice(t *testing.T) {
	a, ctx := folderSourceFixture(t)
	input := folderSourceInput()
	links := types.StringArray{}
	for i := 0; i < 18; i++ {
		id := fmt.Sprintf("branch%d", i)
		slug := "folder_summary/" + id
		links = append(links, slug)
		require.NoError(t, a.DB.Exec(`INSERT INTO wiki_pages (id,knowledge_base_id,tenant_id,slug,page_type,status,version,out_links) VALUES (?,'kb',1,?,'folder_summary','published',1,?)`, id, slug, []byte(`["summary/doc"]`)).Error)
		input = append(input, Evidence{KBID: "kb", PageID: id, Version: "1", SentToModel: true})
	}
	require.NoError(t, a.DB.Exec(`UPDATE wiki_pages SET out_links=? WHERE id='folder'`, links).Error)
	out, err := a.Sources(ctx, Run{TenantID: 1}, input)
	require.NoError(t, err, "19 distinct linked pages fit within the traversal bound even with 18 shared edges")
	require.Len(t, out, 1)
}

func TestFolderSourceReportsMultiPageCycleAndStillReadsIndependentOriginal(t *testing.T) {
	a, ctx := folderSourceFixture(t)
	require.NoError(t, a.DB.Exec(`INSERT INTO wiki_pages (id,knowledge_base_id,tenant_id,slug,page_type,status,version,out_links) VALUES ('child','kb',1,'folder_summary/b','folder_summary','published',1,?)`, []byte(`["folder_summary/a","summary/doc"]`)).Error)
	require.NoError(t, a.DB.Exec(`UPDATE wiki_pages SET out_links=? WHERE id='folder'`, []byte(`["folder_summary/b"]`)).Error)
	input := append(folderSourceInput(), Evidence{KBID: "kb", PageID: "child", Version: "1", SentToModel: true})
	out, err := a.Sources(ctx, Run{TenantID: 1}, input)
	require.Len(t, out, 1)
	var limit *SourceReadError
	require.ErrorAs(t, err, &limit)
	require.Equal(t, []string{"source_folder_link_cycle"}, limit.Issues)
}

func TestFolderSourceRejectsChangedCapturedChildVersion(t *testing.T) {
	a, ctx := folderSourceFixture(t)
	require.NoError(t, a.DB.Exec(`UPDATE wiki_pages SET version=4 WHERE id='summary'`).Error)
	out, err := a.Sources(ctx, Run{TenantID: 1}, folderSourceInput())
	require.Empty(t, out)
	var limit *SourceReadError
	require.ErrorAs(t, err, &limit)
	require.Contains(t, limit.Issues, "source_page_version_changed")
}

func TestFolderSourceAuditPreservesActionableReasons(t *testing.T) {
	audit := sourceAuditForAttempt(&SourceReadError{Issues: []string{"source_chunk_limit", "source_folder_versions_unknown"}, CharLimit: 16000})
	require.Equal(t, []string{"source_chunk_limit", "source_folder_versions_unknown"}, audit.Issues)
	reason := sourceFollowupReason(Run{Analysis: &Analysis{Status: "succeeded", Report: Report{SourceAudit: audit}}})
	require.Contains(t, reason, "片段")
	require.Contains(t, reason, "回答时版本")
	require.NotContains(t, reason, "补齐原始材料", "a lookup budget restriction should not imply an absent document")
}
