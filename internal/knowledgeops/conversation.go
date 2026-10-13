package knowledgeops

import (
	"context"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const conversationMetadataLimit = 200
const conversationDisplayLimit = 10

type Conversation struct {
	SessionID  string             `json:"session_id"`
	Title      string             `json:"title,omitempty"`
	TurnNumber int                `json:"turn_number"`
	TurnKnown  bool               `json:"turn_known"`
	HasEarlier bool               `json:"has_earlier"`
	Incomplete bool               `json:"incomplete"`
	Truncated  bool               `json:"truncated"`
	Status     string             `json:"status"`
	Turns      []ConversationTurn `json:"turns,omitempty"`
}

type ConversationTurn struct {
	RunID      string    `json:"run_id,omitempty"`
	Question   string    `json:"question,omitempty"`
	Answer     string    `json:"answer,omitempty"`
	TurnNumber int       `json:"turn_number"`
	Current    bool      `json:"current"`
	Status     string    `json:"status"`
	StartedAt  time.Time `json:"started_at"`
}

type conversationMessage struct {
	ID, SessionID, RequestID, Role string
	CreatedAt                      time.Time
	DeletedAt                      *time.Time
}

// Read session/message identity only, then load text from individually
// authorized operation runs. Never reconstruct model evidence from chat text.
// Callers must first authorize the current run using loadDetail.
func (s *Service) conversation(ctx context.Context, run Run, full bool) (*Conversation, error) {
	if run.SessionID == "" {
		return nil, nil
	}
	v := &Conversation{SessionID: run.SessionID, Status: "unavailable", Incomplete: true}
	var session struct{ Title, ParentSessionID string }
	q := s.repo.db.WithContext(ctx).Table("sessions").Select("title,parent_session_id").Where("id = ? AND tenant_id = ? AND deleted_at IS NULL", run.SessionID, run.TenantID).Take(&session)
	if errors.Is(q.Error, gorm.ErrRecordNotFound) {
		return v, nil
	}
	if q.Error != nil {
		return nil, q.Error
	}
	v.Title = session.Title
	var target conversationMessage
	err := s.repo.db.WithContext(ctx).Table("messages").Select("id,session_id,request_id,role,created_at,deleted_at").Where("id = ? AND session_id = ?", run.MessageID, run.SessionID).Take(&target).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return v, nil
	}
	if err != nil {
		return nil, err
	}
	var users []conversationMessage
	err = s.repo.db.WithContext(ctx).Table("messages").Select("id,request_id,created_at,deleted_at").Where("session_id = ? AND role = ? AND created_at <= ?", run.SessionID, "user", target.CreatedAt).Order("created_at DESC,id DESC").Limit(conversationMetadataLimit + 1).Find(&users).Error
	if err != nil {
		return nil, err
	}
	var deleted int64
	if err := s.repo.db.WithContext(ctx).Table("messages").Where("session_id = ? AND role IN ? AND created_at <= ? AND deleted_at IS NOT NULL", run.SessionID, []string{"user", "assistant"}, target.CreatedAt).Count(&deleted).Error; err != nil {
		return nil, err
	}
	v.Status, v.Incomplete = "available", false
	// Automatic follow-ups can retain the preceding request ID in capture's
	// context. The stored assistant message, not that request context, owns the turn.
	v.TurnKnown = deleted == 0 && len(users) <= conversationMetadataLimit && session.ParentSessionID == "" && target.DeletedAt == nil && target.Role == "assistant" && target.RequestID != ""
	seen := map[string]bool{}
	turns := []conversationMessage{}
	for i := len(users) - 1; i >= 0; i-- {
		m := users[i]
		if m.DeletedAt != nil || m.RequestID == "" {
			v.TurnKnown = false
		}
		if i > 0 && m.CreatedAt.Equal(users[i-1].CreatedAt) && m.RequestID != users[i-1].RequestID {
			v.TurnKnown = false
		}
		if m.RequestID != "" && seen[m.RequestID] {
			continue
		}
		seen[m.RequestID] = true
		turns = append(turns, m)
	}
	current := -1
	for i, m := range turns {
		if m.RequestID == target.RequestID {
			current = i
			break
		}
	}
	if current < 0 {
		v.TurnKnown = false
	} else {
		turns = turns[:current+1]
	}
	v.HasEarlier = current > 0 || session.ParentSessionID != "" || len(users) > conversationMetadataLimit
	if v.TurnKnown {
		v.TurnNumber = current + 1
	}
	v.Incomplete = !v.TurnKnown
	if !full {
		return v, nil
	}
	start := max(0, len(turns)-conversationDisplayLimit)
	v.Truncated = start > 0
	for i := start; i < len(turns); i++ {
		m := turns[i]
		item := ConversationTurn{Status: "unavailable", Current: m.RequestID == target.RequestID, StartedAt: m.CreatedAt}
		if v.TurnKnown {
			item.TurnNumber = i + 1
		}
		if m.DeletedAt == nil && m.RequestID != "" {
			var previous Run
			q := s.repo.db.WithContext(ctx).Model(&Run{}).Select("knowledge_ops_runs.*").
				Joins("JOIN messages AS answer ON answer.id = knowledge_ops_runs.message_id AND answer.session_id = knowledge_ops_runs.session_id").
				Where("knowledge_ops_runs.tenant_id = ? AND knowledge_ops_runs.session_id = ? AND knowledge_ops_runs.started_at <= ?", run.TenantID, run.SessionID, run.StartedAt).
				Where("answer.request_id = ? AND answer.role = ? AND answer.deleted_at IS NULL", m.RequestID, "assistant")
			if item.Current {
				q = q.Where("knowledge_ops_runs.id = ?", run.ID)
			}
			err = q.Order("knowledge_ops_runs.started_at DESC,knowledge_ops_runs.id DESC").Take(&previous).Error
			if err == nil {
				detail, accessErr := s.loadDetail(ctx, run.TenantID, previous.ID, false)
				if accessErr == nil {
					item.RunID, item.Question, item.Answer, item.Status = previous.ID, detail.Run.Question, detail.Run.Answer, "available"
				} else if !errors.Is(accessErr, ErrForbidden) && !errors.Is(accessErr, gorm.ErrRecordNotFound) {
					return nil, accessErr
				}
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, err
			}
		}
		if item.Status != "available" {
			v.Incomplete = true
		}
		v.Turns = append(v.Turns, item)
	}
	if current < 0 {
		item := ConversationTurn{Current: true, Status: "unavailable", StartedAt: run.StartedAt}
		if target.DeletedAt == nil && target.Role == "assistant" {
			item.RunID, item.Question, item.Answer, item.Status = run.ID, run.Question, run.Answer, "available"
		}
		v.Turns = append(v.Turns, item)
	}
	return v, nil
}

func (s *Service) populateConversation(ctx context.Context, run *Run) {
	v, err := s.conversation(ctx, *run, false)
	if err != nil {
		v = &Conversation{SessionID: run.SessionID, Status: "unavailable", Incomplete: true}
	}
	run.Conversation = v
}

func (s *Service) getConversation(c *gin.Context) {
	d, err := s.loadDetail(c.Request.Context(), tenant(c), c.Param("id"), false)
	if err != nil {
		reply(c, nil, err)
		return
	}
	v, err := s.conversation(c.Request.Context(), d.Run, true)
	reply(c, v, err)
}
