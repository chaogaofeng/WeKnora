package capture

import (
	"context"
	"slices"

	"github.com/Tencent/WeKnora/internal/models/chat"
)

// HistoryInput contains metadata only. It never copies historical statements
// into unbound evidence, and a model/tool iteration is not a conversation turn.
type HistoryInput struct {
	Turns     int      `json:"turns"`
	TurnIDs   []string `json:"turn_ids,omitempty"`
	IDsKnown  bool     `json:"ids_known"`
	Compacted bool     `json:"compacted"`
}

// HistorySnapshot describes the last captured model input and whether any
// captured call used a compacted summary. Nil on old runs means unrecorded.
type HistorySnapshot struct {
	HistoryInput
	ModelCalls    int  `json:"model_calls"`
	EverCompacted bool `json:"ever_compacted"`
}

func historyInput(messages []chat.Message) *HistoryInput {
	h := &HistoryInput{IDsKnown: true}
	for _, m := range messages {
		if m.Kind == chat.MessageKindCompactionSummary {
			h.Compacted = true
		}
		if m.TurnID != "" && !slices.Contains(h.TurnIDs, m.TurnID) {
			h.TurnIDs = append(h.TurnIDs, m.TurnID)
		}
	}
	h.Turns = len(h.TurnIDs)
	return h
}

// RAG's shared history type has no durable turn IDs. Its actual history count
// is passed at the existing capture hook instead of inferred from message roles.
func RecordRAGModelInput(ctx context.Context, messages []chat.Message, encode func(string) string, modelID string, historyTurns int) {
	recordModelInput(ctx, messages, encode, &HistoryInput{Turns: max(0, historyTurns), IDsKnown: historyTurns == 0}, modelID)
}
