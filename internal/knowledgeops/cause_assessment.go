package knowledgeops

// CauseAssessment is a read-only consistency check, not a second model verdict.
// Keep the original report for audit; an unlinked attribution is neither an
// established problem nor proof that no problem exists.
type CauseAssessment struct {
	Candidates []Cause `json:"candidates"`
	Unlinked   []Cause `json:"unlinked"`
}

func assessCauses(r Report) CauseAssessment {
	result := CauseAssessment{Candidates: []Cause{}, Unlinked: []Cause{}}
	answerProblem := false
	for _, claim := range r.Claims {
		switch claim.Verdict {
		case "partial", "unsupported", "contradicted":
			answerProblem = true
		}
	}
	for _, cause := range r.Causes {
		// Successful extraction is an observation, not a generation fault.
		// An aggregate partial caused by capture limits is not enough either.
		// Missing facts/conditions must be represented by a partial claim.
		if cause.Category == "generation" && !answerProblem {
			result.Unlinked = append(result.Unlinked, cause)
		} else {
			result.Candidates = append(result.Candidates, cause)
		}
	}
	return result
}

func effectiveGroupKey(a *Analysis) string {
	if a == nil || len(assessCauses(a.Report).Unlinked) > 0 {
		return ""
	}
	return a.GroupKey
}
