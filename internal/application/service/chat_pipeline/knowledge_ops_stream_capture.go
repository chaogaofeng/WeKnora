package chatpipeline

import (
	"context"
	"errors"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/knowledgeops/capture"
	"github.com/Tencent/WeKnora/internal/types"
)

// knowledgeOpsStreamCapture observes only this stream's emitted events. Keeping
// capture at this boundary includes decoder tails and presentation fallbacks
// without coupling it to the provider response transformations.
// It is owned exclusively by the stream consumer goroutine.
type knowledgeOpsStreamCapture struct {
	types.EventBusInterface
	ctx       context.Context
	answer    *capture.AnswerBuffer
	err       error
	completed bool
}

func captureKnowledgeOpsStream(ctx context.Context, bus types.EventBusInterface) (types.EventBusInterface, func()) {
	if capture.FromContext(ctx) == nil {
		return bus, func() {}
	}
	stream := &knowledgeOpsStreamCapture{EventBusInterface: bus, ctx: ctx, answer: capture.NewAnswerBuffer(ctx)}
	return stream, stream.finish
}

func (s *knowledgeOpsStreamCapture) Emit(ctx context.Context, evt types.Event) error {
	if !s.completed {
		switch evt.Type {
		case types.EventType(event.EventAgentFinalAnswer):
			if data, ok := evt.Data.(event.AgentFinalAnswerData); ok {
				s.answer.Append(data.Content)
				if data.Done {
					s.completed = true
					// Downstream terminal handlers may cancel the request; record its
					// completion first, as soon as the final displayed content is known.
					capture.Finish(s.ctx, s.answer.String(), s.err)
				}
			}
		case types.EventType(event.EventError):
			s.err = errors.New("model stream failed")
		}
	}
	return s.EventBusInterface.Emit(ctx, evt)
}

// finish runs after decoder flushing on cancellation or channel close. Done
// events have already finished their run, even if the provider stays open.
func (s *knowledgeOpsStreamCapture) finish() {
	if s.completed {
		return
	}
	if s.err == nil {
		s.err = s.ctx.Err()
		if s.err == nil {
			s.err = errors.New("model stream ended before completion")
			capture.Gap(s.ctx, "model_stream_incomplete")
		}
	}
	capture.Finish(s.ctx, s.answer.String(), s.err)
}
