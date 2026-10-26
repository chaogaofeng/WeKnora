package knowledgeops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFollowupMigrationRollbackReplayAndLinearHistory(t *testing.T) {
	repo := testStore(t)
	for _, suffix := range []string{"down", "up"} {
		body, err := os.ReadFile(filepath.Join("..", "..", "migrations", "sqlite", "000038_knowledge_ops_followups."+suffix+".sql"))
		require.NoError(t, err)
		for _, stmt := range strings.Split(string(body), ";") {
			if strings.TrimSpace(stmt) != "" {
				require.NoError(t, repo.db.Exec(stmt).Error)
			}
		}
	}
	require.True(t, repo.db.Migrator().HasTable("knowledge_ops_followups"))
	seedWorkbench(t, repo, "migration", "low", "technical_followup", "", 0)
	insert := `INSERT INTO knowledge_ops_followups (id,tenant_id,run_id,analysis_id,route,action,reason,actor_id,previous_id,created_at) VALUES (?,1,'migration','migration-a','technical_followup','resolved','investigated','operator',?,CURRENT_TIMESTAMP)`
	require.NoError(t, repo.db.Exec(insert, "first", "").Error)
	require.Error(t, repo.db.Exec(insert, "duplicate-root", "").Error, "one root record per analysis route")
	require.NoError(t, repo.db.Exec(insert, "second", "first").Error)
	require.Error(t, repo.db.Exec(insert, "branch", "first").Error, "one successor per analysis route")
	var runs int64
	require.NoError(t, repo.db.Table("knowledge_ops_runs").Count(&runs).Error)
	require.EqualValues(t, 1, runs, "followup migration leaves capture tables intact")
}
