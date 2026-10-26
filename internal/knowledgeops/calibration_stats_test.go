package knowledgeops

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWorkbenchCountsOnlyCurrentHumanAnswerOutcomes(t *testing.T) {
	for _, tc := range []struct{ action, verdict, outcome string }{{"accepted", "usable", "usable"}, {"revision_required", "revision_required", "revision_required"}, {"excluded", "excluded", "excluded"}, {"needs_more_evidence", "unverifiable", "needs_evidence"}, {"corrected", "partial", ""}} {
		t.Run(tc.action, func(t *testing.T) {
			d := Detail{Run: Run{Status: "completed", QAType: "rag-qa", CaptureStatus: "complete", Analysis: &Analysis{ID: "current", Status: "succeeded", Route: "quality_sample", Report: Report{Verdict: "supported", Risk: "low", SourceCheck: "consistent"}}}, Reviews: []Review{
				{AnalysisID: "old", Target: "answer", Action: "revision_required", Verdict: "revision_required", CreatedAt: time.Now()},
				{AnalysisID: "current", Target: "cause", Action: "corrected", Verdict: "partial"},
				{AnalysisID: "current", Target: "answer", Action: tc.action, Verdict: tc.verdict},
				{AnalysisID: "current", Target: "answer", Action: "accepted", Verdict: "usable"},
			}}
			stats := newWorkbenchStats()
			stats.observe(d)
			require.Equal(t, 1, stats.SampleTotal)
			raw, err := json.Marshal(stats)
			require.NoError(t, err)
			var values map[string]any
			require.NoError(t, json.Unmarshal(raw, &values))
			if tc.outcome != "" {
				require.Equal(t, map[string]any{tc.outcome: float64(1)}, values["answer_outcomes"])
			} else {
				require.Empty(t, values["answer_outcomes"])
			}
			if tc.action == "revision_required" {
				require.Equal(t, float64(1), values["sample_revision_required"])
			} else {
				require.Equal(t, float64(0), values["sample_revision_required"])
			}
			if tc.action == "corrected" {
				require.Equal(t, 1, stats.SampleCorrected)
			} else {
				require.Zero(t, stats.SampleCorrected)
			}
		})
	}
}

func TestWorkbenchHumanOutcomesDoNotRequireSuccessfulMachineDiagnosis(t *testing.T) {
	d := Detail{Run: Run{Analysis: &Analysis{ID: "current", Status: "failed"}}, Reviews: []Review{{AnalysisID: "current", Target: "answer", Action: "accepted", Verdict: "usable"}}}
	stats := newWorkbenchStats()
	stats.observe(d)
	require.Equal(t, map[string]int{"usable": 1}, stats.AnswerOutcomes)
	require.Zero(t, stats.SampleTotal)
}
