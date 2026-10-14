package capture

import (
	"context"
	"strings"
)

type rewriteKey struct{}

// rewriteState belongs to one synchronous query-understanding stage. It holds
// observation metadata only: it never changes the query or parses model output.
type rewriteState struct {
	event            QueryEvent
	original         string
	historyAvailable bool
	finished         bool
}

// BeginRewrite keeps the optional observer out of pipeline method signatures.
// Finish must run before the next pipeline stage, which may finish the QA run.
func BeginRewrite(ctx context.Context, original string, enabled bool) (context.Context, func(string)) {
	if _, ok := FromContext(ctx).(queryRecorder); !ok {
		return ctx, func(string) {}
	}
	s := &rewriteState{
		event:    QueryEvent{Kind: "rewrite", Status: "failed", ErrorCode: "model_unavailable", HistoryStatus: "not_used"},
		original: original, historyAvailable: true,
	}
	if !enabled {
		s.event.Status, s.event.ErrorCode = "disabled", ""
	}
	ctx = context.WithValue(ctx, rewriteKey{}, s)
	return ctx, func(query string) {
		if s.finished {
			return
		}
		s.finished = true
		s.event.Query = query
		if s.event.Status == "unchanged" && strings.TrimSpace(s.original) != strings.TrimSpace(query) {
			s.event.Status = "rewritten"
		}
		RecordQuery(ctx, s.event)
	}
}

func rewriteFromContext(ctx context.Context) *rewriteState {
	s, _ := ctx.Value(rewriteKey{}).(*rewriteState)
	if s != nil && !s.finished {
		return s
	}
	return nil
}

func RecordRewriteHistory(ctx context.Context, available bool) {
	if s := rewriteFromContext(ctx); s != nil {
		s.historyAvailable = available
	}
}

// RecordRewriteInput observes the effective templates before substitution.
// Loaded history/attachments/memory count only when actually inserted into them.
func RecordRewriteInput(ctx context.Context, systemTemplate, userTemplate string, historyTurns int, hasAttachments, hasMemory bool) {
	if s := rewriteFromContext(ctx); s != nil {
		templates := systemTemplate + "\n" + userTemplate
		if strings.Contains(templates, "{{conversation}}") {
			s.event.HistoryTurns, s.event.HistoryStatus = historyTurns, "available"
			if !s.historyAvailable {
				s.event.HistoryStatus = "unavailable"
			}
		}
		usesQuery := strings.Contains(templates, "{{query}}")
		s.event.HasAttachments = usesQuery && hasAttachments
		s.event.HasMemory = usesQuery && hasMemory
	}
}

func RecordRewriteCall(ctx context.Context, modelID string, hasImages, failed bool) {
	if s := rewriteFromContext(ctx); s != nil {
		s.event.ModelID, s.event.HasImages = modelID, hasImages
		s.event.ErrorCode = "invalid_output"
		if failed {
			s.event.ErrorCode = "model_error"
		}
	}
}

// RecordRewriteResult consumes the existing pipeline parser's result. Keeping
// parsing in QA avoids a second set of aliases/fallback rules drifting over time.
func RecordRewriteResult(ctx context.Context, accepted bool) {
	if s := rewriteFromContext(ctx); s != nil && accepted {
		s.event.Status, s.event.ErrorCode = "unchanged", ""
	}
}
