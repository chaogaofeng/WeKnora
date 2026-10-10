package knowledgeops

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/knowledgeops/capture"
	"github.com/Tencent/WeKnora/internal/modelcontext"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestNewAnswersRetainActualSourceRevisionsAcrossModes(t *testing.T) {
	for _, mode := range []string{"rag-qa", "wiki-qa", "hybrid-rag-wiki"} {
		t.Run(mode, func(t *testing.T) {
			svc, repo := enabledTestService(t)
			ctx := capture.Begin(context.Background(), svc, capture.Start{TenantID: 1, SessionID: "s", MessageID: mode, QAType: mode})
			registry := modelcontext.NewRegistry(true)
			var messages []chat.Message
			if mode != "wiki-qa" {
				capture.RecordSources(ctx, capture.SearchSources([]*types.SearchResult{{ID: "chunk", KnowledgeBaseID: "kb", KnowledgeID: "doc", SourceRevisions: map[string]int{"chunk": 0}}}, ""))
				messages = append(messages, chat.Message{Role: "user", Content: `<retrieval><chunk id="chunk">RAG 正文</chunk></retrieval>`})
			}
			if mode != "rag-qa" {
				inline := `[[neighbor]] (关联页的摘要 <web url="https://example.com/source"/> <kb chunk_id="cited-chunk"/>)`
				result := &types.ToolResult{Success: true,
					Output: `<wiki_page><knowledge_base_id>kb</knowledge_base_id><link>[[main|主页面]]</link><content>主页面正文</content><links_to>` + inline + `</links_to></wiki_page>`,
					SourceSnapshots: []types.ToolSourceSnapshot{
						{Kind: "wiki", KBID: "kb", PageID: "main-id", Slug: "main", Version: "2"},
						{Kind: "wiki_summary", KBID: "kb", PageID: "neighbor-id", Slug: "neighbor", Version: "7", Title: "关联页", ParentPageID: "main-id", Rendered: inline, Truncated: true},
					},
				}
				capture.RecordToolResult(ctx, "wiki-call", "wiki_read_page", result)
				messages = append(messages, chat.Message{Role: "tool", Name: "wiki_read_page", ToolCallID: "wiki-call", Content: registry.ModelToolResultForTool("wiki_read_page", result)})
			}
			messages = registry.EncodeMessages(messages)
			capture.RecordModelInput(ctx, messages, registry.CompactEvidenceText, "answer-model")
			capture.Finish(ctx, "answer", nil)
			var run Run
			require.NoError(t, repo.db.First(&run).Error)
			d, err := svc.Detail(context.Background(), 1, run.ID)
			require.NoError(t, err)
			require.Empty(t, d.Run.Gaps)
			require.Equal(t, "complete", d.Run.Coverage.ContentStatus)
			require.Equal(t, "known", d.Run.Coverage.ProvenanceStatus)
			var summaries []Evidence
			for _, e := range d.Evidence {
				if e.SentToModel {
					require.NotEqual(t, "unknown", e.Version)
					if e.Kind == "wiki_summary" {
						summaries = append(summaries, e)
					}
				}
			}
			if mode == "rag-qa" {
				require.Empty(t, summaries)
				return
			}
			require.Len(t, summaries, 1)
			require.Equal(t, "7", summaries[0].Version)
			require.Equal(t, "neighbor-id", summaries[0].PageID)
			require.Contains(t, summaries[0].Content, "关联页的摘要")
			require.Contains(t, summaries[0].Content, `<ref id="w1"/>`)
			require.Contains(t, summaries[0].Content, `<ref id="c1"/>`)
			require.NotContains(t, summaries[0].Content, "主页面正文")
			if mode != "wiki-qa" {
				return
			}
			// A consumed summary is an independent permission dependency, even
			// though it arrived inline with another page's body.
			for _, sql := range []string{
				`CREATE TABLE knowledge_bases (id TEXT PRIMARY KEY, tenant_id BIGINT, deleted_at TIMESTAMP)`,
				`CREATE TABLE wiki_pages (id TEXT PRIMARY KEY, knowledge_base_id TEXT, tenant_id BIGINT, status TEXT, deleted_at TIMESTAMP)`,
				`INSERT INTO knowledge_bases (id,tenant_id) VALUES ('kb',1)`,
				`INSERT INTO wiki_pages VALUES ('main-id','kb',1,'published',NULL),('neighbor-id','kb',1,'published',NULL)`,
			} {
				require.NoError(t, repo.db.Exec(sql).Error)
			}
			adapter := RuntimeAdapter{DB: repo.db, Sessions: sessionReadStub{session: &types.Session{ID: "s", TenantID: 1, UserID: "owner"}}}
			readCtx := types.WithCaller(context.Background(), types.Caller{TenantID: 1, UserID: "owner", Role: types.TenantRoleAdmin})
			require.NoError(t, adapter.Check(readCtx, d.Run, d.Evidence))
			require.NoError(t, repo.db.Exec(`DELETE FROM wiki_pages WHERE id='neighbor-id'`).Error)
			require.ErrorIs(t, adapter.Check(readCtx, d.Run, d.Evidence), ErrForbidden)
		})
	}
}
