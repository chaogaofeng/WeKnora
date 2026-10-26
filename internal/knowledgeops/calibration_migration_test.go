package knowledgeops

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCalibrationMigrationRollbackReplayPreservesHistoricalReports(t *testing.T) {
	repo, s := calibrationFixture(t)
	s.CalibrationConfirmed = true
	require.NoError(t, repo.SaveSettings(context.Background(), 1, s))
	seedWorkbench(t, repo, "historical", "low", "auto_record", "", 0)
	var before Analysis
	require.NoError(t, repo.db.First(&before, "id = ?", "historical-a").Error)
	for _, suffix := range []string{"down", "up"} {
		body, err := os.ReadFile(filepath.Join("..", "..", "migrations", "sqlite", "000039_knowledge_ops_calibration."+suffix+".sql"))
		require.NoError(t, err)
		for _, stmt := range strings.Split(string(body), ";") {
			if strings.TrimSpace(stmt) != "" {
				require.NoError(t, repo.db.Exec(stmt).Error)
			}
		}
		require.Equal(t, suffix == "up", repo.db.Migrator().HasColumn(&Settings{}, "calibration_binding"))
	}
	got, err := repo.Settings(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, "unconfirmed", got.CalibrationStatus)
	require.Equal(t, s.CalibrationNote, got.CalibrationNote)
	require.True(t, got.AutoRecordEnabled)
	require.Equal(t, "human_review", calibrationRoute(got))
	var after Analysis
	require.NoError(t, repo.db.First(&after, "id = ?", "historical-a").Error)
	require.Equal(t, before, after)
}
