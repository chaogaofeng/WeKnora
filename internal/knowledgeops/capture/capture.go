// Package capture is the optional, fail-open boundary between QA and knowledge
// operations. It has no persistence dependency and never scans stored turns.
package capture

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
)

type Sink interface {
	Start(context.Context, Start) Recorder
}
type Recorder interface {
	RecordInput(context.Context, Input)
	RecordSources(context.Context, []Source)
	Finish(context.Context, Completion)
	Gap(context.Context, string)
}
type Start struct {
	TenantID                                                                                             uint64
	ActorID, SessionID, MessageID, RequestID, Channel, QAType, ExecutionMode, AgentID, ModelID, Question string
	StartedAt                                                                                            time.Time
}
type Source struct {
	ID, Kind, KBID, KnowledgeID, ChunkID, PageID, Slug, Version, Content, Title, ToolCallID string
	AcquiredAt                                                                              time.Time
	Truncated, SentToModel, PostHoc                                                         bool
}
type Input struct {
	Messages []InputMessage
	ModelID  string
}
type InputMessage struct{ Role, Content, Name, ToolCallID string }
type Completion struct {
	Answer, Status string
	FinishedAt     time.Time
}

type recorderKey struct{}
type run struct {
	recorder Recorder
	mu       sync.Mutex
	sources  []Source
	finish   sync.Once
}

// Begin consults enabled state exactly once, before any setup/model work.
// Sink implementations should bound I/O and return nil when disabled.
func Begin(ctx context.Context, sink Sink, in Start) (out context.Context) {
	out = ctx
	if sink == nil {
		return
	}
	defer func() {
		if recover() != nil {
			logger.Warn(ctx, "Knowledge operations capture start failed")
		}
	}()
	if in.StartedAt.IsZero() {
		in.StartedAt = time.Now().UTC()
	}
	var questionChanged bool
	in.Question, questionChanged = sanitize(in.Question)
	rec := sink.Start(ctx, in)
	if rec != nil {
		out = WithRecorder(ctx, rec)
		if questionChanged {
			Gap(out, "sensitive_question_content_omitted")
		}
	}
	return
}
func WithRecorder(ctx context.Context, rec Recorder) context.Context {
	if rec == nil {
		return ctx
	}
	return context.WithValue(ctx, recorderKey{}, &run{recorder: rec})
}
func state(ctx context.Context) *run { r, _ := ctx.Value(recorderKey{}).(*run); return r }
func FromContext(ctx context.Context) Recorder {
	if r := state(ctx); r != nil {
		return r.recorder
	}
	return nil
}
func guarded(ctx context.Context, f func(Recorder)) {
	r := state(ctx)
	if r == nil {
		return
	}
	defer func() {
		if recover() != nil {
			logger.Warn(ctx, "Knowledge operations capture failed")
			gapSafely(ctx, r.recorder, "capture_callback_failed")
		}
	}()
	f(r.recorder)
}
func gapSafely(ctx context.Context, r Recorder, reason string) {
	defer func() { _ = recover() }()
	r.Gap(context.WithoutCancel(ctx), reason)
}
func Gap(ctx context.Context, reason string) {
	if r := state(ctx); r != nil {
		gapSafely(ctx, r.recorder, reason)
	}
}
func RecordSources(ctx context.Context, sources []Source) {
	r := state(ctx)
	if r == nil || len(sources) == 0 {
		return
	}
	if len(sources) > 512 {
		sources = sources[:512]
		Gap(ctx, "capture_candidate_count_budget")
	}
	ss := append([]Source(nil), sources...)
	for i := range ss {
		if ss[i].Version == "" {
			ss[i].Version = "unknown"
		}
		if ss[i].AcquiredAt.IsZero() {
			ss[i].AcquiredAt = time.Now().UTC()
		}
		var changed bool
		ss[i].Content, changed = sanitize(ss[i].Content)
		ss[i].Title, _ = sanitize(ss[i].Title)
		if changed {
			ss[i].Truncated = true
			Gap(ctx, "sensitive_source_content_omitted")
		}
	}
	r.mu.Lock()
	for _, s := range ss {
		if !s.SentToModel && !s.PostHoc {
			if len(r.sources) < 512 {
				s.Content = ""
				r.sources = append(r.sources, s)
			}
		}
	}
	full := len(r.sources) >= 512
	r.mu.Unlock()
	if full {
		Gap(ctx, "capture_candidate_count_budget")
	}
	guarded(ctx, func(rec Recorder) { rec.RecordSources(context.WithoutCancel(ctx), ss) })
}

// Finish is idempotent across setup errors, asynchronous stream completion and
// cancellation. A cancelled request still gets its terminal fact persisted.
func Finish(ctx context.Context, answer string, err error) {
	r := state(ctx)
	if r == nil {
		return
	}
	r.finish.Do(func() {
		status := "completed"
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			status = "cancelled"
		} else if err != nil || ctx.Err() != nil {
			status = "failed"
		}
		answer, changed := sanitize(answer)
		if changed {
			Gap(ctx, "sensitive_answer_content_omitted")
		}
		guarded(ctx, func(rec Recorder) {
			rec.Finish(context.WithoutCancel(ctx), Completion{Answer: answer, Status: status, FinishedAt: time.Now().UTC()})
		})
	})
}
