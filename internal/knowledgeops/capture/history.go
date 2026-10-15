package capture

import (
	"context"
	"slices"
	"strings"

	"github.com/Tencent/WeKnora/internal/models/chat"
)

const (
	HistoryAttachmentContextOmitted = "history_attachment_context_omitted"
	HistoryImageContextOmitted      = "history_image_context_omitted"
)

// Historical user messages may include attachments/captions appended by the
// answering pipeline. Stored run questions do not contain these supplements;
// binding the turn ID alone cannot recover the full historical input.
func recordHistoryContextGaps(ctx context.Context, m chat.Message, content string) {
	if m.TurnID == "" {
		return
	}
	Gap(ctx, "history_source_permissions_unbound")
	if m.Role != "user" {
		return
	}
	// Match the rendered wrappers, not ordinary questions about their labels.
	if strings.Contains(content, "\n\n<attachments>\n<instruction>") {
		Gap(ctx, HistoryAttachmentContextOmitted)
	}
	if strings.Contains(content, "\n\n[用户上传图片内容]\n") {
		Gap(ctx, HistoryImageContextOmitted)
	}
}

func imageContextGap(m chat.Message) string {
	if m.TurnID != "" && m.Role == "user" {
		return HistoryImageContextOmitted
	}
	return "image_context_omitted"
}

// HistoryInput contains metadata only. It never copies historical statements
// into unbound evidence, and a model/tool iteration is not a conversation turn.
type HistoryInput struct {
	Turns     int      `json:"turns"`
	TurnIDs   []string `json:"turn_ids,omitempty"`
	IDsKnown  bool     `json:"ids_known"`
	Compacted bool     `json:"compacted"`
	// SupplementsChecked means the capture hook checked this rendered input
	// for historical attachments/images. Older snapshots leave this false.
	SupplementsChecked bool `json:"supplements_checked,omitempty"`
}

// HistorySnapshot describes the last captured model input and whether any
// captured call used a compacted summary. Nil on old runs means unrecorded.
type HistorySnapshot struct {
	HistoryInput
	ModelCalls    int  `json:"model_calls"`
	EverCompacted bool `json:"ever_compacted"`
}

func historyInput(messages []chat.Message) *HistoryInput {
	h := &HistoryInput{IDsKnown: true, SupplementsChecked: true}
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

// Validate the identities attached to the actual rendered input against the
// history count. Older/custom producers without IDs remain explicitly unknown.
func RecordRAGModelInput(ctx context.Context, messages []chat.Message, encode func(string) string, modelID string, historyTurns int) {
	h := historyInput(messages)
	h.Turns = max(0, historyTurns)
	h.IDsKnown = len(h.TurnIDs) == h.Turns
	recordModelInput(ctx, messages, encode, h, modelID)
}
