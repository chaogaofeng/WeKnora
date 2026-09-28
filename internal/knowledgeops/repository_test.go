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
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func testStore(t *testing.T) *Repository {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { raw.Close() })
	data, err := os.ReadFile(filepath.Join("..", "..", "migrations", "sqlite", "000034_knowledge_ops.up.sql"))
	require.NoError(t, err)
	for _, sql := range strings.Split(string(data), ";") {
		if strings.TrimSpace(sql) != "" {
			require.NoError(t, db.Exec(sql).Error)
		}
	}
	return NewRepository(db)
}
func TestSettingsDefaultDisabledAndEnableTimeStable(t *testing.T) {
	r := testStore(t)
	ctx := context.Background()
	s, err := r.Settings(ctx, 1)
	require.NoError(t, err)
	require.False(t, s.Enabled)
	s.Enabled = true
	s.ModelID = "model"
	require.NoError(t, r.SaveSettings(ctx, 1, s))
	first, _ := r.Settings(ctx, 1)
	require.NotNil(t, first.CaptureEnabledAt)
	first.SamplePercent = 20
	require.NoError(t, r.SaveSettings(ctx, 1, first))
	next, _ := r.Settings(ctx, 1)
	require.Equal(t, *first.CaptureEnabledAt, *next.CaptureEnabledAt)
	other, _ := r.Settings(ctx, 2)
	require.False(t, other.Enabled)
}
func TestTaskLeaseAndTenantIsolation(t *testing.T) {
	r := testStore(t)
	ctx := context.Background()
	settings := DefaultSettings(1)
	settings.Enabled = true
	settings.ModelID = "model"
	require.NoError(t, r.SaveSettings(ctx, 1, settings))
	run := Run{ID: uuid.NewString(), TenantID: 1, MessageID: "m", Status: "completed", StartedAt: time.Now()}
	require.NoError(t, r.CreateRun(ctx, &run))
	require.NoError(t, r.Enqueue(ctx, run, 1))
	require.NoError(t, r.Enqueue(ctx, run, 1))
	var count int64
	require.NoError(t, r.db.Model(&Task{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	task, err := r.Claim(ctx, time.Now(), time.Minute)
	require.NoError(t, err)
	require.NotNil(t, task)
	other, err := r.Claim(ctx, time.Now(), time.Minute)
	require.NoError(t, err)
	require.Nil(t, other)
	recovered, err := r.Claim(ctx, time.Now().Add(2*time.Minute), time.Minute)
	require.NoError(t, err)
	require.Equal(t, task.ID, recovered.ID)
	require.NotEqual(t, task.LeaseToken, recovered.LeaseToken)
	require.Error(t, r.CompleteTask(ctx, *task))
	require.NoError(t, r.CompleteTask(ctx, *recovered))
	_, err = r.Run(ctx, 2, run.ID)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}
func TestReviewIsAppendOnlyAndBoundToAnalysis(t *testing.T) {
	r := testStore(t)
	ctx := context.Background()
	run := Run{ID: uuid.NewString(), TenantID: 1, MessageID: "m", StartedAt: time.Now()}
	require.NoError(t, r.CreateRun(ctx, &run))
	a := Analysis{ID: uuid.NewString(), RunID: run.ID, TenantID: 1, Version: 1, Report: Report{Verdict: "supported"}}
	require.NoError(t, r.db.Create(&a).Error)
	input := Review{RunID: run.ID, AnalysisID: a.ID, Target: "answer", Verdict: "supported", Action: "confirmed", Reason: "checked", ReviewerID: "u"}
	require.NoError(t, r.AddReview(ctx, 1, &input))
	second := input
	second.ID = ""
	second.Action = "corrected"
	second.Verdict = "partial"
	require.NoError(t, r.AddReview(ctx, 1, &second))
	require.NotEqual(t, input.ID, second.ID)
	var all []Review
	require.NoError(t, r.db.Order("created_at").Find(&all).Error)
	require.Len(t, all, 2)
	require.Equal(t, "supported", all[0].Verdict)
	invalid := input
	invalid.ID = ""
	invalid.Target = "knowledge_write"
	require.Error(t, r.AddReview(ctx, 1, &invalid))
	invalid = input
	invalid.ID = ""
	require.Error(t, r.AddReview(ctx, 2, &invalid))
}

func TestMigrationRollbackAndReapply(t *testing.T) {
	r := testStore(t)
	for _, name := range []string{"000034_knowledge_ops.down.sql", "000034_knowledge_ops.up.sql"} {
		body, err := os.ReadFile(filepath.Join("..", "..", "migrations", "sqlite", name))
		require.NoError(t, err)
		for _, stmt := range strings.Split(string(body), ";") {
			if strings.TrimSpace(stmt) != "" {
				require.NoError(t, r.db.Exec(stmt).Error)
			}
		}
	}
	for _, table := range []any{&Settings{}, &Run{}, &Evidence{}, &Analysis{}, &Review{}, &Task{}} {
		require.True(t, r.db.Migrator().HasTable(table))
	}
}
func TestDisabledTenantCannotStarveEnabledTask(t *testing.T) {
	r := testStore(t)
	ctx := context.Background()
	for i := 0; i < 25; i++ {
		run := Run{ID: uuid.NewString(), TenantID: 2, MessageID: uuid.NewString(), StartedAt: time.Now()}
		require.NoError(t, r.CreateRun(ctx, &run))
		require.NoError(t, r.Enqueue(ctx, run, 1))
	}
	s := DefaultSettings(1)
	s.Enabled = true
	s.ModelID = "m"
	require.NoError(t, r.SaveSettings(ctx, 1, s))
	run := Run{ID: uuid.NewString(), TenantID: 1, MessageID: uuid.NewString(), StartedAt: time.Now()}
	require.NoError(t, r.CreateRun(ctx, &run))
	require.NoError(t, r.Enqueue(ctx, run, 1))
	task, err := r.Claim(ctx, time.Now(), time.Minute)
	require.NoError(t, err)
	require.NotNil(t, task)
	require.Equal(t, run.ID, task.RunID)
}
func TestAutoRecordingRequiresCalibrationAndReviewsMatchTarget(t *testing.T) {
	r := testStore(t)
	ctx := context.Background()
	s := DefaultSettings(1)
	s.AutoRecordEnabled = true
	require.ErrorIs(t, r.SaveSettings(ctx, 1, s), ErrInvalid)
	s.CalibrationNote = "manually evaluated mode/channel/risk strata"
	require.NoError(t, r.SaveSettings(ctx, 1, s))
	run := Run{ID: uuid.NewString(), TenantID: 1, MessageID: uuid.NewString(), StartedAt: time.Now()}
	require.NoError(t, r.CreateRun(ctx, &run))
	a := Analysis{ID: uuid.NewString(), TenantID: 1, RunID: run.ID, Version: 1, Report: Report{Verdict: "partial", Causes: []Cause{{Category: "retrieval", Status: "suspected"}}}}
	require.NoError(t, r.db.Create(&a).Error)
	v := Review{RunID: run.ID, AnalysisID: a.ID, Target: "answer", Verdict: "supported", Action: "confirmed", Reason: "reason", ReviewerID: "u"}
	require.ErrorIs(t, r.AddReview(ctx, 1, &v), ErrInvalid)
	v.Action = "corrected"
	require.NoError(t, r.AddReview(ctx, 1, &v))
	v.ID = ""
	v.Target = "cause"
	v.Verdict = "suspected"
	v.Action = "confirmed"
	v.CauseCategory = "retrieval"
	require.NoError(t, r.AddReview(ctx, 1, &v))
	v.ID = ""
	v.CauseCategory = "generation"
	require.ErrorIs(t, r.AddReview(ctx, 1, &v), ErrInvalid)
}
