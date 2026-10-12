package knowledgeops

import (
	"errors"
	"slices"
)

const sourceCharLimit = 16000

// SourceReadError reports safe limits while preserving SourceReader compatibility.
// Issues must use the module's stable codes; upstream error text is never stored.
type SourceReadError struct {
	Issues    []string
	CharLimit int
}

func (e *SourceReadError) Error() string { return "source comparison incomplete" }

func sourceFollowupPending(run Run) bool {
	return sourceFollowupReason(run) != ""
}

func sourceFollowupReason(run Run) string {
	a := run.Analysis
	if a == nil {
		return ""
	}
	if a.Report.SourceAudit != nil && len(a.Report.SourceAudit.Issues) > 0 {
		return "来源补查尚未完成，需补齐原始材料或解决访问、范围限制"
	}
	for _, gaps := range [][]string{run.Gaps, run.Coverage.ProvenanceGaps} {
		for _, gap := range gaps {
			if slices.Contains([]string{"source_version_unknown", "wiki_linked_summary_versions_unknown", "source_lookup_incomplete"}, gap) {
				return "来源版本或关联信息尚未齐全，需补充核验"
			}
		}
	}
	// Failed diagnoses use an unresolved placeholder, not a source finding.
	// Keep independently established gaps above, but do not invent new ones.
	if a.Status == "failed" {
		return ""
	}
	if a.Report.SourceCheck == "unresolved" {
		return "来源比较仍有未决事项，需核实原文与引用关系"
	}
	// A missing old status or an unchecked Wiki has no verified source comparison.
	if a.Report.SourceCheck == "" {
		return "来源核验状态缺失，需确认核验是否完成"
	}
	if a.Report.SourceCheck == "not_checked" && (run.QAType == "wiki-qa" || run.QAType == "hybrid-rag-wiki") {
		return "Wiki 与原始来源尚未完成比较，需核验来源"
	}
	return ""
}

func sourceAuditForAttempt(err error) *SourceAudit {
	audit := &SourceAudit{EvidenceIDs: []string{}, Issues: []string{}}
	if err != nil {
		var sourceErr *SourceReadError
		if errors.As(err, &sourceErr) {
			audit.CharLimit = sourceErr.CharLimit
			for _, issue := range sourceErr.Issues {
				if slices.Contains(sourceIssueCodes, issue) && !slices.Contains(audit.Issues, issue) {
					audit.Issues = append(audit.Issues, issue)
				}
			}
		}
		if len(audit.Issues) == 0 {
			audit.Issues = append(audit.Issues, "source_lookup_failed")
		}
	}
	return audit
}

var sourceIssueCodes = []string{"source_lookup_failed", "source_access_denied", "source_page_version_changed", "source_linkage_missing", "source_document_limit", "source_document_lookup_failed", "source_chunk_refs_missing", "source_chunks_missing", "source_chunk_limit", "source_char_budget_exhausted", "source_linked_versions_unknown"}
