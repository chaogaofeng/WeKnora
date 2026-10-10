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
	a := run.Analysis
	if a == nil {
		return false
	}
	if a.Report.SourceAudit != nil && len(a.Report.SourceAudit.Issues) > 0 {
		return true
	}
	for _, gaps := range [][]string{run.Gaps, run.Coverage.ProvenanceGaps} {
		for _, gap := range gaps {
			if slices.Contains([]string{"source_version_unknown", "wiki_linked_summary_versions_unknown", "source_lookup_incomplete"}, gap) {
				return true
			}
		}
	}
	// Failed diagnoses use an unresolved placeholder, not a source finding.
	// Keep independently established gaps above, but do not invent new ones.
	if a.Status == "failed" {
		return false
	}
	if a.Report.SourceCheck == "unresolved" {
		return true
	}
	// A missing old status or an unchecked Wiki has no verified source comparison.
	return a.Report.SourceCheck == "" || a.Report.SourceCheck == "not_checked" && (run.QAType == "wiki-qa" || run.QAType == "hybrid-rag-wiki")
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
