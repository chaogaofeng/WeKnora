package knowledgeops

import (
	"fmt"
	"slices"
	"strings"
)

func hasSourceComparisonEvidence(evidence []Evidence) bool {
	return slices.ContainsFunc(evidence, func(e Evidence) bool {
		return e.PostHoc && strings.TrimSpace(e.Content) != ""
	})
}

// constrainSourceCheck limits the model's judgment to what the worker could
// actually compare. It preserves claims and records corrections for review;
// malformed statuses and invalid evidence references still fail validation.
// The return value flags a suspected conflict that cannot be established with
// incomplete comparison, but still needs a human decision rather than just lookup.
func constrainSourceCheck(run Run, report *Report, evidence []Evidence) bool {
	switch report.SourceCheck {
	case "not_checked", "consistent", "inconsistent", "unresolved":
	default:
		return false
	}
	original := report.SourceCheck
	if slices.Contains(run.Gaps, "source_lookup_incomplete") {
		report.SourceCheck = "unresolved"
		report.Unknowns = append(report.Unknowns, fmt.Sprintf("模型原来源判定为 %s；来源关联、版本或有界补查范围不完整，实际核验状态为未解决", original))
	} else if (original == "consistent" || original == "inconsistent") && !hasSourceComparisonEvidence(evidence) {
		report.SourceCheck = "not_checked"
		report.Unknowns = append(report.Unknowns, fmt.Sprintf("模型原来源判定为 %s；未取得可用于来源对照的补查材料，已降级为未核验；回答支持性与来源一致性分别判断", original))
	}
	return original == "inconsistent" && report.SourceCheck == "unresolved" && hasSourceComparisonEvidence(evidence)
}
