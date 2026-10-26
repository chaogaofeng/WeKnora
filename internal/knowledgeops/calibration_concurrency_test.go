package knowledgeops

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// A configuration update can commit after the model result was triaged but
// before SaveAnalysis acquires its run lock. Exercise the real worker boundary.
func TestCalibrationChangedBeforePersistencePausesAutomaticRouting(t *testing.T) {
	for _, change := range []string{"unchanged", "disable_auto", "reconfirm_budget", "model_config", "missing_model", "quality_sample_disabled"} {
		t.Run(change, func(t *testing.T) {
			repo, settings := calibrationFixture(t)
			settings.Enabled = true
			settings.CalibrationConfirmed = true
			if change == "quality_sample_disabled" {
				settings.SamplePercent = 100
			}
			ctx := context.Background()
			require.NoError(t, repo.SaveSettings(ctx, 1, settings))
			run := Run{ID: "before-persistence", TenantID: 1, MessageID: "before-persistence", Status: "completed", CaptureStatus: "complete", QAType: "rag-qa", Channel: "web", StartedAt: time.Now()}
			require.NoError(t, repo.CreateRun(ctx, &run))
			require.NoError(t, repo.db.Create(&Evidence{ID: "e", TenantID: 1, RunID: run.ID, Kind: "chunk", Content: "A", SentToModel: true}).Error)
			require.NoError(t, repo.Enqueue(ctx, run, 1))
			raw, err := repo.db.DB()
			require.NoError(t, err)
			raw.SetMaxOpenConns(2)
			changed := false
			require.NoError(t, repo.db.Callback().Query().Before("gorm:query").Register("test:change-before-analysis-lock", func(tx *gorm.DB) {
				if changed || tx.Statement.Table != (Run{}).TableName() || tx.Statement.Clauses["FOR"].Expression == nil {
					return
				}
				changed = true
				switch change {
				case "disable_auto", "quality_sample_disabled":
					settings.AutoRecordEnabled = false
					settings.CalibrationConfirmed = false
					require.NoError(t, repo.SaveSettings(ctx, 1, settings))
				case "reconfirm_budget":
					settings.MaxOutputTokens--
					require.NoError(t, repo.SaveSettings(ctx, 1, settings))
				case "model_config":
					require.NoError(t, repo.db.Model(&types.Model{}).Where("id = ?", settings.ModelID).UpdateColumn("name", "changed-before-save").Error)
				case "missing_model":
					require.NoError(t, repo.db.Delete(&types.Model{}, "id = ?", settings.ModelID).Error)
				}
			}))
			model := &fakeChat{payload: `{"verdict":"supported","claims":[{"text":"A","position":"answer","verdict":"supported","evidence_ids":["e"],"reason":"observed"}],"source_check":"not_checked","risk":"low","scope":"source support"}`}
			factory := snapshotModelFactory(repo.db, func(context.Context, *types.Model) (chat.Chat, error) { return model, nil })
			svc := NewService(repo, NewAnalyzer(factory, nil), func(context.Context, Run, []Evidence) error { return nil })
			svc.SetExecutionContext(func(ctx context.Context, run Run) (context.Context, error) {
				return context.WithValue(ctx, types.TenantIDContextKey, run.TenantID), nil
			})
			require.NoError(t, svc.RunOnce(ctx))
			require.True(t, changed, "update must occur at the analysis-save boundary")
			analyses, err := repo.Analyses(ctx, 1, run.ID)
			require.NoError(t, err)
			require.Len(t, analyses, 1)
			if change == "unchanged" {
				require.Equal(t, "auto_record", analyses[0].Route)
			} else {
				require.Equal(t, "human_review", analyses[0].Route)
			}
			require.Equal(t, "succeeded", analyses[0].Status)
			require.Equal(t, "supported", analyses[0].Report.Verdict)
			require.Equal(t, settings.ModelID, analyses[0].ModelID)
		})
	}
}

func calibrationPendingSave(t *testing.T, repo *Repository, settings Settings) (Task, Analysis) {
	t.Helper()
	now := time.Now().UTC()
	run := Run{ID: uuid.NewString(), TenantID: settings.TenantID, MessageID: uuid.NewString(), StartedAt: now, FinishedAt: &now, Status: "completed"}
	require.NoError(t, repo.CreateRun(context.Background(), &run))
	task := Task{ID: uuid.NewString(), TenantID: settings.TenantID, RunID: run.ID, Version: 1, Status: "running", LeaseToken: uuid.NewString(), CreatedAt: now, UpdatedAt: now}
	require.NoError(t, repo.db.Create(&task).Error)
	return task, Analysis{Status: "succeeded", Route: "auto_record", ModelID: settings.ModelID, calibrationModelFingerprint: settings.calibrationModelFingerprint, Report: Report{Verdict: "supported", Risk: "low"}}
}

// Invoked only by the existing disposable-schema PostgreSQL integration test.
func testPostgresCalibrationFinalization(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.AutoMigrate(&types.Model{}))
	repo := NewRepository(db)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	settings := DefaultSettings(42)
	settings.Enabled, settings.AutoRecordEnabled, settings.CalibrationConfirmed = true, true, true
	settings.ModelID, settings.CalibrationNote = "finalization-model", "isolated calibration fixture"
	require.NoError(t, db.Create(&types.Model{ID: settings.ModelID, TenantID: settings.TenantID, Name: "model-before-change", Type: types.ModelTypeKnowledgeQA, Status: types.ModelStatusActive}).Error)
	var err error
	settings.CalibrationSnapshot, err = repo.CalibrationSnapshot(ctx, settings.TenantID, settings.ModelID)
	require.NoError(t, err)

	t.Run("configuration_committed_while_waiting_for_run_lock", func(t *testing.T) {
		require.NoError(t, repo.SaveSettings(ctx, settings.TenantID, settings))
		initial, err := repo.Settings(ctx, settings.TenantID)
		require.NoError(t, err)
		task, analysis := calibrationPendingSave(t, repo, initial)
		blocker := db.WithContext(ctx).Begin()
		require.NoError(t, blocker.Error)
		defer blocker.Rollback()
		require.NoError(t, lockRun(blocker, task.TenantID, task.RunID))
		result := make(chan error, 1)
		go func() { result <- repo.SaveAnalysis(ctx, task, analysis, initial) }()
		select {
		case err := <-result:
			t.Fatalf("analysis bypassed the run lock: %v", err)
		case <-time.After(150 * time.Millisecond):
		}
		changed := initial
		changed.AutoRecordEnabled = false
		require.NoError(t, repo.SaveSettings(ctx, settings.TenantID, changed))
		require.NoError(t, blocker.Commit().Error)
		require.NoError(t, <-result)
		analyses, err := repo.Analyses(ctx, task.TenantID, task.RunID)
		require.NoError(t, err)
		require.Len(t, analyses, 1)
		require.Equal(t, "human_review", analyses[0].Route)
		require.Equal(t, "supported", analyses[0].Report.Verdict)
	})

	t.Run("settings_and_model_updates_wait_for_analysis_commit", func(t *testing.T) {
		require.NoError(t, repo.SaveSettings(ctx, settings.TenantID, settings))
		initial, err := repo.Settings(ctx, settings.TenantID)
		require.NoError(t, err)
		task, analysis := calibrationPendingSave(t, repo, initial)
		tx := db.WithContext(ctx).Begin()
		require.NoError(t, tx.Error)
		defer tx.Rollback()
		require.NoError(t, NewRepository(tx).SaveAnalysis(ctx, task, analysis, initial))
		settingsSaved, modelSaved := make(chan error, 1), make(chan error, 1)
		go func() {
			changed := initial
			changed.AutoRecordEnabled = false
			settingsSaved <- repo.SaveSettings(ctx, settings.TenantID, changed)
		}()
		go func() {
			modelSaved <- db.WithContext(ctx).Model(&types.Model{}).Where("id = ?", settings.ModelID).UpdateColumn("name", "model-after-commit").Error
		}()
		select {
		case err := <-settingsSaved:
			t.Fatalf("settings changed before the analysis committed: %v", err)
		case err := <-modelSaved:
			t.Fatalf("model changed before the analysis committed: %v", err)
		case <-time.After(150 * time.Millisecond):
		}
		require.NoError(t, tx.Commit().Error)
		require.NoError(t, <-settingsSaved)
		require.NoError(t, <-modelSaved)
		analyses, err := repo.Analyses(ctx, task.TenantID, task.RunID)
		require.NoError(t, err)
		require.Len(t, analyses, 1)
		require.Equal(t, "auto_record", analyses[0].Route)
	})
}
