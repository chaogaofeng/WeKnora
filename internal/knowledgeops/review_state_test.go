package knowledgeops

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestReviewAPIsSeparateHumanCompletionFromSourceFollowup(t *testing.T) {
	_, repo, engine := workbenchFixture(t)
	seedWorkbench(t, repo, "reviewed", "low", "needs_evidence", "", 0)
	require.NoError(t, repo.db.Model(&Run{}).Where("id = ?", "reviewed").Update("gaps", `["wiki_linked_summary_versions_unknown"]`).Error)
	require.NoError(t, repo.db.Model(&Analysis{}).Where("id = ?", "reviewed-a").Update("report", `{"verdict":"supported","source_check":"unresolved","causes":[]}`).Error)
	for i, id := range []string{"first-review", "second-review"} {
		require.NoError(t, repo.db.Create(&Review{ID: id, TenantID: 1, RunID: "reviewed", AnalysisID: "reviewed-a", Target: "answer", Action: "confirmed", Verdict: "supported", Reason: "没问题", ReviewerID: "u", CreatedAt: time.Now().Add(time.Duration(i) * time.Second)}).Error)
	}
	list := workbenchGET(t, engine, "/runs")
	run := list["items"].([]any)[0].(map[string]any)
	require.Equal(t, "reviewed", run["review_state"])
	require.Equal(t, true, run["source_followup_pending"])
	detail := workbenchGET(t, engine, "/runs/reviewed")
	run = detail["run"].(map[string]any)
	require.Equal(t, "reviewed", run["review_state"])
	require.Equal(t, true, run["source_followup_pending"])
	groups := workbenchGET(t, engine, "/groups")
	require.EqualValues(t, 1, groups["total"])
	representative := groups["items"].([]any)[0].(map[string]any)["representative"].(map[string]any)
	require.Equal(t, "reviewed", representative["review_state"])
	require.Equal(t, true, representative["source_followup_pending"])
	stats := workbenchGET(t, engine, "/overview")
	require.EqualValues(t, 1, stats["pending_runs"], "source work remains actionable after human review")
	require.Equal(t, map[string]any{"source_verification": float64(1)}, stats["queue_counts"])
	var stored Analysis
	require.NoError(t, repo.db.First(&stored, "id = ?", "reviewed-a").Error)
	require.Equal(t, "unresolved", stored.Report.SourceCheck)
	require.Equal(t, "needs_evidence", stored.Route, "historical diagnosis is not rewritten")
}
