package chatpipeline

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/knowledgeops/capture"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type opsRecorder struct {
	mu      sync.Mutex
	inputs  []capture.Input
	sources []capture.Source
	finish  chan capture.Completion
	gaps    []string
}

func (r *opsRecorder) RecordInput(_ context.Context, in capture.Input) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inputs = append(r.inputs, in)
}
func (r *opsRecorder) RecordSources(_ context.Context, ss []capture.Source) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sources = append(r.sources, ss...)
}
func (r *opsRecorder) Finish(_ context.Context, c capture.Completion) { r.finish <- c }
func (r *opsRecorder) Gap(_ context.Context, s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gaps = append(r.gaps, s)
}
func TestKnowledgeOpsStreamCapturesEncodedInputAndTerminalStatus(t *testing.T) {
	for _, tc := range []struct {
		name           string
		chunks         []types.StreamResponse
		status, answer string
	}{
		{"completed", []types.StreamResponse{{ResponseType: types.ResponseTypeThinking, Content: "hidden reasoning"}, {ResponseType: types.ResponseTypeAnswer, Content: "actual answer", Done: true}}, "completed", "actual answer"},
		{"error", []types.StreamResponse{{ResponseType: types.ResponseTypeError, Content: "model failed"}}, "failed", ""},
		{"premature_close", []types.StreamResponse{{ResponseType: types.ResponseTypeAnswer, Content: "partial"}}, "failed", "partial"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &opsRecorder{finish: make(chan capture.Completion, 2)}
			ctx := capture.WithRecorder(context.Background(), r)
			cm := &types.ChatManage{PipelineRequest: types.PipelineRequest{SummaryConfig: types.SummaryConfig{Prompt: "private full prompt {contexts}"}}, PipelineState: types.PipelineState{MergeResult: []*types.SearchResult{{ID: "chunk-real", KnowledgeID: "doc-real", KnowledgeBaseID: "kb-real", Content: "model-visible evidence"}}, RenderedContexts: "{old}"}, PipelineContext: types.PipelineContext{EventBus: &syncEventBus{}}}
			cm.UserContent = "{old}\nquestion"
			p := &PluginChatCompletionStream{modelService: &stubModelService{model: &openStreamChat{closeStream: true, chunks: tc.chunks}}}
			require.Nil(t, p.OnEvent(ctx, types.CHAT_COMPLETION_STREAM, cm, func() *PluginError { return nil }))
			select {
			case c := <-r.finish:
				require.Equal(t, tc.status, c.Status)
				require.Equal(t, tc.answer, c.Answer)
			case <-time.After(time.Second):
				t.Fatal("run did not finish")
			}
			r.mu.Lock()
			defer r.mu.Unlock()
			require.Len(t, r.inputs, 1)
			all := ""
			for _, m := range r.inputs[0].Messages {
				all += m.Content
			}
			require.Contains(t, all, `id="c1"`)
			require.NotContains(t, all, "private full prompt")
			require.NotContains(t, all, "hidden reasoning")
			sent := false
			for _, s := range r.sources {
				if s.SentToModel && s.ChunkID == "chunk-real" {
					sent = true
					require.Contains(t, s.Content, "model-visible evidence")
				}
			}
			require.True(t, sent)
		})
	}
}
func TestKnowledgeOpsStreamCapturesCancelledRun(t *testing.T) {
	r := &opsRecorder{finish: make(chan capture.Completion, 1)}
	ctx, cancel := context.WithCancel(capture.WithRecorder(context.Background(), r))
	cm := &types.ChatManage{PipelineContext: types.PipelineContext{EventBus: &syncEventBus{}}}
	p := &PluginChatCompletionStream{modelService: &stubModelService{model: &openStreamChat{}}}
	require.Nil(t, p.OnEvent(ctx, types.CHAT_COMPLETION_STREAM, cm, func() *PluginError { return nil }))
	cancel()
	select {
	case c := <-r.finish:
		require.Equal(t, "cancelled", c.Status)
	case <-time.After(time.Second):
		t.Fatal("cancelled run did not finish")
	}
}

type opsSyncChat struct{ *openStreamChat }

func (m opsSyncChat) Chat(context.Context, []chat.Message, *chat.ChatOptions) (*types.ChatResponse, error) {
	return &types.ChatResponse{Content: "sync answer"}, nil
}
func TestKnowledgeOpsSyncCompletionCapturesRun(t *testing.T) {
	r := &opsRecorder{finish: make(chan capture.Completion, 1)}
	ctx := capture.WithRecorder(context.Background(), r)
	p := &PluginChatCompletion{modelService: &stubModelService{model: opsSyncChat{}}}
	require.Nil(t, p.OnEvent(ctx, types.CHAT_COMPLETION, &types.ChatManage{}, func() *PluginError { return nil }))
	select {
	case c := <-r.finish:
		require.Equal(t, "completed", c.Status)
		require.Equal(t, "sync answer", c.Answer)
	default:
		t.Fatal("sync run did not finish")
	}
}

func TestKnowledgeOpsStreamFinishesOnDoneWithoutWaitingForProviderClose(t *testing.T) {
	r := &opsRecorder{finish: make(chan capture.Completion, 1)}
	ctx, cancel := context.WithCancel(capture.WithRecorder(context.Background(), r))
	defer cancel()
	p := &PluginChatCompletionStream{modelService: &stubModelService{model: &openStreamChat{chunks: []types.StreamResponse{{ResponseType: types.ResponseTypeAnswer, Content: "answer", Done: true}}}}}
	cm := &types.ChatManage{PipelineContext: types.PipelineContext{EventBus: &syncEventBus{}}}
	require.Nil(t, p.OnEvent(ctx, types.CHAT_COMPLETION_STREAM, cm, func() *PluginError { return nil }))
	select {
	case c := <-r.finish:
		require.Equal(t, "completed", c.Status)
	case <-time.After(time.Second):
		t.Fatal("terminal answer not captured until channel closes")
	}
}

func TestKnowledgeOpsStreamCapturesDecoderTailAtEveryTerminalPath(t *testing.T) {
	for _, terminal := range []string{"completed", "failed", "cancelled"} {
		t.Run(terminal, func(t *testing.T) {
			r := &opsRecorder{finish: make(chan capture.Completion, 2)}
			ctx, cancel := context.WithCancel(capture.WithRecorder(context.Background(), r))
			defer cancel()
			bus := &syncEventBus{}
			chunks := []types.StreamResponse{{ResponseType: types.ResponseTypeAnswer, Content: "answer"}}
			if terminal == "completed" {
				chunks = append(chunks, types.StreamResponse{ResponseType: types.ResponseTypeAnswer, Done: true}, types.StreamResponse{ResponseType: types.ResponseTypeAnswer, Done: true})
			}
			if terminal == "failed" {
				chunks = append(chunks, types.StreamResponse{ResponseType: types.ResponseTypeError, Content: "provider failure"})
			}
			model := &openStreamChat{chunks: chunks, closeStream: terminal != "cancelled"}
			cm := &types.ChatManage{PipelineContext: types.PipelineContext{EventBus: bus}}
			p := &PluginChatCompletionStream{modelService: &stubModelService{model: model}}
			require.Nil(t, p.OnEvent(ctx, types.CHAT_COMPLETION_STREAM, cm, func() *PluginError { return nil }))
			if terminal == "cancelled" {
				// The final r is held by the resource decoder until cancellation flushes it.
				require.Eventually(t, func() bool { return len(bus.finalAnswerContents()) > 0 }, time.Second, time.Millisecond)
				require.Equal(t, []string{"answe"}, bus.finalAnswerContents())
				cancel()
			}
			select {
			case completion := <-r.finish:
				require.Equal(t, terminal, completion.Status)
				require.Equal(t, "answer", completion.Answer)
			case <-time.After(time.Second):
				t.Fatal("terminal run was not captured")
			}
			require.Eventually(t, func() bool { return strings.Join(bus.finalAnswerContents(), "") == "answer" }, time.Second, time.Millisecond)
			require.Empty(t, r.finish)
		})
	}
}
