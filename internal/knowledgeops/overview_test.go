package knowledgeops

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOverviewQualityCountsOnlyLatestSuccessfulDiagnoses(t *testing.T) {
	_, repo, engine := workbenchFixture(t)
	for _, id := range []string{"supported", "unverifiable", "failed", "private"} {
		seedWorkbench(t, repo, id, "low", "human_review", "", 0)
	}
	// A previous success must not survive a failed latest diagnosis in quality metrics.
	require.NoError(t, repo.db.Create(&Analysis{ID: "failed-new", TenantID: 1, RunID: "failed", Version: 2, Status: "failed", Route: "human_review", Report: Report{Verdict: "unverifiable", SourceCheck: "unresolved", Risk: "high", Causes: []Cause{{Category: "capture"}}}}).Error)
	for id, report := range map[string]Report{
		"supported":    {Verdict: "supported", SourceCheck: "unresolved", Risk: "low", Causes: []Cause{{Category: "retrieval"}, {Category: "retrieval"}}},
		"unverifiable": {Verdict: "unverifiable", SourceCheck: "not_checked", Risk: "low"},
	} {
		body, err := json.Marshal(report)
		require.NoError(t, err)
		require.NoError(t, repo.db.Model(&Analysis{}).Where("id = ?", id+"-a").Update("report", string(body)).Error)
	}
	require.NoError(t, repo.db.Create(&Run{ID: "pending", TenantID: 1, MessageID: "pending", QAType: "wiki-qa", Gaps: []string{}, StartedAt: time.Now()}).Error)
	v := workbenchGET(t, engine, "/overview")
	require.EqualValues(t, 4, v["total"])
	require.Equal(t, map[string]any{"succeeded": float64(2), "failed": float64(1), "pending": float64(1)}, v["diagnosis_status"])
	require.Equal(t, map[string]any{"supported": float64(1), "unverifiable": float64(1)}, v["by_verdict"], "execution failure is not an answer quality judgment")
	require.Equal(t, map[string]any{"unresolved": float64(1), "not_checked": float64(1)}, v["source_checks"], "failed placeholder cannot imply an unresolved source finding")
	require.Equal(t, map[string]any{"retrieval": float64(1)}, v["causes"], "count affected runs once per category, only after a successful diagnosis")
}

func TestOverviewPendingRunsDeduplicatesQueuesAndHonorsVisibility(t *testing.T) {
	_, repo, engine := workbenchFixture(t)
	for _, id := range []string{"broken", "done", "private"} {
		seedWorkbench(t, repo, id, "high", "human_review", "", 0)
	}
	require.NoError(t, repo.db.Model(&Run{}).Where("id = ?", "broken").Updates(map[string]any{"status": "failed", "gaps": `["source_version_unknown"]`}).Error)
	require.NoError(t, repo.db.Create(&Review{ID: "done-r", TenantID: 1, RunID: "done", AnalysisID: "done-a", Target: "answer", Action: "confirmed", CreatedAt: time.Now()}).Error)
	v := workbenchGET(t, engine, "/overview")
	require.EqualValues(t, 2, v["total"])
	require.EqualValues(t, 1, v["pending_runs"], "one run occupies business, source, and technical queues but is counted once")
	require.Equal(t, map[string]any{"human_review": float64(1), "source_verification": float64(1), "technical_followup": float64(1)}, v["queue_counts"])
	filtered := workbenchGET(t, engine, "/overview?search=Question%20done")
	require.EqualValues(t, 0, filtered["pending_runs"])
}

func TestOverviewSourceComparisonApplicabilityFollowsActualWikiUse(t *testing.T) {
	for _, tc := range []struct {
		name, mode, check, want string
		evidence                []Evidence
	}{
		{"RAG without Wiki", "rag-qa", "not_checked", "not_applicable", nil},
		{"Wiki required", "wiki-qa", "not_checked", "not_checked", nil},
		{"hybrid required", "hybrid-rag-wiki", "not_checked", "not_checked", nil},
		{"actual Wiki use", "rag-qa", "not_checked", "not_checked", []Evidence{{PageID: "page", SentToModel: true}}},
		{"unread Wiki candidate", "rag-qa", "not_checked", "not_applicable", []Evidence{{PageID: "page"}}},
		{"RAG limitation preserved", "rag-qa", "unresolved", "unresolved", nil},
		{"unknown mode", "unknown", "not_checked", "not_checked", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stats := newWorkbenchStats()
			stats.observe(Detail{Run: Run{QAType: tc.mode, Analysis: &Analysis{Status: "succeeded", Report: Report{SourceCheck: tc.check}}}, Evidence: tc.evidence})
			require.Equal(t, map[string]int{tc.want: 1}, stats.SourceChecks)
		})
	}
}
