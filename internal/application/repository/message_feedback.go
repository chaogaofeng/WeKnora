package repository

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type messageFeedbackRepository struct {
	db *gorm.DB
}

// NewMessageFeedbackRepository creates a message-feedback repository.
func NewMessageFeedbackRepository(db *gorm.DB) interfaces.MessageFeedbackRepository {
	return &messageFeedbackRepository{db: db}
}

// UpsertForMessage collapses the (tenant, session, message, from_source) tuple
// to a single row: a matching row is updated in place, otherwise a new one is
// created. This lets a user re-rate a message without spawning duplicates.
func (r *messageFeedbackRepository) UpsertForMessage(
	ctx context.Context, fb *types.MessageFeedback,
) (*types.MessageFeedback, error) {
	var existing types.MessageFeedback
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND session_id = ? AND message_id = ? AND from_source = ?",
			fb.TenantID, fb.SessionID, fb.MessageID, fb.FromSource).
		First(&existing).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return nil, err
	}

	now := time.Now()
	if err == gorm.ErrRecordNotFound {
		if fb.ID == "" {
			fb.ID = uuid.New().String()
		}
		fb.CreatedAt = now
		fb.UpdatedAt = now
		if err := r.db.WithContext(ctx).Create(fb).Error; err != nil {
			return nil, err
		}
		return fb, nil
	}

	existing.Rating = fb.Rating
	existing.Content = fb.Content
	existing.CreatorID = fb.CreatorID
	existing.UpdatedAt = now
	if err := r.db.WithContext(ctx).Save(&existing).Error; err != nil {
		return nil, err
	}
	return &existing, nil
}

// Delete removes a feedback row within the tenant scope.
func (r *messageFeedbackRepository) Delete(ctx context.Context, tenantID uint64, id string) error {
	return r.db.WithContext(ctx).
		Where("tenant_id = ? AND id = ?", tenantID, id).
		Delete(&types.MessageFeedback{}).Error
}

// ListBySession returns all feedback for a session, newest first.
func (r *messageFeedbackRepository) ListBySession(
	ctx context.Context, tenantID uint64, sessionID string,
) ([]*types.MessageFeedback, error) {
	var rows []*types.MessageFeedback
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND session_id = ?", tenantID, sessionID).
		Order("created_at DESC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}
