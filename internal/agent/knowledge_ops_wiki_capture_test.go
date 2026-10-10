package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	agenttoken "github.com/Tencent/WeKnora/internal/agent/token"
	agenttools "github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/knowledgeops/capture"
	"github.com/Tencent/WeKnora/internal/modelcontext"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type captureWikiService struct {
	interfaces.WikiPageService
	pages map[string]*types.WikiPage
}

func (s *captureWikiService) GetPageBySlug(_ context.Context, kb, slug string) (*types.WikiPage, error) {
	page := s.pages[slug]
	if page != nil && page.KnowledgeBaseID == kb {
		return page, nil
	}
	return nil, nil
}

type wikiCaptureRecorder struct {
	sources []capture.Source
	gaps    []string
}

func (r *wikiCaptureRecorder) RecordInput(context.Context, capture.Input) {}
func (r *wikiCaptureRecorder) RecordSources(_ context.Context, sources []capture.Source) {
	r.sources = append(r.sources, sources...)
}
func (r *wikiCaptureRecorder) Gap(_ context.Context, code string)         { r.gaps = append(r.gaps, code) }
func (r *wikiCaptureRecorder) Finish(context.Context, capture.Completion) {}

func TestWikiCaptureDoesNotPromoteNestedSummaryAfterToolCompaction(t *testing.T) {
	for _, prefix := range []string{"右括号 ), ", "), ", "summary omitted, already seen), "} {
		t.Run(prefix, func(t *testing.T) { testCompactedWikiSummary(t, prefix) })
	}
}

func testCompactedWikiSummary(t *testing.T, prefix string) {
	t.Helper()
	main := &types.WikiPage{ID: "main-id", KnowledgeBaseID: "real-kb", Slug: "main", Title: "Main", Version: 1, Content: "BODY"}
	pages := map[string]*types.WikiPage{"main": main}
	for i := 0; i < 20; i++ {
		slug, summary := fmt.Sprintf("n%02d", i), strings.Repeat("关联页面内容。", 22)
		if i == 0 {
			slug, summary = "b", prefix+"[[a]] (A), "+strings.Repeat("尾部内容。", 26)
		} else if i == 1 {
			slug, summary = "a", "A"
		}
		main.OutLinks = append(main.OutLinks, slug)
		pages[slug] = &types.WikiPage{ID: slug + "-id", KnowledgeBaseID: main.KnowledgeBaseID, Slug: slug, Title: slug, Version: i + 1, Summary: summary}
	}
	tool := agenttools.NewWikiReadPageTool(&captureWikiService{pages: pages}, nil, agenttools.NewWikiScopesFromKBIDs([]string{main.KnowledgeBaseID}), nil)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"slugs":["main"]}`))
	require.NoError(t, err)
	require.True(t, result.Success)
	registry := modelcontext.NewRegistry(true)
	full := chat.Message{Role: "tool", Name: "wiki_read_page", ToolCallID: "call", Content: registry.ModelToolResultForTool("wiki_read_page", result)}
	estimator, err := agenttoken.NewEstimator()
	require.NoError(t, err)
	// Find a real head/tail preview that retains the nested mention but drops
	// the independent A entry. Do not tie the test to a tokenizer's exact count.
	var compacted chat.Message
	for budget := 180; budget < 2000; budget++ {
		preview := compactToolMessage(full, budget, estimator)
		if strings.Contains(preview.Content, "[tool result preview omitted]") &&
			strings.Count(preview.Content, "[[a]] (A)") == 1 &&
			strings.Contains(preview.Content, "[[b]] ("+prefix+"[[a]] (A),") {
			compacted = preview
			break
		}
	}
	require.NotEmpty(t, compacted.Content, "fixture must exercise actual head/tail compaction")
	for _, tc := range []struct {
		name    string
		message chat.Message
		wantA   int
	}{
		{"complete tool output", full, 1},
		{"compacted tool output", compacted, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := &wikiCaptureRecorder{}
			ctx := capture.WithRecorder(context.Background(), rec)
			capture.RecordToolResult(ctx, "call", "wiki_read_page", result)
			capture.RecordModelInput(ctx, registry.EncodeMessages([]chat.Message{tc.message}), registry.CompactEvidenceText)
			count := 0
			for _, source := range rec.sources {
				if source.Kind == "wiki_summary" && source.PageID == "a-id" && source.SentToModel {
					count++
				}
			}
			require.Equal(t, tc.wantA, count, "nested mentions must not inherit a cropped candidate's version")
			if tc.wantA == 0 {
				require.Contains(t, rec.gaps, "wiki_linked_summary_versions_unknown")
			} else {
				require.Empty(t, rec.gaps)
			}
		})
	}
}
