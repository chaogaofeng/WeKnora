package knowledgeops

import (
	"errors"
	"slices"
	"strings"
)

// SourceReadError reports safe limits while preserving SourceReader compatibility.
// Issues must use the module's stable codes; upstream error text is never stored.
type SourceReadError struct {
	Issues    []string
	CharLimit int
}

func (e *SourceReadError) Error() string { return "source comparison incomplete" }

// SourceReadOutcome carries trusted scope without changing SourceReader's
// legacy signature. Zero issues is a successful lookup, not an error/gap.
type SourceReadOutcome struct {
	SourceReadError
	ComparisonScope string
	HumanPageIDs    []string
}

func (e *SourceReadOutcome) Error() string { return "source comparison scope recorded" }
func (e *SourceReadOutcome) Unwrap() error {
	if len(e.Issues) == 0 {
		return nil
	}
	return &e.SourceReadError
}

func sourceReadIncomplete(err error) bool {
	var outcome *SourceReadOutcome
	if errors.As(err, &outcome) {
		return len(outcome.Issues) > 0
	}
	return err != nil
}

func sourceFollowupPending(run Run) bool {
	return sourceFollowupReason(run) != ""
}

func sourceFollowupReason(run Run) string {
	a := run.Analysis
	if a == nil || nonKnowledgeRun(run) {
		return ""
	}
	if a.Report.SourceAudit != nil && len(a.Report.SourceAudit.Issues) > 0 {
		return sourceAuditReason(a.Report.SourceAudit.Issues)
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
	if a.Report.SourceCheck == "not_checked" && !humanReviewedSourceOnly(run, a.Report) && (run.QAType == "wiki-qa" || run.QAType == "hybrid-rag-wiki") {
		return "Wiki 与原始来源尚未完成比较，需核验来源"
	}
	return ""
}

func sourceAuditForAttempt(err error) *SourceAudit {
	audit := &SourceAudit{EvidenceIDs: []string{}, Issues: []string{}}
	if err != nil {
		var sourceErr *SourceReadError
		var outcome *SourceReadOutcome
		if errors.As(err, &outcome) {
			audit.ComparisonScope = outcome.ComparisonScope
			audit.HumanPageIDs = append([]string(nil), outcome.HumanPageIDs...)
			sourceErr = &outcome.SourceReadError
		} else {
			errors.As(err, &sourceErr)
		}
		if sourceErr != nil {
			audit.CharLimit = sourceErr.CharLimit
			for _, issue := range sourceErr.Issues {
				if slices.Contains(sourceIssueCodes, issue) && !slices.Contains(audit.Issues, issue) {
					audit.Issues = append(audit.Issues, issue)
				}
			}
		}
		if len(audit.Issues) == 0 && sourceReadIncomplete(err) {
			audit.Issues = append(audit.Issues, "source_lookup_failed")
		}
	}
	return audit
}

var sourceIssueCodes = []string{"source_lookup_failed", "source_access_denied", "source_page_version_changed", "source_linkage_missing", "source_document_limit", "source_document_lookup_failed", "source_chunk_refs_missing", "source_chunks_missing", "source_chunk_limit", "source_char_budget_exhausted", "source_linked_versions_unknown", "source_folder_links_missing", "source_folder_link_unavailable", "source_folder_link_cycle", "source_folder_page_limit", "source_folder_versions_unknown"}

// Keep operator-facing reasons tied to recorded limits, rather than implying
// that every incomplete comparison needs the user to upload more material.
func sourceAuditReason(issues []string) string {
	labels := map[string]string{
		"source_lookup_failed":           "来源读取失败",
		"source_access_denied":           "来源访问权限不足",
		"source_page_version_changed":    "Wiki 页面版本已变化",
		"source_linkage_missing":         "原始文档关联缺失",
		"source_document_limit":          "关联文档超过本次补查数量上限",
		"source_document_lookup_failed":  "关联原始文档不可用",
		"source_chunk_refs_missing":      "原文片段关联未记录",
		"source_chunks_missing":          "关联原文片段不可用",
		"source_chunk_limit":             "部分原文片段超出本次补查范围",
		"source_char_budget_exhausted":   "来源补查字符预算已用尽",
		"source_linked_versions_unknown": "关联来源版本未记录",
		"source_folder_links_missing":    "文件夹摘要未记录可追溯的关联页面",
		"source_folder_link_unavailable": "文件夹摘要的关联页面不可用",
		"source_folder_link_cycle":       "文件夹摘要的关联页面存在循环引用",
		"source_folder_page_limit":       "文件夹摘要的关联页面超出本次补查范围",
		"source_folder_versions_unknown": "文件夹关联页未采集回答时版本，仅可对照当前材料",
	}
	reasons := []string{}
	for _, issue := range issues {
		label := labels[issue]
		if label == "" {
			label = "来源补查仍有未决事项"
		}
		if !slices.Contains(reasons, label) {
			reasons = append(reasons, label)
		}
	}
	return strings.Join(reasons, "；")
}
