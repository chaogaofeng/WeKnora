package agent

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/knowledgeops/capture"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type opsInputRecorder struct {
	inputs  []capture.Input
	sources []capture.Source
	gaps    []string
}

func (r *opsInputRecorder) RecordInput(_ context.Context, in capture.Input) {
	r.inputs = append(r.inputs, in)
}
func (r *opsInputRecorder) RecordSources(_ context.Context, ss []capture.Source) {
	r.sources = append(r.sources, ss...)
}
func (r *opsInputRecorder) Finish(context.Context, capture.Completion) {}
func (r *opsInputRecorder) Gap(_ context.Context, s string)            { r.gaps = append(r.gaps, s) }
func TestAgentKnowledgeOpsCapturesAfterModelEncoding(t *testing.T) {
	r := &opsInputRecorder{}
	ctx := capture.WithRecorder(context.Background(), r)
	model := &mockChat{responses: []mockResponse{{chunks: []types.StreamResponse{{ResponseType: types.ResponseTypeAnswer, Content: "done", Done: true}}}}}
	e := newTestEngine(t, model)
	const ref = "resource://AbCdEfGhIjKlMnOpQrStUv"
	_, err := e.streamLLMToEventBus(ctx, []chat.Message{{Role: "system", Content: "private prompt"}, {Role: "user", Content: "read " + ref, Images: []string{"image"}, ReasoningContent: "secret"}}, nil, nil)
	require.NoError(t, err)
	require.Len(t, r.inputs, 1)
	require.Equal(t, model.GetModelID(), r.inputs[0].ModelID)
	require.Equal(t, "read res://0001", r.inputs[0].Messages[0].Content)
	require.NotEmpty(t, r.gaps)
}
