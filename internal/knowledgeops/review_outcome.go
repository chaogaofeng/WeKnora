package knowledgeops

// answerReviewVerdict gives the independent human conclusion associated with
// each new review action. Machine-verdict actions retain their legacy meaning.
func answerReviewVerdict(action string) string {
	switch action {
	case "accepted":
		return "usable"
	case "revision_required":
		return "revision_required"
	case "excluded":
		return "excluded"
	case "needs_more_evidence":
		return "unverifiable"
	default:
		return ""
	}
}

// currentAnswerReview uses the same newest-first review history as detail and
// workbench responses. Reviews of other diagnoses or legacy causes are audit
// records and cannot determine the current answer's outcome.
func currentAnswerReview(d Detail) *Review {
	if d.Run.Analysis == nil {
		return nil
	}
	for i := range d.Reviews {
		if d.Reviews[i].AnalysisID == d.Run.Analysis.ID && d.Reviews[i].Target == "answer" {
			return &d.Reviews[i]
		}
	}
	return nil
}

// currentAnswerOutcome is deliberately separate from the machine report and
// review completion. In particular, legacy confirmation/correction does not
// constitute approval that the answer is usable.
func currentAnswerOutcome(d Detail) string {
	review := currentAnswerReview(d)
	if review == nil {
		return ""
	}
	verdict := answerReviewVerdict(review.Action)
	if verdict == "" || review.Verdict != verdict {
		return ""
	}
	if review.Action == "needs_more_evidence" {
		return "needs_evidence"
	}
	return verdict
}
