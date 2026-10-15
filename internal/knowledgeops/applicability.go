package knowledgeops

import (
	"strings"

	"github.com/Tencent/WeKnora/internal/knowledgeops/capture"
)

// No claim is not sufficient: an unanswered knowledge request or an ambiguous
// follow-up still needs diagnosis. The model must explicitly assess the task
// AND the actual answer, with no unresolved finding or execution/capture loss.
func needsNoKnowledgeCheck(run Run, report Report) bool {
	if report.Applicability != "non_knowledge" || strings.TrimSpace(report.ApplicabilityReason) == "" ||
		report.Verdict != "not_applicable" || report.Risk != "low" || report.SourceCheck != "not_checked" ||
		len(report.Claims)+len(report.Causes)+len(report.Unknowns)+len(report.ReviewQuestions) != 0 ||
		run.Status != "completed" || strings.TrimSpace(run.Question) == "" || strings.TrimSpace(run.Answer) == "" || highRisk(run, report) {
		return false
	}
	for _, gap := range run.Gaps {
		switch gap {
		case "no_visible_source", "history_source_permissions_unbound", capture.HistoryAttachmentContextOmitted, capture.HistoryImageContextOmitted:
			// These do not establish missing evidence for a self-contained social reply.
		default:
			return false
		}
	}
	return true
}

func nonKnowledgeRun(run Run) bool {
	a := run.Analysis
	return a != nil && a.Status == "succeeded" && needsNoKnowledgeCheck(run, a.Report)
}
