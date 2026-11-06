package knowledgeops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
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
	require.NoError(t, applyWikiAdoptionMigration(svc.repo.db, "sqlite", "up"))
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

func applyWikiAdoptionMigration(db *gorm.DB, dialect, suffix string) error {
	name := "000040_knowledge_ops_wiki_adoptions"
	if dialect == "versioned" {
		name = "000121_knowledge_ops_wiki_adoptions"
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "migrations", dialect, name+"."+suffix+".sql"))
	if err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		for _, stmt := range strings.Split(string(raw), ";") {
			if strings.TrimSpace(stmt) != "" {
				if err := tx.Exec(stmt).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// Up/down must preserve historical FAQ publication identifiers and content,
// and database uniqueness must permit one snapshot per destination type.
func TestWikiAdoptionMigrationPreservesFAQAndSeparatesTargets(t *testing.T) {
	svc, _, ctx, run, a, r := adoptionFixture(t)
	first, err := svc.Adopt(ctx, 1, run.ID, adoptionInput(a, r))
	require.NoError(t, err)
	require.NoError(t, applyWikiAdoptionMigration(svc.repo.db, "sqlite", "down"))
	var legacy struct {
		ID       string
		EntryID  int64
		Question string
		Answer   string
	}
	require.NoError(t, svc.repo.db.Table("knowledge_ops_adoptions").First(&legacy).Error)
	require.Equal(t, first.ID, legacy.ID)
	require.Equal(t, first.EntryID, legacy.EntryID)
	require.Equal(t, first.Question, legacy.Question)
	require.Equal(t, first.Answer, legacy.Answer)
	require.NoError(t, applyWikiAdoptionMigration(svc.repo.db, "sqlite", "up"))
	var migrated Adoption
	require.NoError(t, svc.repo.db.First(&migrated, "id = ?", first.ID).Error)
	require.Equal(t, "faq", migrated.TargetType)
	require.Equal(t, first.EntryID, migrated.EntryID)
	require.Empty(t, migrated.WikiPageID)
	wiki := migrated
	wiki.ID = "separate-wiki"
	wiki.TargetType = "wiki"
	require.NoError(t, svc.repo.db.Create(&wiki).Error)
	duplicate := wiki
	duplicate.ID = "duplicate-wiki"
	require.Error(t, svc.repo.db.Create(&duplicate).Error)
	require.Error(t, applyWikiAdoptionMigration(svc.repo.db, "sqlite", "down"), "rollback cannot discard Wiki provenance")
	var survived Adoption
	require.NoError(t, svc.repo.db.First(&survived, "id = ?", wiki.ID).Error)
	require.Equal(t, "wiki", survived.TargetType)
	var count int64
	require.NoError(t, svc.repo.db.Model(&Adoption{}).Count(&count).Error)
	require.EqualValues(t, 2, count)
	require.NoError(t, svc.repo.db.Delete(&wiki).Error)
	require.NoError(t, applyWikiAdoptionMigration(svc.repo.db, "sqlite", "down"))
	require.NoError(t, applyWikiAdoptionMigration(svc.repo.db, "sqlite", "up"))
	require.NoError(t, svc.repo.db.First(&migrated, "id = ?", first.ID).Error)
	require.Equal(t, first.EntryID, migrated.EntryID)
}
