package knowledgeops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// The digest binds approval to the evaluator configuration, without storing a
// model's credentials or copying its configuration into the operations module.
func (r *Repository) calibrationModelFingerprint(ctx context.Context, tenant uint64, id string) (string, error) {
	var model types.Model
	err := r.db.WithContext(ctx).Where("id = ? AND (tenant_id = ? OR is_builtin = ?) AND status = ? AND type = ?", id, tenant, true, types.ModelStatusActive, types.ModelTypeKnowledgeQA).First(&model).Error
	if err != nil {
		return "", err
	}
	return diagnosticModelFingerprint(model)
}

func diagnosticModelFingerprint(model types.Model) (string, error) {
	// Inherited cloud credentials are resolved outside this model snapshot. Keep
	// diagnosis available, but do not certify an incomplete automatic evaluator.
	if model.Parameters.Provider == "weknoracloud" && (model.Parameters.AppID == "" || model.Parameters.AppSecret == "") {
		return "", ErrInvalid
	}
	endpoint := ""
	if strings.EqualFold(string(model.Source), string(types.ModelSourceLocal)) {
		endpoint = os.Getenv("OLLAMA_BASE_URL")
		if endpoint == "" {
			endpoint = "http://localhost:11434"
		}
	}
	// Display metadata and loader timestamps do not change evaluator behavior.
	body, err := json.Marshal(struct {
		Name          string
		Type          types.ModelType
		Source        types.ModelSource
		Parameters    types.ModelParameters
		LocalEndpoint string
	}{model.Name, model.Type, model.Source, model.Parameters, endpoint})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:]), nil
}

// A concurrency token for the effective model and server policy seen by the
// operator. It is never accepted as approval by itself and exposes no secrets.
func calibrationSnapshot(tenant uint64, modelID, fingerprint string) string {
	if fingerprint == "" {
		return ""
	}
	return calibrationBinding(Settings{TenantID: tenant, ModelID: modelID, calibrationModelFingerprint: fingerprint}, SourceLookupPolicyFromEnv())
}

func (r *Repository) CalibrationSnapshot(ctx context.Context, tenant uint64, modelID string) (string, error) {
	fingerprint, err := r.calibrationModelFingerprint(ctx, tenant, modelID)
	if err != nil {
		return "", err
	}
	return calibrationSnapshot(tenant, modelID, fingerprint), nil
}

func calibrationBinding(s Settings, policy SourceLookupPolicy) string {
	if s.ModelID == "" || s.calibrationModelFingerprint == "" {
		return ""
	}
	body, _ := json.Marshal(struct {
		Tenant                                                                     uint64
		ModelID, ModelFingerprint, PolicyVersion, SourceSelection, CalibrationNote string
		MaxEvidenceChars, MaxOutputTokens, TimeoutSeconds, SamplePercent           int
		SourcePolicy                                                               SourceLookupPolicy
	}{s.TenantID, s.ModelID, s.calibrationModelFingerprint, PolicyVersion, sourceSelectionStrategy, strings.TrimSpace(s.CalibrationNote), s.MaxEvidenceChars, s.MaxOutputTokens, s.TimeoutSeconds, s.SamplePercent, policy.normalized()})
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:])
}

func calibrationValid(s Settings) bool {
	return strings.TrimSpace(s.CalibrationNote) != "" && s.CalibrationConfirmedAt != nil && s.CalibrationPolicyVersion == PolicyVersion && s.CalibrationModelID == s.ModelID && s.CalibrationBinding != "" && s.CalibrationBinding == calibrationBinding(s, SourceLookupPolicyFromEnv())
}

func (r *Repository) populateCalibration(ctx context.Context, s *Settings) {
	s.CalibrationConfirmed = false
	s.CalibrationSnapshot = ""
	s.calibrationModelFingerprint = ""
	s.CalibrationStatus = "unconfirmed"
	if s.CalibrationConfirmedAt != nil {
		s.CalibrationStatus = "stale"
	}
	if s.ModelID == "" {
		return
	}
	// Missing/revoked models invalidate auto eligibility without breaking capture,
	// settings reads or manual diagnosis routes. Database errors also fail closed.
	fingerprint, err := r.calibrationModelFingerprint(ctx, s.TenantID, s.ModelID)
	if err != nil {
		return
	}
	s.calibrationModelFingerprint = fingerprint
	s.CalibrationSnapshot = calibrationSnapshot(s.TenantID, s.ModelID, fingerprint)
	if calibrationValid(*s) {
		s.CalibrationStatus = "valid"
	}
}

// Settings writers and analysis finalization take locks in settings -> model
// order. Model updates/deletions honor the database row lock without coupling
// the shared model service to knowledge operations. SQLite omits these clauses
// and serializes the surrounding write transaction instead.
func (r *Repository) lockCalibrationModel(ctx context.Context, tenant uint64, id string) error {
	var model types.Model
	return r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "SHARE"}).Select("id").
		Where("id = ? AND (tenant_id = ? OR is_builtin = ?) AND status = ? AND type = ?", id, tenant, true, types.ModelStatusActive, types.ModelTypeKnowledgeQA).
		First(&model).Error
}

func (r *Repository) settingsForCalibrationUpdate(ctx context.Context, tenant uint64) (Settings, error) {
	current := DefaultSettings(tenant)
	if err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Find(&current, "tenant_id = ?", tenant).Error; err != nil {
		return current, err
	}
	current.CalibrationStatus = "unconfirmed"
	if current.CalibrationConfirmedAt == nil {
		return current, nil
	}
	current.CalibrationStatus = "stale"
	if err := r.lockCalibrationModel(ctx, tenant, current.ModelID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return current, nil
		}
		return current, err
	}
	r.populateCalibration(ctx, &current)
	return current, nil
}

// Call only inside SaveAnalysis's transaction after acquiring its run lock.
// Hold the settings/model locks through insertion, so configuration changes
// cannot commit between eligibility validation and the automatic diagnosis.
func (r *Repository) recheckAutomaticCalibration(ctx context.Context, tenant uint64, initial Settings, a *Analysis) error {
	if a.Route != "auto_record" && a.Route != "quality_sample" {
		return nil
	}
	current, err := r.settingsForCalibrationUpdate(ctx, tenant)
	if err != nil {
		return err
	}
	if initial.TenantID == tenant && a.ModelID == initial.ModelID && a.calibrationModelFingerprint != "" && a.calibrationModelFingerprint == initial.calibrationModelFingerprint && current.Enabled && current.AutoRecordEnabled && calibrationValid(current) && current.CalibrationBinding == initial.CalibrationBinding {
		return nil
	}
	a.Route = "human_review"
	a.RouteReason = "诊断期间配置或校准状态发生变化，保留人工复核"
	return nil
}
