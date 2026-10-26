package knowledgeops

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/knowledgeops/capture"
	"github.com/Tencent/WeKnora/internal/modelcontext"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestCapturePreservesDocumentApplicabilityMetadata(t *testing.T) {
	svc, repo := enabledTestService(t)
	ctx := capture.Begin(context.Background(), svc, capture.Start{TenantID: 1, MessageID: "metadata", StartedAt: time.Now()})
	result := &types.ToolResult{Success: true, Data: map[string]any{"display_type": "search_results", "results": []map[string]any{{"chunk_id": "chunk-real", "knowledge_id": "doc-real", "knowledge_base_id": "kb", "knowledge_title": "Policy", "knowledge_metadata": "Only applies to Beijing after 2027", "content": "Benefit: 100", "version": "1"}}}}
	capture.RecordToolResult(ctx, "call", "search_knowledge", result)
	registry := modelcontext.NewRegistry(true)
	message := registry.ModelToolResultForTool("search_knowledge", result)
	require.Contains(t, message, "Only applies to Beijing after 2027")
	capture.RecordModelInput(ctx, []chat.Message{{Role: "tool", Name: "search_knowledge", ToolCallID: "call", Content: message}}, registry.CompactKnownText, "model")
	capture.Finish(ctx, "Benefit: 100", nil)
	var run Run
	require.NoError(t, repo.db.First(&run).Error)
	ev, err := repo.Evidence(ctx, 1, run.ID)
	require.NoError(t, err)
	all := ""
	for _, e := range ev {
		all += e.Content
	}
	require.Contains(t, all, "Only applies to Beijing after 2027")
	require.Equal(t, "complete", run.CaptureStatus)
	require.Empty(t, run.Gaps)
}

func TestCaptureMergedChildRevocation(t *testing.T) {
	svc, repo := enabledTestService(t)
	for _, sql := range []string{
		`CREATE TABLE knowledge_bases(id TEXT PRIMARY KEY, tenant_id BIGINT, deleted_at TIMESTAMP)`,
		`CREATE TABLE knowledges(id TEXT PRIMARY KEY, knowledge_base_id TEXT, deleted_at TIMESTAMP)`,
		`CREATE TABLE chunks(id TEXT PRIMARY KEY, knowledge_base_id TEXT, is_enabled BOOLEAN, deleted_at TIMESTAMP)`,
		`INSERT INTO knowledge_bases(id,tenant_id) VALUES('kb',1)`,
		`INSERT INTO knowledges(id,knowledge_base_id) VALUES('doc','kb')`,
		`INSERT INTO chunks(id,knowledge_base_id,is_enabled) VALUES('c1','kb',true),('c2','kb',false)`,
	} {
		require.NoError(t, repo.db.Exec(sql).Error)
	}
	adapter := &RuntimeAdapter{DB: repo.db, Sessions: sessionReadStub{session: &types.Session{ID: "s", TenantID: 1, UserID: "owner"}}}
	svc.access = adapter.Check
	ctx := types.WithCaller(context.Background(), types.Caller{TenantID: 1, UserID: "owner", Role: types.TenantRoleAdmin})
	ctx = capture.Begin(ctx, svc, capture.Start{TenantID: 1, SessionID: "s", MessageID: "merged", StartedAt: time.Now()})
	// This is the result shape produced by mergeSequentialChunks/recordMergedChild.
	merged := &types.SearchResult{ID: "c1", KnowledgeID: "doc", KnowledgeBaseID: "kb", SubChunkID: []string{"c2"}, Content: "public body; secret from c2", Metadata: map[string]string{"version": "1"}}
	capture.RecordSources(ctx, capture.SearchSources([]*types.SearchResult{merged}, ""))
	capture.RecordModelInput(ctx, []chat.Message{{Role: "user", Content: `<retrieval><chunk id="c1">public body; secret from c2</chunk></retrieval>`}}, nil, "model")
	capture.Finish(ctx, "secret from c2", nil)
	var run Run
	require.NoError(t, repo.db.First(&run).Error)
	_, err := svc.Detail(ctx, 1, run.ID)
	require.ErrorIs(t, err, ErrForbidden)
	require.NoError(t, repo.db.Exec(`UPDATE chunks SET is_enabled=true WHERE id='c2'`).Error)
	detail, err := svc.Detail(ctx, 1, run.ID)
	require.NoError(t, err)
	require.Equal(t, "secret from c2", detail.Run.Answer)
	require.NoError(t, repo.db.Exec(`DELETE FROM chunks WHERE id='c2'`).Error)
	_, err = svc.Detail(ctx, 1, run.ID)
	require.ErrorIs(t, err, ErrForbidden)
}

func TestIncompleteCaptureCannotClaimUnsupported(t *testing.T) {
	for _, status := range []string{"missing", "partial", "unknown", "complete"} {
		t.Run(status, func(t *testing.T) {
			m := &fakeChat{payload: `{"verdict":"unsupported","claims":[{"text":"A","position":"answer","verdict":"unsupported","evidence_ids":[],"reason":"no supporting evidence"}],"source_check":"not_checked","risk":"low","scope":"captured"}`}
			a := NewAnalyzer(func(context.Context, string) (chat.Chat, error) { return m, nil }, nil)
			out, err := a.Analyze(context.Background(), Run{Status: "completed", CaptureStatus: status, Answer: "A"}, nil, DefaultSettings(1))
			require.NoError(t, err)
			expected := "unverifiable"
			if status == "complete" {
				expected = "unsupported"
			}
			require.Equal(t, expected, out.Report.Verdict)
			require.Equal(t, expected, out.Report.Claims[0].Verdict)
		})
	}
}

func TestDocumentMetadataUsesCaptureBudget(t *testing.T) {
	svc, repo := enabledTestService(t)
	settings, err := repo.Settings(context.Background(), 1)
	require.NoError(t, err)
	settings.MaxEvidenceChars = 48000 // Explicit tenant limits survive default changes.
	require.NoError(t, repo.SaveSettings(context.Background(), 1, settings))
	ctx := capture.Begin(context.Background(), svc, capture.Start{TenantID: 1, MessageID: "large-metadata", StartedAt: time.Now()})
	capture.RecordSources(ctx, []capture.Source{{Kind: "chunk", ChunkID: "c1", KBID: "kb", Version: "1"}})
	capture.RecordModelInput(ctx, []chat.Message{{Role: "user", Content: `<retrieval><document><metadata>` + strings.Repeat("condition ", 6000) + `</metadata><chunk id="c1">body</chunk></document></retrieval>`}}, nil, "model")
	capture.Finish(ctx, "answer", nil)
	var run Run
	require.NoError(t, repo.db.First(&run).Error)
	require.Equal(t, "partial", run.CaptureStatus)
	require.Contains(t, run.Gaps, "capture_budget_exhausted")
	rows, err := repo.Evidence(ctx, 1, run.ID)
	require.NoError(t, err)
	total := 0
	for _, e := range rows {
		total += len([]rune(e.Content))
	}
	require.LessOrEqual(t, total, 48000)
}

func TestAnalysisDisclosesConfigurationLimitWithoutBlockingEvidenceSupport(t *testing.T) {
	m := &fakeChat{payload: `{"verdict":"supported","claims":[{"text":"A","position":"answer","verdict":"supported","evidence_ids":["e"],"reason":"observed"}],"source_check":"not_checked","risk":"low","scope":"source support"}`}
	settings := DefaultSettings(1)
	settings.AutoRecordEnabled = true
	settings.CalibrationNote = "validated"
	settings.SamplePercent = 0
	settings = withTestCalibration(settings)
	a := NewAnalyzer(func(context.Context, string) (chat.Chat, error) {
		return &diagnosticModel{diagnosticChat: m, fingerprint: settings.calibrationModelFingerprint}, nil
	}, nil)
	out, err := a.Analyze(context.Background(), Run{Status: "completed", CaptureStatus: "complete", QAType: "rag-qa", Channel: "web"}, []Evidence{{ID: "e", Content: "A", Kind: "chunk", SentToModel: true}}, settings)
	require.NoError(t, err)
	require.Equal(t, "auto_record", out.Route)
	require.Contains(t, out.Report.Scope, "配置相关归因未核验")
}
