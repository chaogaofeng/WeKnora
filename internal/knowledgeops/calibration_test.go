package knowledgeops

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/Tencent/WeKnora/internal/knowledgeops/capture"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func calibrationFixture(t *testing.T) (*Repository, Settings) {
	t.Helper()
	repo := testStore(t)
	require.NoError(t, repo.db.Create(&types.Model{ID: "calibration-model", TenantID: 1, Name: "diagnosis", Type: types.ModelTypeKnowledgeQA, Status: types.ModelStatusActive}).Error)
	s := DefaultSettings(1)
	s.ModelID = "calibration-model"
	s.CalibrationNote = "人工样本已按模式、渠道、风险校准"
	s.AutoRecordEnabled = true
	s.SamplePercent = 0
	s.MaxOutputTokens = 4096
	s.TimeoutSeconds = 90
	var err error
	s.CalibrationSnapshot, err = repo.CalibrationSnapshot(context.Background(), 1, s.ModelID)
	require.NoError(t, err)
	return repo, s
}

func calibrationRoute(s Settings) string {
	route, _ := Triage(Run{ID: "calibration-run", Status: "completed", CaptureStatus: "complete", QAType: "rag-qa", Channel: "web"}, Report{Verdict: "supported", Risk: "low", SourceCheck: "consistent"}, s)
	return route
}

func TestCalibrationNoteAloneDoesNotAuthorizeAutomaticRecording(t *testing.T) {
	repo, s := calibrationFixture(t)
	require.ErrorIs(t, repo.SaveSettings(context.Background(), 1, s), ErrInvalid)
	// Simulate an existing legacy unbound preference from before this migration.
	require.NoError(t, repo.db.Create(&s).Error)
	got, err := repo.Settings(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, "human_review", calibrationRoute(got))
	_, reason := Triage(Run{ID: "calibration-run", Status: "completed", CaptureStatus: "complete", QAType: "rag-qa", Channel: "web"}, Report{Verdict: "supported", Risk: "low", SourceCheck: "consistent"}, got)
	require.Contains(t, reason, "校准")
}

func TestCalibrationConfirmationBindsCurrentConfiguration(t *testing.T) {
	repo, s := calibrationFixture(t)
	s.CalibrationConfirmed = true
	require.NoError(t, repo.SaveSettings(context.Background(), 1, s))
	got, err := repo.Settings(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, "valid", got.CalibrationStatus)
	require.Equal(t, PolicyVersion, got.CalibrationPolicyVersion)
	require.Equal(t, s.ModelID, got.CalibrationModelID)
	require.NotNil(t, got.CalibrationConfirmedAt)
	require.Equal(t, "auto_record", calibrationRoute(got))
	body, err := json.Marshal(got)
	require.NoError(t, err)
	require.NotContains(t, string(body), "calibration_confirmed\"")
	require.NotContains(t, string(body), "calibration_binding")
	require.NotContains(t, string(body), "fingerprint")
	// Ordinary saves preserve the original binding and confirmation time.
	require.NoError(t, repo.SaveSettings(context.Background(), 1, got))
	unchanged, err := repo.Settings(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, "valid", unchanged.CalibrationStatus)
	got.CalibrationNote = "更新业务样本说明"
	require.NoError(t, repo.SaveSettings(context.Background(), 1, got))
	next, err := repo.Settings(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, got.CalibrationConfirmedAt, next.CalibrationConfirmedAt)
	require.Equal(t, "更新业务样本说明", next.CalibrationNote)
	require.Equal(t, "stale", next.CalibrationStatus)
}

func TestCalibrationConfigurationChangesRequireFreshConfirmation(t *testing.T) {
	for _, field := range []string{"model_id", "model_config", "model_parameters", "policy_version", "evidence_budget", "output_budget", "timeout", "sample_percent", "source_policy", "missing_model", "revoked_model", "disabled_model", "model_type", "cross_tenant_model"} {
		t.Run(field, func(t *testing.T) {
			repo, s := calibrationFixture(t)
			s.CalibrationConfirmed = true
			ctx := context.Background()
			require.NoError(t, repo.SaveSettings(ctx, 1, s))
			got, err := repo.Settings(ctx, 1)
			require.NoError(t, err)
			switch field {
			case "model_id":
				got.ModelID = "another-model"
			case "model_config":
				require.NoError(t, repo.db.Model(&types.Model{}).Where("id = ?", s.ModelID).UpdateColumn("name", "changed-diagnosis").Error)
			case "model_parameters":
				require.NoError(t, repo.db.Model(&types.Model{}).Where("id = ?", s.ModelID).Update("parameters", types.ModelParameters{Provider: "changed-provider"}).Error)
			case "policy_version":
				require.NoError(t, repo.db.Model(&Settings{}).Where("tenant_id = ?", 1).Update("calibration_policy_version", "old-policy").Error)
			case "evidence_budget":
				got.MaxEvidenceChars++
			case "output_budget":
				got.MaxOutputTokens++
			case "timeout":
				got.TimeoutSeconds++
			case "sample_percent":
				got.SamplePercent++
			case "source_policy":
				t.Setenv("KNOWLEDGE_OPS_SOURCE_MAX_CHARS", "49000")
			case "missing_model":
				require.NoError(t, repo.db.Unscoped().Delete(&types.Model{}, "id = ?", s.ModelID).Error)
			case "revoked_model":
				require.NoError(t, repo.db.Delete(&types.Model{}, "id = ?", s.ModelID).Error)
			case "disabled_model":
				require.NoError(t, repo.db.Model(&types.Model{}).Where("id = ?", s.ModelID).Update("status", "download_failed").Error)
			case "model_type":
				require.NoError(t, repo.db.Model(&types.Model{}).Where("id = ?", s.ModelID).Update("type", "Embedding").Error)
			case "cross_tenant_model":
				require.NoError(t, repo.db.Model(&types.Model{}).Where("id = ?", s.ModelID).Update("tenant_id", 2).Error)
			}
			// A derived Settings value cannot remain eligible after in-memory budget edits.
			if field == "evidence_budget" || field == "output_budget" || field == "timeout" || field == "sample_percent" || field == "model_id" || field == "source_policy" {
				require.Equal(t, "human_review", calibrationRoute(got))
			}
			require.NoError(t, repo.SaveSettings(ctx, 1, got))
			next, err := repo.Settings(ctx, 1)
			require.NoError(t, err)
			require.Equal(t, "stale", next.CalibrationStatus)
			require.Equal(t, "human_review", calibrationRoute(next))
			require.Equal(t, s.CalibrationNote, next.CalibrationNote)
			require.True(t, next.AutoRecordEnabled, "stored preference remains explicit while stale approval fails closed")
		})
	}
}

func TestCalibrationUnchangedModelMetadataPreservesConfirmation(t *testing.T) {
	repo, s := calibrationFixture(t)
	s.CalibrationConfirmed = true
	ctx := context.Background()
	require.NoError(t, repo.SaveSettings(ctx, 1, s))
	// Built-in model loading touches updated_at on every application startup.
	require.NoError(t, repo.db.Model(&types.Model{}).Where("id = ?", s.ModelID).Updates(map[string]any{"updated_at": time.Now().Add(time.Hour), "display_name": "Display label", "description": "Updated description", "is_default": true}).Error)
	got, err := repo.Settings(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, "valid", got.CalibrationStatus)
	require.Equal(t, "auto_record", calibrationRoute(got))
}

func TestCalibrationClientBindingsCannotForgeOrTransferApproval(t *testing.T) {
	repo, s := calibrationFixture(t)
	s.AutoRecordEnabled = false
	s.CalibrationStatus = "valid"
	s.CalibrationPolicyVersion = PolicyVersion
	s.CalibrationModelID = s.ModelID
	now := time.Now()
	s.CalibrationConfirmedAt = &now
	require.NoError(t, repo.SaveSettings(context.Background(), 1, s))
	got, err := repo.Settings(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, "unconfirmed", got.CalibrationStatus)
	require.Nil(t, got.CalibrationConfirmedAt)
	require.Equal(t, "human_review", calibrationRoute(got))
	// Even a publicly forged status passed directly to Triage has no approval.
	s.AutoRecordEnabled = true
	require.Equal(t, "human_review", calibrationRoute(s))
	s.CalibrationConfirmed = true
	require.NoError(t, repo.SaveSettings(context.Background(), 1, s))
	s.CalibrationConfirmed = false
	s.MaxOutputTokens++
	require.NoError(t, repo.SaveSettings(context.Background(), 1, s))
	got, err = repo.Settings(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, "stale", got.CalibrationStatus)
	s.CalibrationConfirmed = true
	require.NoError(t, repo.SaveSettings(context.Background(), 1, s))
	got, err = repo.Settings(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, "valid", got.CalibrationStatus)
	require.Equal(t, "auto_record", calibrationRoute(got))
}

func TestCalibrationConfirmationNeedsNoteAndAvailableModel(t *testing.T) {
	repo, s := calibrationFixture(t)
	s.CalibrationConfirmed = true
	s.AutoRecordEnabled = false
	s.CalibrationNote = " "
	require.ErrorIs(t, repo.SaveSettings(context.Background(), 1, s), ErrInvalid)
	s.CalibrationNote = "business samples"
	s.ModelID = "not-found"
	require.ErrorIs(t, repo.SaveSettings(context.Background(), 1, s), ErrInvalid)
}

// Direct policy tests use this explicit server-derived fixture after their
// configuration setup; production settings can only obtain it via SaveSettings.
func withTestCalibration(s Settings) Settings {
	if s.ModelID == "" {
		s.ModelID = "test-id"
	}
	now := time.Now().UTC()
	s.CalibrationConfirmedAt = &now
	s.CalibrationModelID = s.ModelID
	s.CalibrationPolicyVersion = PolicyVersion
	s.calibrationModelFingerprint = "test-current-model-configuration"
	s.CalibrationBinding = calibrationBinding(s, SourceLookupPolicyFromEnv())
	s.CalibrationStatus = "valid"
	return s
}

func TestCalibrationRevertingSettingsDoesNotRestoreInvalidatedApproval(t *testing.T) {
	repo, s := calibrationFixture(t)
	s.CalibrationConfirmed = true
	ctx := context.Background()
	require.NoError(t, repo.SaveSettings(ctx, 1, s))
	s.CalibrationConfirmed = false
	s.MaxOutputTokens--
	require.NoError(t, repo.SaveSettings(ctx, 1, s))
	s.MaxOutputTokens++
	require.NoError(t, repo.SaveSettings(ctx, 1, s))
	got, err := repo.Settings(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, "stale", got.CalibrationStatus)
	require.Equal(t, "human_review", calibrationRoute(got))
}

func TestCalibrationUnavailableModelDoesNotDisableCaptureOrTaskClaim(t *testing.T) {
	repo, s := calibrationFixture(t)
	s.Enabled = true
	s.CalibrationConfirmed = true
	ctx := context.Background()
	require.NoError(t, repo.SaveSettings(ctx, 1, s))
	require.NoError(t, repo.db.Delete(&types.Model{}, "id = ?", s.ModelID).Error)
	got, err := repo.Settings(ctx, 1)
	require.NoError(t, err)
	require.True(t, got.Enabled)
	require.Equal(t, "stale", got.CalibrationStatus)
	svc := NewService(repo, nil, nil)
	rec := svc.Start(ctx, capture.Start{TenantID: 1, MessageID: "still-captured", StartedAt: time.Now().Add(time.Second)})
	require.NotNil(t, rec)
	rec.Finish(ctx, capture.Completion{Status: "completed", FinishedAt: time.Now().Add(2 * time.Second)})
	task, err := repo.Claim(ctx, time.Now(), time.Minute)
	require.NoError(t, err)
	require.NotNil(t, task)
}

func TestCalibrationHTTPRejectsForgedReadFieldsAndRequiresConfirmation(t *testing.T) {
	repo, s := calibrationFixture(t)
	engine := gin.New()
	NewService(repo, nil, nil).RegisterHTTP(engine.Group("/ops"))
	put := func(body map[string]any, want int) map[string]any {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		req := httptest.NewRequest("PUT", "/ops/settings", bytes.NewReader(raw)).WithContext(types.WithCaller(context.Background(), types.Caller{TenantID: 1, UserID: "u", Role: types.TenantRoleAdmin}))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)
		require.Equal(t, want, w.Code, w.Body.String())
		var reply map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &reply))
		return reply
	}
	raw, err := json.Marshal(s)
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, json.Unmarshal(raw, &body))
	body["calibration_status"] = "valid"
	body["calibration_model_id"] = s.ModelID
	body["calibration_policy_version"] = PolicyVersion
	body["calibration_confirmed_at"] = time.Now().Format(time.RFC3339)
	body["calibration_binding"] = "forged"
	put(body, 400)
	body["auto_record_enabled"] = false
	got := put(body, 200)["data"].(map[string]any)
	require.Equal(t, "unconfirmed", got["calibration_status"])
	require.Nil(t, got["calibration_confirmed_at"])
	body["calibration_confirmed"] = true
	body["auto_record_enabled"] = true
	got = put(body, 200)["data"].(map[string]any)
	require.Equal(t, "valid", got["calibration_status"])
	require.NotContains(t, got, "calibration_confirmed")
	require.NotContains(t, got, "calibration_binding")
	body["calibration_confirmed"] = false
	body["max_output_tokens"] = 4000
	got = put(body, 200)["data"].(map[string]any)
	require.Equal(t, "stale", got["calibration_status"])
	require.Equal(t, s.CalibrationNote, got["calibration_note"])
}

type calibrationChangingChat struct {
	fakeChat
	change func()
}

func (m *calibrationChangingChat) Chat(ctx context.Context, messages []chat.Message, options *chat.ChatOptions) (*types.ChatResponse, error) {
	m.change()
	return m.fakeChat.Chat(ctx, messages, options)
}

func TestCalibrationChangedDuringDiagnosisPausesAutomaticRouting(t *testing.T) {
	repo, s := calibrationFixture(t)
	s.Enabled = true
	s.CalibrationConfirmed = true
	ctx := context.Background()
	require.NoError(t, repo.SaveSettings(ctx, 1, s))
	run := Run{ID: "inflight-calibration", TenantID: 1, MessageID: "inflight", Status: "completed", CaptureStatus: "complete", QAType: "rag-qa", Channel: "web", StartedAt: time.Now()}
	require.NoError(t, repo.CreateRun(ctx, &run))
	require.NoError(t, repo.db.Create(&Evidence{ID: "e", TenantID: 1, RunID: run.ID, Kind: "chunk", Content: "A", SentToModel: true}).Error)
	require.NoError(t, repo.Enqueue(ctx, run, 1))
	model := &calibrationChangingChat{fakeChat: fakeChat{payload: `{"verdict":"supported","claims":[{"text":"A","position":"answer","verdict":"supported","evidence_ids":["e"],"reason":"observed"}],"source_check":"not_checked","risk":"low","scope":"source support"}`}, change: func() {
		require.NoError(t, repo.db.Model(&types.Model{}).Where("id = ?", s.ModelID).Update("name", "changed-during-diagnosis").Error)
	}}
	svc := NewService(repo, NewAnalyzer(func(context.Context, string) (chat.Chat, error) { return model, nil }, nil), func(context.Context, Run, []Evidence) error { return nil })
	require.NoError(t, svc.RunOnce(ctx))
	analyses, err := repo.Analyses(ctx, 1, run.ID)
	require.NoError(t, err)
	require.Len(t, analyses, 1)
	require.Equal(t, "succeeded", analyses[0].Status)
	require.Equal(t, "supported", analyses[0].Report.Verdict)
	require.Equal(t, "human_review", analyses[0].Route)
}
