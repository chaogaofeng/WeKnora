package knowledgeops

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReviewReasonOptionalOnlyForConfirmation(t *testing.T) {
	for _, tc := range []struct {
		name, target, action, verdict, reason string
		valid                                 bool
	}{
		{"confirm answer", "answer", "confirmed", "partial", "", true},
		{"confirm cause", "cause", "confirmed", "suspected", " \n\t ", false},
		{"optional note", "answer", "confirmed", "partial", " checked evidence ", true},
		{"cannot silently correct", "answer", "confirmed", "supported", "", false},
		{"correct answer", "answer", "corrected", "supported", "", false},
		{"correct cause", "cause", "corrected", "evidence_backed", " \n\t ", false},
		{"request answer evidence", "answer", "needs_more_evidence", "unverifiable", "", false},
		{"request cause evidence", "cause", "needs_more_evidence", "unresolved", " \n ", false},
		{"explained correction", "answer", "corrected", "supported", " checked the attachment ", true},
		{"explained evidence request", "answer", "needs_more_evidence", "unverifiable", " provide the original ", true},
		{"optional note still bounded", "answer", "confirmed", "partial", strings.Repeat("证", 1334), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, repo, _ := workbenchFixture(t)
			seedWorkbench(t, repo, "case", "low", "human_review", "", 0)
			saveWorkbenchReport(t, repo, "case-a", Report{Verdict: "partial", Causes: []Cause{{Category: "retrieval", Status: "suspected"}}})
			v := Review{RunID: "case", AnalysisID: "case-a", Target: tc.target, Action: tc.action, Verdict: tc.verdict, CauseCategory: "retrieval", Reason: tc.reason, ReviewerID: "reviewer"}
			err := repo.AddReview(context.Background(), 1, &v)
			if !tc.valid {
				require.ErrorIs(t, err, ErrInvalid)
				var count int64
				require.NoError(t, repo.db.Model(&Review{}).Count(&count).Error)
				require.Zero(t, count)
				return
			}
			require.NoError(t, err)
			var saved Review
			require.NoError(t, repo.db.First(&saved, "id = ?", v.ID).Error)
			require.Equal(t, strings.TrimSpace(tc.reason), saved.Reason)
			require.Equal(t, tc.verdict, saved.Verdict)
			require.Equal(t, "case-a", saved.AnalysisID)
			require.Equal(t, "reviewer", saved.ReviewerID)
			require.False(t, saved.CreatedAt.IsZero())
		})
	}
}

func TestBlankConfirmationClosesReviewAndPreservesAudit(t *testing.T) {
	// Use the HTTP path to cover actor attribution and read-model updates too.
	_, repo, engine := workbenchFixture(t)
	seedWorkbench(t, repo, "case", "low", "human_review", "", 0)
	postWorkbenchReview(t, engine, "case", Review{AnalysisID: "case-a", Target: "answer", Action: "confirmed", Verdict: "supported"})
	run := workbenchGET(t, engine, "/runs/case")["run"].(map[string]any)
	require.Equal(t, "reviewed", run["review_state"])
	require.EqualValues(t, 0, workbenchGET(t, engine, "/runs?view=queue")["total"])
	page := workbenchGET(t, engine, "/reviews")
	require.EqualValues(t, 1, page["total"])
	review := page["items"].([]any)[0].(map[string]any)
	require.Equal(t, "", review["reason"])
	require.Equal(t, "reviewer", review["reviewer_id"])
	require.Equal(t, "case-a", review["analysis_id"])
	require.Equal(t, "supported", review["verdict"])
	require.NotEmpty(t, review["created_at"])
}
