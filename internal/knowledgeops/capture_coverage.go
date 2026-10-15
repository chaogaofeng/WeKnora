package knowledgeops

import (
	"slices"

	"github.com/Tencent/WeKnora/internal/knowledgeops/capture"
)

// CaptureCoverage separates exact answer-time content capture from source
// provenance. It is derived from the immutable capture audit, never backfilled
// from current knowledge or used to overwrite the original capture status.
type CaptureCoverage struct {
	ContentStatus    string   `json:"content_status"`
	ContentReason    string   `json:"content_reason,omitempty"`
	ProvenanceStatus string   `json:"provenance_status"`
	ContentGaps      []string `json:"content_gaps"`
	ProvenanceGaps   []string `json:"provenance_gaps"`
	ContextGaps      []string `json:"context_gaps,omitempty"`
}

func captureCoverage(run Run, evidence []Evidence) CaptureCoverage {
	v := CaptureCoverage{
		ContentStatus: run.CaptureStatus, ProvenanceStatus: "unknown",
		ContentGaps: []string{}, ProvenanceGaps: []string{},
	}
	if nonKnowledgeRun(run) {
		v.ContentStatus, v.ProvenanceStatus = "not_required", "not_required"
		return v
	}
	for _, gap := range run.Gaps {
		switch gap {
		case "source_version_unknown", "wiki_linked_summary_versions_unknown", "source_lookup_incomplete":
			v.ProvenanceGaps = append(v.ProvenanceGaps, gap)
		case "history_source_permissions_unbound", capture.HistoryAttachmentContextOmitted, capture.HistoryImageContextOmitted:
			v.ContextGaps = append(v.ContextGaps, gap)
		default:
			// New or unclassified gaps cannot silently promote content coverage.
			v.ContentGaps = append(v.ContentGaps, gap)
		}
	}
	switch run.CaptureStatus {
	case "complete":
		if len(v.ContentGaps) > 0 {
			v.ContentStatus = "partial"
		}
	case "partial":
		if run.Status == "completed" && len(v.ContentGaps) == 0 && len(v.ProvenanceGaps)+len(v.ContextGaps) > 0 {
			v.ContentStatus = "complete"
		}
	case "missing", "unknown":
		// Missing content is never inferred from a source version or vice versa.
	default:
		v.ContentStatus = "unknown"
	}
	// Only stored answer-time sources establish captured provenance. Current
	// post-hoc documents and unread candidates cannot fill the original gap.
	visible := false
	for _, e := range evidence {
		if !e.SentToModel || e.PostHoc || e.Kind == "conversation" {
			continue
		}
		visible = true
		if (e.Version == "" || e.Version == "unknown") && !slices.Contains(v.ProvenanceGaps, "source_version_unknown") {
			v.ProvenanceGaps = append(v.ProvenanceGaps, "source_version_unknown")
		}
	}
	if len(v.ProvenanceGaps) > 0 {
		v.ProvenanceStatus = "pending"
	} else if visible && v.ContentStatus == "complete" {
		v.ProvenanceStatus = "known"
	}
	// Only an explicitly observed skip with no capture failures establishes
	// that the lack of source bodies was intentional. Old runs remain unknown.
	if !visible && skippedSourceInput(run) {
		v.ContentReason = "not_provided"
	}
	return v
}

func skippedSourceInput(run Run) bool {
	if run.Status != "completed" || run.CaptureStatus != "missing" || run.QueryContext == nil || run.QueryContext.Truncated ||
		run.QueryContext.Retrieval == nil || run.QueryContext.Retrieval.Status != "skipped" || len(run.QueryContext.Searches) > 0 ||
		!slices.Contains(run.Gaps, "no_visible_source") {
		return false
	}
	for _, gap := range run.Gaps {
		if gap != "no_visible_source" && gap != "history_source_permissions_unbound" {
			return false
		}
	}
	return true
}
