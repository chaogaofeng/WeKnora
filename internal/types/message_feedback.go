package types

import (
	"time"

	"gorm.io/gorm"
)

// Feedback source discriminates who authored a conversation annotation.
const (
	// FeedbackSourceUser is a rating left by the end user on a message.
	FeedbackSourceUser = "user"
	// FeedbackSourceAdmin is an annotation/correction written by a tenant admin
	// in the conversation-log console.
	FeedbackSourceAdmin = "admin"
)

// MessageFeedback records a rating or free-text annotation on a single message.
// Each subject (user or admin) keeps at most one feedback per message, keyed by
// (tenant_id, session_id, message_id, from_source); Upsert collapses duplicates.
type MessageFeedback struct {
	ID         string         `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID   uint64         `json:"tenant_id"`
	SessionID  string         `json:"session_id"`
	MessageID  string         `json:"message_id"`
	FromSource string         `json:"from_source"`
	Rating     int            `json:"rating"` // 1 (positive), -1 (negative), 0 (neutral/none)
	Content    string         `json:"content"`
	CreatorID  string         `json:"creator_id"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
	DeletedAt  gorm.DeletedAt `json:"-" gorm:"index"`
}

// TableName pins the SQL table for GORM.
func (MessageFeedback) TableName() string { return "message_feedbacks" }
