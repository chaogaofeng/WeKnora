package service

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/knowledgeops/capture"
	"github.com/Tencent/WeKnora/internal/modelcontext"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type sessionOpsRecorder struct {
	finish []capture.Completion
	gaps   []string
}

func (r *sessionOpsRecorder) RecordInput(context.Context, capture.Input)      {}
func (r *sessionOpsRecorder) RecordSources(context.Context, []capture.Source) {}
func (r *sessionOpsRecorder) Finish(_ context.Context, c capture.Completion) {
	r.finish = append(r.finish, c)
}
func (r *sessionOpsRecorder) Gap(_ context.Context, s string) { r.gaps = append(r.gaps, s) }

type sessionOpsSink struct {
	starts  []capture.Start
	rec     *sessionOpsRecorder
	enabled bool
}

func (s *sessionOpsSink) Start(_ context.Context, in capture.Start) capture.Recorder {
	if !s.enabled {
		return nil
	}
	s.starts = append(s.starts, in)
	return s.rec
}
func TestKnowledgeOpsAgentSetupFailureStillFinishesNewRun(t *testing.T) {
	sink := &sessionOpsSink{enabled: true, rec: &sessionOpsRecorder{}}
	svc := &sessionService{}
	svc.SetKnowledgeOpsSink(sink)
	req := &types.QARequest{Session: &types.Session{ID: "session", TenantID: 7}, Query: "new question", AssistantMessageID: "message"}
	ctx := types.WithPrincipal(context.Background(), types.Principal{Type: types.PrincipalWebUser, ID: "user"})
	ctx = context.WithValue(ctx, types.RequestIDContextKey, "request")
	err := svc.AgentQA(ctx, req, event.NewEventBus())
	require.Error(t, err)
	require.Len(t, sink.starts, 1)
	require.Len(t, sink.rec.finish, 1)
	require.Equal(t, "failed", sink.rec.finish[0].Status)
	require.Equal(t, uint64(7), sink.starts[0].TenantID)
	require.Equal(t, "new question", sink.starts[0].Question)
	require.Equal(t, "request", sink.starts[0].RequestID)
	require.Equal(t, "web", sink.starts[0].Channel)
	require.Equal(t, "smart-reasoning", sink.starts[0].ExecutionMode)
}
func TestKnowledgeOpsStartUsesSessionTenantAndPresetWithoutHistoryScan(t *testing.T) {
	for _, qaType := range []string{types.AgentTypeRAGQA, types.AgentTypeWikiQA, types.AgentTypeHybridRAGWiki} {
		sink := &sessionOpsSink{enabled: true, rec: &sessionOpsRecorder{}}
		svc := &sessionService{}
		svc.SetKnowledgeOpsSink(sink)
		req := &types.QARequest{Session: &types.Session{ID: "session", TenantID: 7, IMPlatform: "wecom"}, Query: "new turn", CustomAgent: &types.CustomAgent{ID: "agent", TenantID: 99, Config: types.CustomAgentConfig{AgentType: qaType, ModelID: "model"}}}
		_ = svc.startKnowledgeOps(context.Background(), req, "smart-reasoning")
		require.Len(t, sink.starts, 1)
		require.Equal(t, uint64(7), sink.starts[0].TenantID)
		require.Equal(t, qaType, sink.starts[0].QAType)
		require.Equal(t, "wecom", sink.starts[0].Channel)
	}
}

func TestKnowledgeOpsFixedFallbackRecordsActualAnswer(t *testing.T) {
	r := &sessionOpsRecorder{}
	ctx := capture.WithRecorder(context.Background(), r)
	svc := &sessionService{}
	cm := &types.ChatManage{PipelineRequest: types.PipelineRequest{FallbackResponse: "fixed fallback"}, PipelineContext: types.PipelineContext{EventBus: event.NewEventBus().AsEventBusInterface()}}
	svc.handleFixedFallback(ctx, cm)
	require.Len(t, r.finish, 1)
	require.Equal(t, "completed", r.finish[0].Status)
	require.Equal(t, "fixed fallback", r.finish[0].Answer)
}

type fallbackOpsRecorder struct{ finish chan capture.Completion }

func (r *fallbackOpsRecorder) RecordInput(context.Context, capture.Input)      {}
func (r *fallbackOpsRecorder) RecordSources(context.Context, []capture.Source) {}
func (r *fallbackOpsRecorder) Finish(_ context.Context, c capture.Completion)  { r.finish <- c }
func (r *fallbackOpsRecorder) Gap(context.Context, string)                     {}
func TestKnowledgeOpsModelFallbackFinishesOnCancellation(t *testing.T) {
	r := &fallbackOpsRecorder{finish: make(chan capture.Completion, 1)}
	ctx, cancel := context.WithCancel(capture.WithRecorder(context.Background(), r))
	svc := &sessionService{}
	cm := &types.ChatManage{PipelineContext: types.PipelineContext{EventBus: event.NewEventBus().AsEventBusInterface()}}
	done := make(chan struct{})
	go func() {
		svc.consumeFallbackStream(ctx, cm, make(chan types.StreamResponse), modelcontext.NewRegistry(true))
		close(done)
	}()
	cancel()
	select {
	case c := <-r.finish:
		require.Equal(t, "cancelled", c.Status)
	case <-time.After(time.Second):
		t.Fatal("fallback cancellation not recorded")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("fallback stream goroutine leaked")
	}
}
