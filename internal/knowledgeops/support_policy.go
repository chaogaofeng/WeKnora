package knowledgeops

import "slices"

// Resolving exact, authorized historical turns removes that one contextual
// uncertainty for diagnosis. It neither turns prior answers into knowledge
// evidence nor alters the immutable capture audit stored on the run.
func withResolvedHistoryCapture(run Run) Run {
	if run.CaptureStatus != "partial" || run.AnalysisHistory == nil || run.AnalysisHistory.Status != "complete" ||
		len(run.AnalysisHistory.ContextGaps) > 0 ||
		!slices.Contains(run.Gaps, "history_source_permissions_unbound") ||
		run.HistoryContext == nil || !run.HistoryContext.IDsKnown || run.HistoryContext.Turns == 0 ||
		len(run.AnalysisHistory.Turns) != run.HistoryContext.Turns || len(run.HistoryContext.TurnIDs) != run.HistoryContext.Turns ||
		run.HistoryContext.Compacted || run.HistoryContext.EverCompacted {
		return run
	}
	seen := map[string]bool{}
	for _, turn := range run.AnalysisHistory.Turns {
		if turn.TurnID == "" || seen[turn.TurnID] || !slices.Contains(run.HistoryContext.TurnIDs, turn.TurnID) {
			return run
		}
		seen[turn.TurnID] = true
	}
	run.Gaps = slices.DeleteFunc(slices.Clone(run.Gaps), func(gap string) bool { return gap == "history_source_permissions_unbound" })
	if len(run.Gaps) == 0 {
		run.CaptureStatus = "complete"
	}
	return run
}

// Unknown/new gaps stay conservative. Only explicitly identified provenance
// gaps are allowed to preserve positive support from captured source bodies.
func provenanceOnlyCapture(run Run) bool {
	if run.CaptureStatus != "partial" || len(run.Gaps) == 0 {
		return false
	}
	versionGap := false
	for _, gap := range run.Gaps {
		switch gap {
		case "source_version_unknown", "wiki_linked_summary_versions_unknown":
			versionGap = true
		case "source_lookup_incomplete": // Post-hoc lookup is not original context.
		default:
			return false
		}
	}
	return versionGap
}

func applySupportPolicy(run Run, report *Report) {
	run = withResolvedHistoryCapture(run)
	// Overwrite any model-supplied audit fields. Old stored reports without these
	// fields remain distinguishable from new, audited diagnoses.
	report.ModelVerdict = report.Verdict
	report.VerdictAdjustment = ""
	if needsNoKnowledgeCheck(run, *report) || run.CaptureStatus == "complete" {
		return
	}
	if provenanceOnlyCapture(run) {
		report.Unknowns = append(report.Unknowns, "来源版本信息不完整；支持性仅针对已保存的回答时正文，来源版本及有效性仍待核验")
	} else {
		if report.Verdict == "supported" {
			report.Verdict = "partial"
			report.VerdictAdjustment = "capture_context_incomplete"
			if run.CaptureStatus == "missing" {
				report.Verdict = "unverifiable"
				report.VerdictAdjustment = "capture_context_missing"
				if skippedSourceInput(run) {
					report.VerdictAdjustment = "no_source_body_provided"
				}
			}
		}
		if skippedSourceInput(run) {
			report.Unknowns = append(report.Unknowns, "本轮跳过检索，未提供来源正文；历史对话仅用于核对上下文，不能证明知识性论断得到支持")
		} else {
			report.Unknowns = append(report.Unknowns, "生成时上下文采集不完整，整体支持性结论受限")
		}
	}
	// Absence of support still requires complete capture, even when the known
	// gap concerns provenance. Positive and contradictory evidence can stand.
	if report.Verdict == "unsupported" {
		report.Verdict = "unverifiable"
		report.VerdictAdjustment = "capture_absence_unverifiable"
	}
	for i := range report.Claims {
		if report.Claims[i].Verdict == "unsupported" {
			report.Claims[i].Verdict = "unverifiable"
		}
	}
}
