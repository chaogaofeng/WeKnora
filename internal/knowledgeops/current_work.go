package knowledgeops

import (
	"slices"
	"strings"
	"time"
)

// PendingItem describes current work without rewriting the analysis or reviews
// that explain how it arose. Multiple items can share a queue route.
type PendingItem struct {
	Route         string     `json:"route"`
	Reason        string     `json:"reason"`
	Target        string     `json:"target,omitempty"`
	CauseCategory string     `json:"cause_category,omitempty"`
	ReviewID      string     `json:"review_id,omitempty"`
	ReviewerID    string     `json:"reviewer_id,omitempty"`
	CreatedAt     *time.Time `json:"created_at,omitempty"`
}

type currentWork struct {
	ReviewState          string
	PendingReviewTargets []string
	Items                []PendingItem
}

type reviewProgress struct {
	state            string
	answerDone       bool
	causeDone        bool
	evidenceRequests []Review
}

func currentReviewProgress(d Detail) reviewProgress {
	a := d.Run.Analysis
	if a == nil {
		return reviewProgress{state: "pending_analysis"}
	}
	hasCauses := len(assessCauses(a.Report).Candidates) > 0
	progress := reviewProgress{causeDone: !hasCauses}
	seenTargets, seenCauses := map[string]bool{}, map[string]bool{}
	// Reviews arrive newest first. Overall completion still has only two
	// conclusions; explicit requests additionally retain their own scope.
	for _, review := range d.Reviews {
		if review.AnalysisID != a.ID {
			continue
		}
		if !seenTargets[review.Target] {
			seenTargets[review.Target] = true
			switch review.Target {
			case "answer":
				progress.answerDone = review.Action != "needs_more_evidence"
			case "cause":
				progress.causeDone = review.Action != "needs_more_evidence"
			}
			if review.Target == "answer" && review.Action == "needs_more_evidence" {
				progress.evidenceRequests = append(progress.evidenceRequests, review)
			}
		}
		if review.Target == "cause" && !seenCauses[review.CauseCategory] {
			seenCauses[review.CauseCategory] = true
			if review.Action == "needs_more_evidence" {
				progress.evidenceRequests = append(progress.evidenceRequests, review)
			}
		}
	}
	for _, request := range progress.evidenceRequests {
		if request.Target == "cause" {
			progress.causeDone = false
		}
	}
	switch {
	case nonKnowledgeRun(d.Run) && len(seenTargets) == 0:
		progress.state = "not_required"
	case progress.answerDone && progress.causeDone:
		progress.state = "reviewed"
	case progress.answerDone || progress.causeDone && hasCauses:
		progress.state = "partially_reviewed"
	default:
		progress.state = "unreviewed"
	}
	return progress
}

// deriveCurrentWork is the single source of active queues, reasons and review
// targets. It never calls queueRoutes or humanReviewState, or mutates history.
func deriveCurrentWork(d Detail) currentWork {
	progress := currentReviewProgress(d)
	work := currentWork{ReviewState: progress.state, PendingReviewTargets: []string{}, Items: []PendingItem{}}
	a := d.Run.Analysis
	if a != nil && slices.Contains([]string{"human_review", "needs_evidence", "quality_sample"}, a.Route) && progress.state != "reviewed" {
		reason := a.RouteReason
		if strings.TrimSpace(reason) == "" {
			switch a.Route {
			case "needs_evidence":
				reason = "采集或诊断证据不足，需补充证据并复核"
			case "quality_sample":
				reason = "已进入质量抽检，需记录人工复核结论"
			default:
				reason = "机器诊断仍需人工复核"
			}
		}
		work.Items = append(work.Items, PendingItem{Route: a.Route, Reason: reason})
		if !progress.answerDone {
			work.PendingReviewTargets = append(work.PendingReviewTargets, "answer")
		}
		if !progress.causeDone {
			work.PendingReviewTargets = append(work.PendingReviewTargets, "cause")
		}
	}
	for _, request := range progress.evidenceRequests {
		work.Items = append(work.Items, PendingItem{Route: "needs_evidence", Reason: request.Reason, Target: request.Target, CauseCategory: request.CauseCategory, ReviewID: request.ID, ReviewerID: request.ReviewerID, CreatedAt: &request.CreatedAt})
		if !slices.Contains(work.PendingReviewTargets, request.Target) {
			work.PendingReviewTargets = append(work.PendingReviewTargets, request.Target)
		}
	}
	if reason := sourceFollowupReason(d.Run); reason != "" {
		work.Items = append(work.Items, PendingItem{Route: "source_verification", Reason: reason})
	}
	if reason := technicalFollowupReason(d.Run); reason != "" {
		work.Items = append(work.Items, PendingItem{Route: "technical_followup", Reason: reason})
	}
	return work
}

func technicalFollowupReason(run Run) string {
	reasons := []string{}
	if run.Analysis != nil && run.Analysis.Status == "failed" {
		reasons = append(reasons, "诊断执行失败，需排查执行异常并重新诊断")
	}
	switch run.Status {
	case "failed":
		reasons = append(reasons, "问答异常结束，需排查执行链路")
	case "cancelled":
		reasons = append(reasons, "问答已取消，需确认中断原因与采集完整性")
	}
	if len(reasons) == 0 && run.Analysis != nil && run.Analysis.Route == "technical_followup" {
		return "问答或诊断未正常完成，需技术排查"
	}
	return strings.Join(reasons, "；")
}

func effectiveRisk(run Run) string {
	if run.Analysis == nil {
		return "unknown"
	}
	if highRisk(run, run.Analysis.Report) {
		return "high"
	}
	if risk := run.Analysis.Report.Risk; risk != "" {
		return risk
	}
	return "unknown"
}

func sourceCheckDisplay(d Detail) string {
	if d.Run.Analysis == nil || d.Run.Analysis.Report.SourceCheck == "" {
		return "unknown"
	}
	if nonKnowledgeRun(d.Run) {
		return "not_applicable"
	}
	source := d.Run.Analysis.Report.SourceCheck
	if source == "not_checked" && d.Run.QAType == "rag-qa" && !slices.ContainsFunc(d.Evidence, func(e Evidence) bool {
		return e.SentToModel && !e.PostHoc && e.PageID != ""
	}) {
		return "not_applicable"
	}
	return source
}

func populateCurrentWork(d *Detail) {
	work := deriveCurrentWork(*d)
	d.Run.ReviewState = work.ReviewState
	d.Run.PendingReviewTargets = work.PendingReviewTargets
	d.Run.PendingItems = work.Items
	d.Run.EffectiveRisk = effectiveRisk(d.Run)
	d.Run.SourceCheckDisplay = sourceCheckDisplay(*d)
	d.Run.SourceFollowupPending = sourceFollowupPending(d.Run)
}
