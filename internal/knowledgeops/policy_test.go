package knowledgeops

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/knowledgeops/capture"
	"github.com/stretchr/testify/require"
)

func TestUnresolvedIssuesStaySeparateInGroupView(t *testing.T) {
	for _, pending := range []string{"review_question", "unresolved_cause"} {
		t.Run(pending, func(t *testing.T) {
			_, repo, engine := workbenchFixture(t)
			report := Report{Risk: "low", Verdict: "partial", Claims: []Claim{{Verdict: "partial"}}, Scope: "same product", SourceCheck: "consistent", Causes: []Cause{{Category: "generation", Summary: "missing condition", Status: "suspected"}}}
			evidence := []Evidence{{KBID: "kb", KnowledgeID: "doc", Version: "v1", Hash: "abc", SentToModel: true}}
			run := Run{TenantID: 1, ActorID: "u"}
			require.NotEmpty(t, groupKey(run, report, evidence), "resolved scope can group suspected causes for review")
			if pending == "review_question" {
				report.ReviewQuestions = []string{"Which product does this apply to?"}
			} else {
				report.Causes[0].Status = "unresolved"
			}
			key := groupKey(run, report, evidence)
			for _, id := range []string{"first", "second"} {
				seedWorkbench(t, repo, id, "low", "needs_evidence", key, 0)
			}
			page := workbenchGET(t, engine, "/groups")
			require.EqualValues(t, 2, page["total"], "unresolved cases must remain visible as separate groups")
			for _, item := range page["items"].([]any) {
				group := item.(map[string]any)
				require.EqualValues(t, 1, group["count"])
				require.Contains(t, group["key"], "run:")
			}
		})
	}
}

func TestExpandedCaptureBudgetPreservesLongEvidenceAndStillBoundsStorage(t *testing.T) {
	for _, tc := range []struct {
		name       string
		chars      int
		wantStatus string
		wantStored int
	}{
		{"long_evidence", 60000, "complete", 60000},
		{"over_budget", 100000, "partial", 96000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo := enabledTestService(t)
			ctx := context.Background()
			rec := svc.Start(ctx, capture.Start{TenantID: 1, MessageID: tc.name, StartedAt: time.Now()})
			require.NotNil(t, rec)
			rec.RecordInput(ctx, capture.Input{})
			rec.RecordSources(ctx, []capture.Source{{Kind: "chunk", KBID: "kb", ChunkID: "chunk", Version: "v1", SentToModel: true, Content: strings.Repeat("证", tc.chars)}})
			rec.Finish(ctx, capture.Completion{Answer: "answer", Status: "completed", FinishedAt: time.Now()})
			var run Run
			require.NoError(t, repo.db.First(&run).Error)
			require.Equal(t, tc.wantStatus, run.CaptureStatus)
			evidence, err := repo.Evidence(ctx, 1, run.ID)
			require.NoError(t, err)
			require.Len(t, evidence, 1)
			require.Equal(t, tc.wantStored, len([]rune(evidence[0].Content)))
			if tc.wantStatus == "partial" {
				require.Contains(t, run.Gaps, "capture_budget_exhausted")
				require.True(t, evidence[0].Truncated)
			} else {
				require.Empty(t, run.Gaps)
			}
		})
	}
}
