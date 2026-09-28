package chatpipeline

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/knowledgeops/capture"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestKnowledgeOpsStreamCaptureUsesFinalEventContentAndFinishesOnce(t *testing.T) {
	r := &opsRecorder{finish: make(chan capture.Completion, 3)}
	ctx, cancel := context.WithCancel(capture.WithRecorder(context.Background(), r))
	defer cancel()
	downstream := event.NewEventBus()
	var received []string
	downstream.On(event.EventAgentFinalAnswer, func(_ context.Context, evt event.Event) error {
		data := evt.Data.(event.AgentFinalAnswerData)
		received = append(received, data.Content)
		if data.Done {
			cancel()
		}
		return nil
	})
	bus, finish := captureKnowledgeOpsStream(ctx, downstream.AsEventBusInterface())
	// The capture boundary sees content after decoding or a fallback has replaced
	// the provider's empty response, without depending on either transformation.
	for _, data := range []event.AgentFinalAnswerData{{Content: "output limit fallback", Done: true}, {Done: true}} {
		require.NoError(t, bus.Emit(ctx, types.Event{Type: types.EventType(event.EventAgentFinalAnswer), Data: data}))
	}
	finish()
	require.Equal(t, []string{"output limit fallback", ""}, received)
	require.Len(t, r.finish, 1)
	completion := <-r.finish
	require.Equal(t, "completed", completion.Status)
	require.Equal(t, "output limit fallback", completion.Answer)
}

func TestKnowledgeOpsStreamCaptureForwardsEventErrors(t *testing.T) {
	ctx := capture.WithRecorder(context.Background(), &opsRecorder{finish: make(chan capture.Completion, 1)})
	downstream := event.NewEventBus()
	failure := errors.New("subscriber failed")
	downstream.On(event.EventAgentThought, func(context.Context, event.Event) error { return failure })
	bus, _ := captureKnowledgeOpsStream(ctx, downstream.AsEventBusInterface())
	require.ErrorIs(t, bus.Emit(ctx, types.Event{Type: types.EventType(event.EventAgentThought), Data: event.AgentThoughtData{Content: "reasoning"}}), failure)
}
