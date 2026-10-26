package knowledgeops

import (
	"context"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/models/utils/ollama"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// Provenance belongs to the immutable configuration used for this client, not
// to a later database read. No model/configuration content is sent to the UI.
type diagnosticChat interface{ chat.Chat }
type diagnosticModel struct {
	diagnosticChat
	fingerprint string
}

// NewDiagnosticModelFactory is an ops-only adapter. It reuses the public chat
// constructors without changing model management or the online answer path.
func NewDiagnosticModelFactory(db *gorm.DB, local *ollama.OllamaService, fallback ModelFactory) ModelFactory {
	return snapshotModelFactory(db, func(ctx context.Context, model *types.Model) (chat.Chat, error) {
		if model.Parameters.Provider == "weknoracloud" && (model.Parameters.AppID == "" || model.Parameters.AppSecret == "") {
			// Preserve the existing inherited-credential diagnosis path. Its
			// external configuration cannot authorize automatic recording here.
			if fallback == nil {
				return nil, ErrInvalid
			}
			return fallback(ctx, model.ID)
		}
		return chat.NewChat(chat.ConfigFromModel(model, model.Parameters.AppID, model.Parameters.AppSecret), local)
	})
}

func snapshotModelFactory(db *gorm.DB, build func(context.Context, *types.Model) (chat.Chat, error)) ModelFactory {
	return func(ctx context.Context, id string) (chat.Chat, error) {
		tenant, _ := types.TenantIDFromContext(ctx)
		if tenant == 0 {
			return nil, ErrForbidden
		}
		var model types.Model
		if err := db.WithContext(ctx).Where("id = ? AND (tenant_id = ? OR is_builtin = ?) AND status = ? AND type = ?", id, tenant, true, types.ModelStatusActive, types.ModelTypeKnowledgeQA).First(&model).Error; err != nil {
			return nil, err
		}
		fingerprint, _ := diagnosticModelFingerprint(model)
		client, err := build(ctx, &model)
		if err != nil {
			return nil, err
		}
		return &diagnosticModel{diagnosticChat: client, fingerprint: fingerprint}, nil
	}
}
