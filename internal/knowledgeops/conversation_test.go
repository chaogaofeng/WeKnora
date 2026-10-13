package knowledgeops

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestConversationLinksTurnsAndChecksEachRunAccess(t *testing.T) {
	_, repo, engine := workbenchFixture(t)
	require.NoError(t, repo.db.Exec(`CREATE TABLE sessions (id TEXT PRIMARY KEY, tenant_id INTEGER, title TEXT, parent_session_id TEXT, deleted_at DATETIME)`).Error)
	require.NoError(t, repo.db.Exec(`CREATE TABLE messages (id TEXT PRIMARY KEY, session_id TEXT, request_id TEXT, role TEXT, created_at DATETIME, deleted_at DATETIME)`).Error)
	require.NoError(t, repo.db.Exec(`INSERT INTO sessions VALUES ('s',1,'合作目标','',NULL)`).Error)
	start := time.Now().UTC().Add(-time.Hour)
	for i, id := range []string{"first", "private", "current"} {
		seedWorkbench(t, repo, id, "low", "human_review", "", 3-i)
		require.NoError(t, repo.db.Model(&Run{}).Where("id = ?", id).Updates(map[string]any{"session_id": "s", "request_id": id, "question": "question-" + id, "answer": "answer-" + id}).Error)
		at := start.Add(time.Duration(i) * time.Minute)
		require.NoError(t, repo.db.Exec(`INSERT INTO messages VALUES (?, 's', ?, 'user', ?, NULL)`, id+"-user", id, at).Error)
		require.NoError(t, repo.db.Exec(`INSERT INTO messages VALUES (?, 's', ?, 'assistant', ?, NULL)`, id, id, at.Add(time.Second)).Error)
	}
	// An assistant retry in the same request is not another user turn.
	require.NoError(t, repo.db.Exec(`INSERT INTO messages VALUES ('retry', 's', 'first', 'assistant', ?, NULL)`, start.Add(2*time.Second)).Error)
	page := workbenchGET(t, engine, "/runs?search=question-current")
	meta, ok := page["items"].([]any)[0].(map[string]any)["conversation"].(map[string]any)
	require.True(t, ok, "list must identify the conversation and turn")
	require.EqualValues(t, 3, meta["turn_number"])
	require.Equal(t, true, meta["turn_known"])
	groups := workbenchGET(t, engine, "/groups")
	require.NotNil(t, groups["items"].([]any)[0].(map[string]any)["representative"].(map[string]any)["conversation"])
	v := workbenchGET(t, engine, "/runs/current/conversation")
	require.Equal(t, "合作目标", v["title"])
	turns := v["turns"].([]any)
	require.Len(t, turns, 3)
	require.Equal(t, "question-first", turns[0].(map[string]any)["question"])
	require.Equal(t, "unavailable", turns[1].(map[string]any)["status"])
	require.NotContains(t, turns[1].(map[string]any), "question")
	require.Equal(t, true, turns[2].(map[string]any)["current"])
	// The current run's access check gates the entire conversation endpoint.
	req := httptest.NewRequest("GET", "/ops/runs/private/conversation", nil).WithContext(types.WithCaller(context.Background(), types.Caller{TenantID: 1, UserID: "u", Role: types.TenantRoleAdmin}))
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	require.Equal(t, 404, w.Code)
	require.NoError(t, repo.db.Exec(`UPDATE messages SET deleted_at = ? WHERE id = 'first-user'`, time.Now()).Error)
	v = workbenchGET(t, engine, "/runs/current/conversation")
	require.Equal(t, false, v["turn_known"], "deleted history must not fabricate an exact ordinal")
	require.NoError(t, repo.db.Exec(`UPDATE messages SET deleted_at = NULL WHERE id = 'first-user'`).Error)
	require.NoError(t, repo.db.Exec(`UPDATE messages SET deleted_at = ? WHERE id = 'first'`, time.Now()).Error)
	v = workbenchGET(t, engine, "/runs/current/conversation")
	first := v["turns"].([]any)[0].(map[string]any)
	require.Equal(t, "unavailable", first["status"])
	require.NotContains(t, first, "answer", "a deleted assistant message cannot be exposed through the conversation panel")
	require.NoError(t, repo.db.Exec(`DELETE FROM messages WHERE id = 'current-user'`).Error)
	require.NoError(t, repo.db.Exec(`UPDATE messages SET deleted_at = ? WHERE id = 'current'`, time.Now()).Error)
	v = workbenchGET(t, engine, "/runs/current/conversation")
	items := v["turns"].([]any)
	current := items[len(items)-1].(map[string]any)
	require.Equal(t, true, current["current"])
	require.Equal(t, "unavailable", current["status"])
	require.NotContains(t, current, "answer", "missing user identity must not bypass the deleted assistant guard")
}

func TestConversationUsesAssistantIdentityForAutoSteerFollowUps(t *testing.T) {
	_, repo, engine := workbenchFixture(t)
	require.NoError(t, repo.db.Exec(`CREATE TABLE sessions (id TEXT PRIMARY KEY, tenant_id INTEGER, title TEXT, parent_session_id TEXT, deleted_at DATETIME)`).Error)
	require.NoError(t, repo.db.Exec(`CREATE TABLE messages (id TEXT PRIMARY KEY, session_id TEXT, request_id TEXT, role TEXT, created_at DATETIME, deleted_at DATETIME)`).Error)
	require.NoError(t, repo.db.Exec(`INSERT INTO sessions VALUES ('s',1,'steer session','',NULL)`).Error)
	start := time.Now().UTC().Add(-time.Hour)
	for i, id := range []string{"original", "followup", "later"} {
		seedWorkbench(t, repo, id, "low", "human_review", "", 3-i)
		msgRequest := "req-" + id
		capturedRequest := msgRequest
		if id == "followup" {
			capturedRequest = "req-original"
		}
		at := start.Add(time.Duration(i) * time.Minute)
		require.NoError(t, repo.db.Model(&Run{}).Where("id = ?", id).Updates(map[string]any{"session_id": "s", "request_id": capturedRequest, "question": "question-" + id, "answer": "answer-" + id, "started_at": at.Add(2 * time.Second)}).Error)
		require.NoError(t, repo.db.Exec(`INSERT INTO messages VALUES (?, 's', ?, 'user', ?, NULL)`, id+"-user", msgRequest, at).Error)
		require.NoError(t, repo.db.Exec(`INSERT INTO messages VALUES (?, 's', ?, 'assistant', ?, NULL)`, id, msgRequest, at.Add(time.Second)).Error)
	}
	t.Run("current followup", func(t *testing.T) {
		result := workbenchGET(t, engine, "/runs/followup/conversation")
		require.Equal(t, true, result["turn_known"])
		require.EqualValues(t, 2, result["turn_number"])
		turns := result["turns"].([]any)
		require.Equal(t, "available", turns[len(turns)-1].(map[string]any)["status"])
		require.Equal(t, "question-followup", turns[len(turns)-1].(map[string]any)["question"])
		require.Equal(t, "question-original", turns[0].(map[string]any)["question"])
	})
	t.Run("later history", func(t *testing.T) {
		result := workbenchGET(t, engine, "/runs/later/conversation")
		turns := result["turns"].([]any)
		require.Equal(t, "question-original", turns[0].(map[string]any)["question"])
		require.Equal(t, "question-followup", turns[1].(map[string]any)["question"])
	})
}
