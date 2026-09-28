package knowledgeops

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Opt-in integration test: each invocation owns a fresh schema, never an
// application's tables. CI can point this at a disposable PostgreSQL instance.
func TestPostgresMigrationAndConcurrentReviewVersionGate(t *testing.T) {
	dsn := os.Getenv("KNOWLEDGE_OPS_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set KNOWLEDGE_OPS_TEST_POSTGRES_DSN to a disposable PostgreSQL instance")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	schema := "knowledge_ops_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	require.NoError(t, admin.Exec("CREATE SCHEMA "+schema).Error)
	defer func() {
		require.NoError(t, admin.Exec("DROP SCHEMA "+schema+" CASCADE").Error)
		raw, _ := admin.DB()
		raw.Close()
	}()
	db, err := gorm.Open(postgres.Open(dsn+" search_path="+schema), &gorm.Config{})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	defer raw.Close()
	apply := func(suffix string) {
		b, err := os.ReadFile(filepath.Join("..", "..", "migrations", "versioned", "000110_knowledge_ops."+suffix+".sql"))
		require.NoError(t, err)
		for _, stmt := range strings.Split(string(b), ";") {
			if strings.TrimSpace(stmt) != "" {
				require.NoError(t, db.Exec(stmt).Error)
			}
		}
	}
	apply("up")
	repo := NewRepository(db)
	ctx := context.Background()
	settings := DefaultSettings(1)
	settings.Enabled = true
	settings.ModelID = "fixture"
	require.NoError(t, repo.SaveSettings(ctx, 1, settings))
	now := time.Now()
	run := Run{ID: uuid.NewString(), TenantID: 1, MessageID: uuid.NewString(), StartedAt: now, FinishedAt: &now, Status: "completed"}
	require.NoError(t, repo.CreateRun(ctx, &run))
	old := Analysis{ID: uuid.NewString(), TenantID: 1, RunID: run.ID, Version: 1, Status: "succeeded", Report: Report{Verdict: "supported"}}
	require.NoError(t, db.Create(&old).Error)
	require.NoError(t, repo.Enqueue(ctx, run, 2))
	task, err := repo.Claim(ctx, now, time.Minute)
	require.NoError(t, err)
	require.NotNil(t, task)
	tx := db.Begin()
	require.NoError(t, tx.Error)
	defer tx.Rollback()
	require.NoError(t, NewRepository(tx).SaveAnalysis(ctx, *task, Analysis{Status: "succeeded", Report: Report{Verdict: "partial"}}))
	started := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		close(started)
		v := Review{RunID: run.ID, AnalysisID: old.ID, Target: "answer", Verdict: "supported", Action: "confirmed", Reason: "review old version", ReviewerID: "u"}
		result <- repo.AddReview(ctx, 1, &v)
	}()
	<-started
	select {
	case err := <-result:
		t.Fatalf("review bypassed uncommitted analysis lock: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	require.NoError(t, tx.Commit().Error)
	select {
	case err := <-result:
		require.ErrorIs(t, err, ErrStale)
	case <-time.After(5 * time.Second):
		t.Fatal("review did not resume after analysis commit")
	}
	var reviews int64
	require.NoError(t, db.Model(&Review{}).Count(&reviews).Error)
	require.Zero(t, reviews)
	apply("down")
	require.False(t, db.Migrator().HasTable(&Run{}))
	apply("up")
	require.True(t, db.Migrator().HasTable(&Run{}))
}
