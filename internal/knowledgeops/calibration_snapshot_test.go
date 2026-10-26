package knowledgeops

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type reviewUncalibratedChat struct{ calibrationChangingChat }

func (m *reviewUncalibratedChat) GetModelName() string { return "uncalibrated-model-B" }

func TestCalibrationFactoryWithoutSnapshotCannotAutomaticallyRecord(t *testing.T) {
	repo, settings := calibrationFixture(t)
	settings.Enabled, settings.CalibrationConfirmed = true, true
	ctx := context.Background()
	require.NoError(t, repo.SaveSettings(ctx, 1, settings))
	run := Run{ID: "review-aba", TenantID: 1, MessageID: "review-aba", Status: "completed", CaptureStatus: "complete", QAType: "rag-qa", Channel: "web", StartedAt: time.Now()}
	require.NoError(t, repo.CreateRun(ctx, &run))
	require.NoError(t, repo.db.Create(&Evidence{ID: "e", TenantID: 1, RunID: run.ID, Kind: "chunk", Content: "A", SentToModel: true}).Error)
	require.NoError(t, repo.Enqueue(ctx, run, 1))
	factory := func(context.Context, string) (chat.Chat, error) {
		// The model row changes after the worker loaded its initial Settings.
		require.NoError(t, repo.db.Model(&types.Model{}).Where("id = ?", settings.ModelID).UpdateColumn("name", "uncalibrated-model-B").Error)
		// The client is constructed for B; an admin rolls the model row back
		// while B's request is in flight, before finalization takes its lock.
		return &reviewUncalibratedChat{calibrationChangingChat{
			fakeChat: fakeChat{payload: `{"verdict":"supported","claims":[{"text":"A","position":"answer","verdict":"supported","evidence_ids":["e"],"reason":"observed"}],"source_check":"not_checked","risk":"low","scope":"source support"}`},
			change: func() {
				require.NoError(t, repo.db.Model(&types.Model{}).Where("id = ?", settings.ModelID).UpdateColumn("name", "diagnosis").Error)
			},
		}}, nil
	}
	svc := NewService(repo, NewAnalyzer(factory, nil), func(context.Context, Run, []Evidence) error { return nil })
	require.NoError(t, svc.RunOnce(ctx))
	analyses, err := repo.Analyses(ctx, 1, run.ID)
	require.NoError(t, err)
	require.Len(t, analyses, 1)
	require.Equal(t, "uncalibrated-model-B", analyses[0].ModelName)
	t.Logf("actual model=%s, final route=%s", analyses[0].ModelName, analyses[0].Route)
	require.Equal(t, "human_review", analyses[0].Route)
}

func TestCalibrationOllamaEndpointChangeInvalidatesConfirmation(t *testing.T) {
	repo, settings := calibrationFixture(t)
	ctx := context.Background()
	require.NoError(t, repo.db.Model(&types.Model{}).Where("id = ?", settings.ModelID).UpdateColumn("source", types.ModelSourceLocal).Error)
	t.Setenv("OLLAMA_BASE_URL", "http://original-ollama:11434")
	settings.Enabled, settings.CalibrationConfirmed = true, true
	settings.CalibrationSnapshot, _ = repo.CalibrationSnapshot(ctx, 1, settings.ModelID)
	require.NoError(t, repo.SaveSettings(ctx, 1, settings))
	t.Setenv("OLLAMA_BASE_URL", "http://replacement-ollama:11434")
	current, err := repo.Settings(ctx, 1)
	require.NoError(t, err)
	t.Logf("after endpoint change: calibration=%s, route=%s", current.CalibrationStatus, calibrationRoute(current))
	require.Equal(t, "stale", current.CalibrationStatus)
}

func TestCalibrationStaleConfirmationRejectsUnseenConfiguration(t *testing.T) {
	repo, draft := calibrationFixture(t)
	draft.AutoRecordEnabled = false
	ctx := context.Background()
	require.NoError(t, repo.SaveSettings(ctx, 1, draft))
	draft, err := repo.Settings(ctx, 1)
	require.NoError(t, err)
	// Operator collected A samples and has settings open. A second operator edits the same model row.
	require.NoError(t, repo.db.Model(&types.Model{}).Where("id = ?", draft.ModelID).UpdateColumn("name", "unseen-model-B").Error)
	draft.CalibrationConfirmed = true
	draft.AutoRecordEnabled = true
	err = repo.SaveSettings(ctx, 1, draft)
	current, readErr := repo.Settings(ctx, 1)
	require.NoError(t, readErr)
	t.Logf("stale confirmation result=%v, status=%s, route=%s", err, current.CalibrationStatus, calibrationRoute(current))
	require.ErrorIs(t, err, ErrStale, "confirmation for A must not approve unseen configuration B")
}

func TestCalibrationActualClientSnapshotRejectsModelABA(t *testing.T) {
	repo, settings := calibrationFixture(t)
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	settings.Enabled, settings.CalibrationConfirmed = true, true
	require.NoError(t, repo.SaveSettings(ctx, 1, settings))
	settings, err := repo.Settings(ctx, 1)
	require.NoError(t, err)
	// A was calibrated. B is selected just before the client is constructed.
	require.NoError(t, repo.db.Model(&types.Model{}).Where("id = ?", settings.ModelID).UpdateColumn("name", "model-B").Error)
	factory := snapshotModelFactory(repo.db, func(_ context.Context, snapshot *types.Model) (chat.Chat, error) {
		require.Equal(t, "model-B", snapshot.Name)
		return &calibrationChangingChat{fakeChat: fakeChat{payload: `{"verdict":"supported","claims":[{"text":"A","position":"answer","verdict":"supported","evidence_ids":["e"],"reason":"observed"}],"source_check":"not_checked","risk":"low","scope":"source support"}`}, change: func() {
			// Restore A while B is answering. A final DB-only check would pass.
			require.NoError(t, repo.db.Model(&types.Model{}).Where("id = ?", settings.ModelID).UpdateColumn("name", "diagnosis").Error)
		}}, nil
	})
	analysis, err := NewAnalyzer(factory, nil).Analyze(ctx, Run{Status: "completed", CaptureStatus: "complete", QAType: "rag-qa", Channel: "web"}, []Evidence{{ID: "e", Kind: "chunk", Content: "A", SentToModel: true}}, settings)
	require.NoError(t, err)
	require.Equal(t, "succeeded", analysis.Status)
	require.Equal(t, "supported", analysis.Report.Verdict)
	require.NotEmpty(t, analysis.calibrationModelFingerprint)
	require.NotEqual(t, settings.calibrationModelFingerprint, analysis.calibrationModelFingerprint)
	require.Equal(t, "human_review", analysis.Route)
	current, err := repo.Settings(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, "valid", current.CalibrationStatus, "DB has reverted to A but B's result must not be approved")
	analysis.Route = "auto_record"
	require.NoError(t, repo.db.Transaction(func(tx *gorm.DB) error {
		return NewRepository(tx).recheckAutomaticCalibration(ctx, 1, settings, &analysis)
	}))
	require.Equal(t, "human_review", analysis.Route, "persistence independently checks actual client provenance")
}

func TestCalibrationConfirmationTokenIsRequiredAndBoundToTenantAndPolicy(t *testing.T) {
	for _, change := range []string{"missing", "forged", "tenant", "policy"} {
		t.Run(change, func(t *testing.T) {
			repo, settings := calibrationFixture(t)
			settings.CalibrationConfirmed = true
			switch change {
			case "missing":
				settings.CalibrationSnapshot = ""
			case "forged":
				settings.CalibrationSnapshot = "forged"
			case "tenant":
				require.NoError(t, repo.db.Model(&types.Model{}).Where("id = ?", settings.ModelID).Update("is_builtin", true).Error)
				var err error
				settings.CalibrationSnapshot, err = repo.CalibrationSnapshot(context.Background(), 2, settings.ModelID)
				require.NoError(t, err)
			case "policy":
				t.Setenv("KNOWLEDGE_OPS_SOURCE_MAX_CHARS", "49000")
			}
			require.ErrorIs(t, repo.SaveSettings(context.Background(), 1, settings), ErrStale)
		})
	}
}

func TestCalibrationSnapshotEndpointScopesAndDoesNotExposeConfiguration(t *testing.T) {
	repo, settings := calibrationFixture(t)
	engine := gin.New()
	NewService(repo, nil, func(context.Context, Run, []Evidence) error { return nil }).RegisterHTTP(engine.Group("/ops"))
	for _, tc := range []struct {
		tenant uint64
		role   types.TenantRole
		status int
	}{
		{1, types.TenantRoleAdmin, 200}, {2, types.TenantRoleAdmin, 404}, {1, types.TenantRoleViewer, 403},
	} {
		req := httptest.NewRequest("GET", "/ops/calibration-snapshot?model_id="+settings.ModelID, nil).WithContext(types.WithCaller(context.Background(), types.Caller{TenantID: tc.tenant, UserID: "reviewer", Role: tc.role}))
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)
		require.Equal(t, tc.status, w.Code, w.Body.String())
		if tc.status == 200 {
			var result struct {
				Data map[string]string `json:"data"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
			require.Equal(t, map[string]string{"model_id": settings.ModelID, "snapshot": settings.CalibrationSnapshot}, result.Data)
		}
	}
}

func TestCalibrationEndpointAndInheritedCredentialsPolicy(t *testing.T) {
	model := types.Model{Name: "diagnosis", Source: types.ModelSourceLocal}
	t.Setenv("OLLAMA_BASE_URL", "")
	defaultHash, err := diagnosticModelFingerprint(model)
	require.NoError(t, err)
	t.Setenv("OLLAMA_BASE_URL", "http://localhost:11434")
	explicitHash, err := diagnosticModelFingerprint(model)
	require.NoError(t, err)
	require.Equal(t, defaultHash, explicitHash)
	model.Source = types.ModelSourceRemote
	remoteHash, err := diagnosticModelFingerprint(model)
	require.NoError(t, err)
	t.Setenv("OLLAMA_BASE_URL", "http://another-local:11434")
	unchanged, err := diagnosticModelFingerprint(model)
	require.NoError(t, err)
	require.Equal(t, remoteHash, unchanged)
	model.Parameters.Provider = "weknoracloud"
	_, err = diagnosticModelFingerprint(model)
	require.ErrorIs(t, err, ErrInvalid)
	model.Parameters.AppID, model.Parameters.AppSecret = "per-model-app", "fixture-secret"
	_, err = diagnosticModelFingerprint(model)
	require.NoError(t, err)
}
