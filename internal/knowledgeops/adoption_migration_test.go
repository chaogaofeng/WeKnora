package knowledgeops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdoptionMigrationRollbackReapplyAndUniqueSnapshot(t *testing.T) {
	svc, _, ctx, run, a, r := adoptionFixture(t)
	got, err := svc.Adopt(ctx, 1, run.ID, adoptionInput(a, r))
	require.NoError(t, err)
	duplicate := got
	duplicate.ID = "another-publication"
	require.Error(t, svc.repo.db.Create(&duplicate).Error, "database uniqueness is the idempotency boundary")
	for _, suffix := range []string{"down", "up"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "migrations", "sqlite", "000037_knowledge_ops_adoptions."+suffix+".sql"))
		require.NoError(t, err)
		for _, sql := range strings.Split(string(raw), ";") {
			if strings.TrimSpace(sql) != "" {
				require.NoError(t, svc.repo.db.Exec(sql).Error)
			}
		}
	}
	require.True(t, svc.repo.db.Migrator().HasTable(&Adoption{}))
	count := int64(-1)
	require.NoError(t, svc.repo.db.Model(&Adoption{}).Count(&count).Error)
	require.Zero(t, count)
	// The re-applied table still admits a normal publication and recovers the
	// existing indexed FAQ rather than creating another entry.
	after, err := svc.Adopt(ctx, 1, run.ID, adoptionInput(a, r))
	require.NoError(t, err)
	require.Equal(t, "succeeded", after.Status)
	require.True(t, after.Reused)
	require.Equal(t, got.EntryID, after.EntryID)
}
