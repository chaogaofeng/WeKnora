package knowledgeops

import (
	"context"
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
