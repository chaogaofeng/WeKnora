package capture

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/models/chat"
)

type memoryRecorder struct {
	inputs      []Input
	sources     []Source
	gaps        []string
	completions []Completion
	panicInput  bool
}

func (r *memoryRecorder) RecordInput(_ context.Context, in Input) {
	if r.panicInput {
		panic("storage unavailable")
	}
	r.inputs = append(r.inputs, in)
}
func (r *memoryRecorder) RecordSources(_ context.Context, ss []Source) {
	r.sources = append(r.sources, ss...)
}
func (r *memoryRecorder) Finish(_ context.Context, c Completion) {
	r.completions = append(r.completions, c)
}
func (r *memoryRecorder) Gap(_ context.Context, s string) { r.gaps = append(r.gaps, s) }

type memorySink struct {
	enabled    bool
	starts     []Start
	rec        *memoryRecorder
	panicStart bool
}

func (s *memorySink) Start(_ context.Context, in Start) Recorder {
	if s.panicStart {
		panic("db unavailable")
	}
	if !s.enabled {
		return nil
	}
	s.starts = append(s.starts, in)
	return s.rec
}

func TestNilAndDisabledCaptureAreNoOp(t *testing.T) {
	ctx := Begin(context.Background(), nil, Start{})
	RecordModelInput(ctx, []chat.Message{{Role: "user", Content: "question"}}, nil)
	Finish(ctx, "answer", nil)
	sink := &memorySink{rec: &memoryRecorder{}}
	ctx = Begin(context.Background(), sink, Start{})
	if FromContext(ctx) != nil {
		t.Fatal("disabled sink must leave no recorder")
	}
	RecordSources(ctx, []Source{{Content: "disabled evidence"}})
	Finish(ctx, "answer", nil)
	if len(sink.starts)+len(sink.rec.sources)+len(sink.rec.completions) != 0 {
		t.Fatal("disabled capture retained data")
	}
}
func TestFinishPreservesCancellationAndFailureAndRunsOnce(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status string
	}{{nil, "completed"}, {context.Canceled, "cancelled"}, {errors.New("model failed"), "failed"}} {
		r := &memoryRecorder{}
		ctx := Begin(context.Background(), &memorySink{enabled: true, rec: r}, Start{StartedAt: time.Now()})
		Finish(ctx, "answer", tc.err)
		Finish(ctx, "duplicate", nil)
		if len(r.completions) != 1 || r.completions[0].Status != tc.status {
			t.Fatalf("got %+v", r.completions)
		}
	}
}
func TestCapturePanicsCannotFailBusinessCall(t *testing.T) {
	ctx := Begin(context.Background(), &memorySink{panicStart: true}, Start{})
	Finish(ctx, "answer", nil)
	r := &memoryRecorder{panicInput: true}
	ctx = Begin(context.Background(), &memorySink{enabled: true, rec: r}, Start{})
	RecordModelInput(ctx, []chat.Message{{Role: "user", Content: "question"}}, nil)
	Finish(ctx, "answer", nil)
	if len(r.completions) != 1 || len(r.gaps) == 0 {
		t.Fatalf("capture failure not disclosed: %+v", r)
	}
}
func TestActualInputExcludesSystemReasoningImagesAndSecrets(t *testing.T) {
	r := &memoryRecorder{}
	ctx := Begin(context.Background(), &memorySink{enabled: true, rec: r}, Start{})
	RecordModelInput(ctx, []chat.Message{
		{Role: "system", Content: "PRIVATE SYSTEM PROMPT\n<retrieval><chunk id=\"c1\"><content>encoded visible evidence</content></chunk></retrieval>"},
		{Role: "user", Content: "question api_key=secret-value data:image/png;base64,aGVsbG8=", Images: []string{"private image"}},
		{Role: "assistant", Content: "visible answer", ReasoningContent: "hidden thinking"},
		{Role: "tool", Name: "sequentialthinking", Content: "private reasoning"},
		{Role: "user", MultiContent: []chat.MessageContentPart{{Type: "text", Text: "visible multipart"}, {Type: "image_url", ImageURL: &chat.ImageURL{URL: "data:image/png;base64,aGVsbG8="}}}},
	}, nil)
	if len(r.inputs) != 1 {
		t.Fatal("input not captured")
	}
	all := ""
	for _, m := range r.inputs[0].Messages {
		all += m.Content
	}
	for _, secret := range []string{"PRIVATE SYSTEM PROMPT", "hidden thinking", "private reasoning", "secret-value", "aGVsbG8=", "private image"} {
		if strings.Contains(all, secret) {
			t.Fatalf("retained %q: %s", secret, all)
		}
	}
	if !strings.Contains(all, "encoded visible evidence") || !strings.Contains(all, "visible multipart") {
		t.Fatalf("lost actual visible evidence: %s", all)
	}
	if len(r.gaps) == 0 {
		t.Fatal("omissions must be disclosed")
	}
}
func TestRawCandidateIsNotModelVisibleUntilMatchedAfterEncoding(t *testing.T) {
	r := &memoryRecorder{}
	ctx := Begin(context.Background(), &memorySink{enabled: true, rec: r}, Start{})
	RecordSources(ctx, []Source{{ID: "chunk-real", Kind: "chunk", KBID: "kb", ChunkID: "chunk-real", Content: "raw full candidate"}, {ID: "not-read", Kind: "chunk", ChunkID: "not-read", Content: "unread"}})
	RecordModelInput(ctx, []chat.Message{{Role: "tool", Name: "search_knowledge", ToolCallID: "call", Content: `<retrieval><document id="d1"><chunk id="c1" view="match"><match>only actual snippet</match></chunk></document></retrieval>`}}, func(s string) string {
		if s == "chunk-real" {
			return "c1"
		}
		return s
	})
	var sent []Source
	for _, s := range r.sources {
		if s.SentToModel {
			sent = append(sent, s)
		}
	}
	if len(sent) != 1 || !strings.Contains(sent[0].Content, "only actual snippet") || strings.Contains(sent[0].Content, "raw full candidate") || !sent[0].Truncated {
		t.Fatalf("wrong visible evidence: %+v", sent)
	}
	if sent[0].Version != "unknown" || sent[0].KBID != "kb" {
		t.Fatalf("lost honest provenance: %+v", sent[0])
	}
}

func TestUnmatchedModelEvidenceDisclosesProvenanceGap(t *testing.T) {
	r := &memoryRecorder{}
	ctx := WithRecorder(context.Background(), r)
	RecordModelInput(ctx, []chat.Message{{Role: "tool", Name: "wiki_read_page", ToolCallID: "call", Content: "<wiki_page><content>unmapped source</content></wiki_page>"}}, nil)
	if len(r.gaps) == 0 {
		t.Fatal("unmapped model-visible evidence must be partial")
	}
}

func TestQuestionAndAnswerAreSanitizedAndMarkedPartial(t *testing.T) {
	r := &memoryRecorder{}
	s := &memorySink{enabled: true, rec: r}
	ctx := Begin(context.Background(), s, Start{Question: "api_key=do-not-retain"})
	if strings.Contains(s.starts[0].Question, "do-not-retain") || len(r.gaps) == 0 {
		t.Fatal("question sanitization must be disclosed")
	}
	Finish(ctx, "<think>hidden chain</think>answer Bearer secret-token", nil)
	if len(r.completions) != 1 || strings.Contains(r.completions[0].Answer, "hidden chain") || strings.Contains(r.completions[0].Answer, "secret-token") {
		t.Fatalf("unsafe answer: %+v", r.completions)
	}
}
func TestMultipartPrecedenceAndSystemEvidenceOnly(t *testing.T) {
	r := &memoryRecorder{}
	ctx := WithRecorder(context.Background(), r)
	RecordModelInput(ctx, []chat.Message{
		{Role: "system", MultiContent: []chat.MessageContentPart{{Type: "text", Text: "private system multipart <retrieval><chunk id=\"c1\">real context</chunk></retrieval>"}}},
		{Role: "user", Content: "unused plain content", MultiContent: []chat.MessageContentPart{{Type: "text", Text: "actual multipart user"}}},
	}, nil)
	all := ""
	for _, m := range r.inputs[0].Messages {
		all += m.Content
	}
	if strings.Contains(all, "private system multipart") || strings.Contains(all, "unused plain content") {
		t.Fatalf("captured non-model content: %s", all)
	}
	if !strings.Contains(all, "real context") || !strings.Contains(all, "actual multipart user") {
		t.Fatalf("lost actual content: %s", all)
	}
}

func TestDocumentMetadataRemainsBoundWithoutSiblingBodies(t *testing.T) {
	r := &memoryRecorder{}
	ctx := WithRecorder(context.Background(), r)
	RecordSources(ctx, []Source{{Kind: "chunk", ChunkID: "c1", KBID: "kb1"}, {Kind: "chunk", ChunkID: "c2", KBID: "kb2"}})
	RecordModelInput(ctx, []chat.Message{{Role: "user", Content: `<retrieval><document id="d1"><metadata>Beijing only</metadata><chunk id="c1">first body</chunk></document><document id="d2"><metadata>Shanghai only</metadata><chunk id="c2">second body</chunk></document></retrieval>`}}, nil)
	sent := 0
	for _, s := range r.sources {
		if !s.SentToModel {
			continue
		}
		sent++
		if s.ChunkID == "c1" && (!strings.Contains(s.Content, "Beijing only") || strings.Contains(s.Content, "Shanghai") || strings.Contains(s.Content, "second body")) {
			t.Fatalf("wrong document binding: %+v", s)
		}
		if s.ChunkID == "c2" && (!strings.Contains(s.Content, "Shanghai only") || strings.Contains(s.Content, "Beijing") || strings.Contains(s.Content, "first body")) {
			t.Fatalf("wrong document binding: %+v", s)
		}
	}
	if sent != 2 {
		t.Fatalf("captured %d visible sources, want 2", sent)
	}
}
