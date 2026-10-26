package knowledgeops

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
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
	scopedDSN := dsn + " search_path=" + schema
	if u, parseErr := url.Parse(dsn); parseErr == nil && (u.Scheme == "postgres" || u.Scheme == "postgresql") {
		q := u.Query()
		q.Set("search_path", schema)
		u.RawQuery = q.Encode()
		scopedDSN = u.String()
	}
	db, err := gorm.Open(postgres.Open(scopedDSN), &gorm.Config{})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	defer raw.Close()
	var activeSchema string
	require.NoError(t, db.Raw("SELECT current_schema()").Scan(&activeSchema).Error)
	require.Equal(t, schema, activeSchema, "never run integration migrations against application tables")
	apply := func(suffix string) {
		names := []string{"000115_knowledge_ops", "000116_knowledge_ops_history", "000117_knowledge_ops_queries", "000118_knowledge_ops_adoptions", "000119_knowledge_ops_followups", "000120_knowledge_ops_calibration"}
		if suffix == "down" {
			for i, j := 0, len(names)-1; i < j; i, j = i+1, j-1 {
				names[i], names[j] = names[j], names[i]
			}
		}
		for _, name := range names {
			b, err := os.ReadFile(filepath.Join("..", "..", "migrations", "versioned", name+"."+suffix+".sql"))
			require.NoError(t, err)
			for _, stmt := range strings.Split(string(b), ";") {
				if strings.TrimSpace(stmt) != "" {
					require.NoError(t, db.Exec(stmt).Error)
				}
			}
		}
	}
	apply("up")
	require.True(t, db.Migrator().HasTable(&Adoption{}))
	require.True(t, db.Migrator().HasTable(&Followup{}))
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

	// Follow-up CAS shares the parent lock. The second operator must see the
	// committed predecessor before deciding whether their input is still current.
	var current Analysis
	require.NoError(t, db.Where("tenant_id = ? AND run_id = ?", 1, run.ID).Order("version DESC").First(&current).Error)
	followupCtx := types.WithCaller(ctx, types.Caller{TenantID: 1, UserID: "operator", Role: types.TenantRoleAdmin})
	access := func(context.Context, Run, []Evidence) error { return nil }
	input := FollowupInput{AnalysisID: current.ID, Route: "source_verification", Action: "resolved", Reason: "source investigated"}
	followupTx := db.Begin()
	require.NoError(t, followupTx.Error)
	defer followupTx.Rollback()
	first, err := (&Service{repo: NewRepository(followupTx), access: access}).AddFollowup(followupCtx, 1, run.ID, input)
	require.NoError(t, err)
	concurrent := make(chan error, 1)
	go func() {
		_, err := (&Service{repo: repo, access: access}).AddFollowup(followupCtx, 1, run.ID, input)
		concurrent <- err
	}()
	select {
	case err := <-concurrent:
		t.Fatalf("followup bypassed uncommitted parent lock: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	require.NoError(t, followupTx.Commit().Error)
	select {
	case err := <-concurrent:
		require.ErrorIs(t, err, ErrStale)
	case <-time.After(5 * time.Second):
		t.Fatal("followup did not resume after commit")
	}
	input.Action = "reopened"
	input.PreviousID = first.ID
	reopened, err := (&Service{repo: repo, access: access}).AddFollowup(followupCtx, 1, run.ID, input)
	require.NoError(t, err)
	require.Equal(t, first.ID, reopened.PreviousID)
	t.Run("calibration_finalization", func(t *testing.T) { testPostgresCalibrationFinalization(t, db) })
	apply("down")
	require.False(t, db.Migrator().HasTable(&Run{}))
	require.False(t, db.Migrator().HasTable(&Adoption{}))
	require.False(t, db.Migrator().HasTable(&Followup{}))
	apply("up")
	require.True(t, db.Migrator().HasTable(&Run{}))
	require.True(t, db.Migrator().HasTable(&Adoption{}))
	require.True(t, db.Migrator().HasTable(&Followup{}))
}
