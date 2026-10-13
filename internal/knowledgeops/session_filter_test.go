package knowledgeops

import (
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWorkbenchSessionFilterScopesListsCountsAndReviews(t *testing.T) {
	_, repo, engine := workbenchFixture(t)
	for index, entry := range []struct{ id, session string }{
		{"first", "session-1"},
		{"second", "session-1"},
		{"prefix-match", "session-10"},
		{"other", "session-2"},
		{"private", "session-1"},
		{"other-tenant", "session-1"},
	} {
		seedWorkbench(t, repo, entry.id, "low", "human_review", "shared-group", index)
		tenantID := uint64(1)
		if entry.id == "other-tenant" {
			tenantID = 2
		}
		require.NoError(t, repo.db.Model(&Run{}).Where("id = ?", entry.id).
			Updates(map[string]any{"session_id": entry.session, "tenant_id": tenantID}).Error)
		require.NoError(t, repo.db.Create(&Review{
			ID: entry.id + "-review", TenantID: tenantID, RunID: entry.id,
			AnalysisID: entry.id + "-a", Target: "answer", Action: "needs_more_evidence",
			CreatedAt: time.Now(),
		}).Error)
	}

	filter := "session_id=" + url.QueryEscape(" session-1 ")
	for _, path := range []string{"/runs?", "/runs?view=queue&", "/reviews?"} {
		t.Run(path, func(t *testing.T) {
			page := workbenchGET(t, engine, path+filter+"&page_size=1")
			require.EqualValues(t, 2, page["total"])
			require.Len(t, page["items"], 1)
			empty := workbenchGET(t, engine, path+"session_id=session-")
			require.EqualValues(t, 0, empty["total"], "session matching must be exact")
		})
	}
	stats := workbenchGET(t, engine, "/overview?"+filter)
	require.EqualValues(t, 2, stats["total"])
	require.EqualValues(t, 2, stats["pending_runs"])
	require.EqualValues(t, 2, stats["review_count"])
	groups := workbenchGET(t, engine, "/groups?"+filter)
	require.EqualValues(t, 1, groups["total"])
	require.EqualValues(t, 2, groups["items"].([]any)[0].(map[string]any)["count"])
	combined := workbenchGET(t, engine, "/runs?"+filter+"&search=second")
	require.EqualValues(t, 1, combined["total"])
	require.Equal(t, "second", combined["items"].([]any)[0].(map[string]any)["id"])
	require.EqualValues(t, 4, workbenchGET(t, engine, "/runs?session_id=%20")["total"])
}
