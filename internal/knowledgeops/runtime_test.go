package knowledgeops

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type sessionReadStub struct {
	interfaces.SessionRepository
	session *types.Session
	im      string
}

func (s sessionReadStub) GetByID(context.Context, uint64, string) (*types.Session, error) {
	if s.session == nil {
		return nil, ErrForbidden
	}
	return s.session, nil
}
func (s sessionReadStub) GetIMPlatform(context.Context, uint64, string) (string, error) {
	return s.im, nil
}
func TestRuntimePreservesPrivateSessionAndRevokedSourceGuards(t *testing.T) {
	repo := testStore(t)
	require.NoError(t, repo.db.Exec(`CREATE TABLE knowledge_bases (id TEXT PRIMARY KEY, tenant_id BIGINT, deleted_at TIMESTAMP)`).Error)
	require.NoError(t, repo.db.Exec(`CREATE TABLE wiki_pages (id TEXT PRIMARY KEY, knowledge_base_id TEXT, tenant_id BIGINT, status TEXT, deleted_at TIMESTAMP)`).Error)
	require.NoError(t, repo.db.Exec(`INSERT INTO knowledge_bases (id,tenant_id) VALUES ('kb',1),('foreign',2)`).Error)
	require.NoError(t, repo.db.Exec(`INSERT INTO wiki_pages (id,knowledge_base_id,tenant_id,status) VALUES ('page','kb',1,'published')`).Error)
	adapter := RuntimeAdapter{DB: repo.db, Sessions: sessionReadStub{session: &types.Session{ID: "s", TenantID: 1, UserID: "owner"}}}
	run := Run{TenantID: 1, SessionID: "s"}
	evidence := []Evidence{{KBID: "kb", PageID: "page"}}
	ctx := types.WithCaller(context.Background(), types.Caller{TenantID: 1, UserID: "owner", Role: types.TenantRoleAdmin})
	require.NoError(t, adapter.Check(ctx, run, evidence))
	otherAdmin := types.WithCaller(context.Background(), types.Caller{TenantID: 1, UserID: "other", Role: types.TenantRoleAdmin})
	require.ErrorIs(t, adapter.Check(otherAdmin, run, evidence), ErrForbidden)
	require.ErrorIs(t, adapter.Check(ctx, run, []Evidence{{KBID: "foreign"}}), ErrForbidden)
	require.NoError(t, repo.db.Exec(`UPDATE wiki_pages SET deleted_at=CURRENT_TIMESTAMP WHERE id='page'`).Error)
	require.ErrorIs(t, adapter.Check(ctx, run, evidence), ErrForbidden)
}
func TestRuntimeChannelReadRequiresAdminAndMissingSessionDenied(t *testing.T) {
	repo := testStore(t)
	run := Run{TenantID: 1, SessionID: "s"}
	adapter := RuntimeAdapter{DB: repo.db, Sessions: sessionReadStub{session: &types.Session{ID: "s", TenantID: 1, UserID: "api_tenant_key:1:key"}}}
	admin := types.WithCaller(context.Background(), types.Caller{TenantID: 1, UserID: "admin", Role: types.TenantRoleAdmin})
	require.NoError(t, adapter.Check(admin, run, nil))
	viewer := types.WithCaller(context.Background(), types.Caller{TenantID: 1, UserID: "viewer", Role: types.TenantRoleViewer})
	require.ErrorIs(t, adapter.Check(viewer, run, nil), ErrForbidden)
	adapter.Sessions = sessionReadStub{}
	require.ErrorIs(t, adapter.Check(admin, run, nil), ErrForbidden)
}

func TestKnowledgeEvidenceWithoutKBCannotBypassAccess(t *testing.T) {
	repo := testStore(t)
	adapter := RuntimeAdapter{DB: repo.db, Sessions: sessionReadStub{session: &types.Session{ID: "s", TenantID: 1, UserID: "owner"}}}
	ctx := types.WithCaller(context.Background(), types.Caller{TenantID: 1, UserID: "owner", Role: types.TenantRoleAdmin})
	require.ErrorIs(t, adapter.Check(ctx, Run{TenantID: 1, SessionID: "s"}, []Evidence{{Kind: "chunk", ChunkID: "c", KnowledgeID: "d", Content: "protected"}}), ErrForbidden)
}

func TestIncompletePermissionBindingsCannotExposeCapturedBodies(t *testing.T) {
	adapter := RuntimeAdapter{Sessions: sessionReadStub{session: &types.Session{ID: "s", TenantID: 1, UserID: "owner"}}}
	ctx := types.WithCaller(context.Background(), types.Caller{TenantID: 1, UserID: "owner", Role: types.TenantRoleAdmin})
	for _, gap := range []string{"evidence_persist_failed", "capture_io_budget", "capture_source_count_budget", "capture_candidate_count_budget", "capture_callback_failed", "capture_interrupted"} {
		t.Run(gap, func(t *testing.T) {
			require.ErrorIs(t, adapter.Check(ctx, Run{TenantID: 1, SessionID: "s", Status: "completed", Gaps: []string{gap}}, nil), ErrForbidden)
		})
	}
	require.ErrorIs(t, adapter.Check(ctx, Run{TenantID: 1, SessionID: "s", Status: "running"}, nil), ErrForbidden)
}

func TestSourceRuntimeReportsBoundedComparisonReasons(t *testing.T) {
	repo := testStore(t)
	for _, sql := range []string{
		`CREATE TABLE knowledge_bases (id TEXT PRIMARY KEY, tenant_id BIGINT, deleted_at TIMESTAMP)`,
		`CREATE TABLE wiki_pages (id TEXT PRIMARY KEY, knowledge_base_id TEXT, tenant_id BIGINT, status TEXT, version INT, source_refs TEXT, chunk_refs TEXT, deleted_at TIMESTAMP)`,
		`CREATE TABLE knowledges (id TEXT PRIMARY KEY, knowledge_base_id TEXT, tenant_id BIGINT, title TEXT, deleted_at TIMESTAMP)`,
		`CREATE TABLE chunks (id TEXT PRIMARY KEY, knowledge_id TEXT, knowledge_base_id TEXT, tenant_id BIGINT, content TEXT, content_revision INT, chunk_index INT, is_enabled BOOLEAN, deleted_at TIMESTAMP)`,
		`INSERT INTO knowledge_bases (id,tenant_id) VALUES ('kb',1)`,
		`INSERT INTO wiki_pages VALUES ('page','kb',1,'published',1,'["doc|Title"]','["chunk"]',NULL)`,
		`INSERT INTO knowledges (id,knowledge_base_id,tenant_id,title) VALUES ('doc','kb',1,'Title')`,
	} {
		require.NoError(t, repo.db.Exec(sql).Error)
	}
	require.NoError(t, repo.db.Exec(`UPDATE wiki_pages SET source_refs=?,chunk_refs=?`, []byte(`["doc|Title"]`), []byte(`["chunk","chunk2"]`)).Error)
	require.NoError(t, repo.db.Exec(`INSERT INTO chunks (id,knowledge_id,knowledge_base_id,tenant_id,content,is_enabled) VALUES ('chunk','doc','kb',1,?,true)`, strings.Repeat("字", 16001)).Error)
	require.NoError(t, repo.db.Exec(`INSERT INTO chunks (id,knowledge_id,knowledge_base_id,tenant_id,content,is_enabled) VALUES ('chunk2','doc','kb',1,'later',true)`).Error)
	adapter := RuntimeAdapter{DB: repo.db}
	ctx := types.WithCaller(context.Background(), types.Caller{TenantID: 1, UserID: "owner", Role: types.TenantRoleAdmin})
	out, err := adapter.Sources(ctx, Run{TenantID: 1}, []Evidence{{KBID: "kb", PageID: "page", Version: "1", SentToModel: true}})
	require.Len(t, out, 1, "reader error: %v", err)
	require.Len(t, []rune(out[0].Content), 16000)
	require.True(t, out[0].Truncated)
	var limitation *SourceReadError
	require.ErrorAs(t, err, &limitation)
	require.Contains(t, limitation.Issues, "source_char_budget_exhausted")
	require.Equal(t, 16000, limitation.CharLimit)
	require.NotContains(t, limitation.Issues, "source_chunks_missing", "existing chunks omitted by budget are not missing")
}

func TestSourceRuntimeSeparatesVersionLinkageAndLookupLimits(t *testing.T) {
	repo := testStore(t)
	require.NoError(t, repo.db.Exec(`CREATE TABLE knowledge_bases (id TEXT PRIMARY KEY,tenant_id BIGINT,deleted_at TIMESTAMP)`).Error)
	require.NoError(t, repo.db.Exec(`CREATE TABLE wiki_pages (id TEXT PRIMARY KEY,knowledge_base_id TEXT,tenant_id BIGINT,status TEXT,version INT,source_refs TEXT,chunk_refs TEXT,deleted_at TIMESTAMP)`).Error)
	require.NoError(t, repo.db.Exec(`INSERT INTO knowledge_bases (id,tenant_id) VALUES ('kb',1)`).Error)
	require.NoError(t, repo.db.Exec(`INSERT INTO wiki_pages (id,knowledge_base_id,tenant_id,status,version) VALUES ('page','kb',1,'published',2)`).Error)
	adapter := RuntimeAdapter{DB: repo.db}
	ctx := types.WithCaller(context.Background(), types.Caller{TenantID: 1, UserID: "owner", Role: types.TenantRoleAdmin})
	for _, tc := range []struct{ version, page, code string }{{"1", "page", "source_page_version_changed"}, {"2", "page", "source_linkage_missing"}, {"2", "absent", "source_lookup_failed"}} {
		out, err := adapter.Sources(ctx, Run{TenantID: 1}, []Evidence{{KBID: "kb", PageID: tc.page, Version: tc.version, SentToModel: true}})
		require.Empty(t, out)
		var limitation *SourceReadError
		require.ErrorAs(t, err, &limitation)
		require.Equal(t, []string{tc.code}, limitation.Issues)
	}
}

func TestSourceRuntimeDoesNotInferMissingRefsOutsideReadBounds(t *testing.T) {
	for _, tc := range []struct {
		name              string
		documents, chunks int
		missingRef        bool
		unavailableDoc    bool
		wantCount         int
		wantIssues        []string
	}{
		{name: "complete", documents: 1, chunks: 2, wantCount: 2},
		{name: "ninth chunk exists", documents: 1, chunks: 9, wantCount: 8, wantIssues: []string{"source_chunk_limit"}},
		{name: "unqueried chunks exist", documents: 1, chunks: 12, wantCount: 8, wantIssues: []string{"source_chunk_limit"}},
		{name: "ninth document exists", documents: 9, chunks: 1, wantCount: 8, wantIssues: []string{"source_document_limit"}},
		{name: "missing reference in fully examined sources", documents: 1, chunks: 2, missingRef: true, wantCount: 2, wantIssues: []string{"source_chunks_missing"}},
		{name: "unavailable document leaves refs unknown", documents: 2, chunks: 1, unavailableDoc: true, wantCount: 1, wantIssues: []string{"source_document_lookup_failed"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := testStore(t)
			for _, sql := range []string{
				`CREATE TABLE knowledge_bases (id TEXT PRIMARY KEY, tenant_id BIGINT, deleted_at TIMESTAMP)`,
				`CREATE TABLE wiki_pages (id TEXT PRIMARY KEY, knowledge_base_id TEXT, tenant_id BIGINT, status TEXT, version INT, source_refs TEXT, chunk_refs TEXT, deleted_at TIMESTAMP)`,
				`CREATE TABLE knowledges (id TEXT PRIMARY KEY, knowledge_base_id TEXT, tenant_id BIGINT, title TEXT, deleted_at TIMESTAMP)`,
				`CREATE TABLE chunks (id TEXT PRIMARY KEY, knowledge_id TEXT, knowledge_base_id TEXT, tenant_id BIGINT, content TEXT, content_revision INT, chunk_index INT, is_enabled BOOLEAN, deleted_at TIMESTAMP)`,
				`INSERT INTO knowledge_bases (id,tenant_id) VALUES ('kb',1)`,
				`INSERT INTO wiki_pages (id,knowledge_base_id,tenant_id,status,version) VALUES ('page','kb',1,'published',1)`,
			} {
				require.NoError(t, repo.db.Exec(sql).Error)
			}
			var sourceRefs, chunkRefs []string
			for d := 0; d < tc.documents; d++ {
				id := fmt.Sprintf("doc%d", d)
				sourceRefs = append(sourceRefs, id+"|Title")
				if !tc.unavailableDoc || d == 0 {
					require.NoError(t, repo.db.Exec(`INSERT INTO knowledges (id,knowledge_base_id,tenant_id,title) VALUES (?,'kb',1,'Title')`, id).Error)
				}
				for c := 0; c < tc.chunks; c++ {
					chunkID := fmt.Sprintf("%s-chunk%d", id, c)
					chunkRefs = append(chunkRefs, chunkID)
					require.NoError(t, repo.db.Exec(`INSERT INTO chunks (id,knowledge_id,knowledge_base_id,tenant_id,content,chunk_index,is_enabled) VALUES (?,?,'kb',1,'body',?,true)`, chunkID, id, c).Error)
				}
			}
			if tc.missingRef {
				chunkRefs = append(chunkRefs, "missing")
			}
			sources, err := json.Marshal(sourceRefs)
			require.NoError(t, err)
			refs, err := json.Marshal(chunkRefs)
			require.NoError(t, err)
			require.NoError(t, repo.db.Exec(`UPDATE wiki_pages SET source_refs=?,chunk_refs=?`, sources, refs).Error)
			adapter := RuntimeAdapter{DB: repo.db}
			ctx := types.WithCaller(context.Background(), types.Caller{TenantID: 1, UserID: "owner", Role: types.TenantRoleAdmin})
			out, err := adapter.Sources(ctx, Run{TenantID: 1}, []Evidence{{KBID: "kb", PageID: "page", Version: "1", SentToModel: true}})
			require.Len(t, out, tc.wantCount)
			if len(tc.wantIssues) == 0 {
				require.NoError(t, err)
			} else {
				var limitation *SourceReadError
				require.ErrorAs(t, err, &limitation)
				require.ElementsMatch(t, tc.wantIssues, limitation.Issues)
			}
		})
	}
}
