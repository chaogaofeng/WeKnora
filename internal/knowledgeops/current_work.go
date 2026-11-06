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
	Followups            []FollowupState
	Items                []PendingItem
}

type reviewProgress struct {
	state            string
	answerDone       bool
	evidenceRequests []Review
}

func currentReviewProgress(d Detail) reviewProgress {
	a := d.Run.Analysis
	if a == nil {
		return reviewProgress{state: "pending_analysis"}
	}
	progress := reviewProgress{state: "unreviewed"}
	// Only the newest answer review for this diagnosis controls completion.
	// Legacy cause reviews remain in audit history, but are no longer tasks.
	if review := currentAnswerReview(d); review != nil {
		if review.Action == "needs_more_evidence" {
			progress.state = "needs_more_evidence"
			progress.evidenceRequests = append(progress.evidenceRequests, *review)
		} else if review.Action == "confirmed" || review.Action == "corrected" {
			progress.answerDone = true
			progress.state = "reviewed"
		} else if currentAnswerOutcome(d) != "" {
			progress.answerDone = true
			progress.state = "reviewed"
		}
		return progress
	}
	if nonKnowledgeRun(d.Run) {
		progress.state = "not_required"
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
	}
	for _, request := range progress.evidenceRequests {
		work.Items = append(work.Items, PendingItem{Route: "needs_evidence", Reason: request.Reason, Target: request.Target, CauseCategory: request.CauseCategory, ReviewID: request.ID, ReviewerID: request.ReviewerID, CreatedAt: &request.CreatedAt})
		if !slices.Contains(work.PendingReviewTargets, request.Target) {
			work.PendingReviewTargets = append(work.PendingReviewTargets, request.Target)
		}
	}
	work.Followups = deriveFollowups(d)
	for _, followup := range work.Followups {
		if followup.State == "pending" {
			work.Items = append(work.Items, PendingItem{Route: followup.Route, Reason: followup.Reason})
		}
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
	if d.Run.Analysis.Status == "succeeded" && humanReviewedSourceOnly(d.Run, d.Run.Analysis.Report) {
		return humanReviewedSourceScope
	}
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
	d.Run.AnswerOutcome = currentAnswerOutcome(*d)
	d.Run.PendingReviewTargets = work.PendingReviewTargets
	d.Run.PendingItems = work.Items
	d.Run.EffectiveRisk = effectiveRisk(d.Run)
	d.Run.SourceCheckDisplay = sourceCheckDisplay(*d)
	d.Run.Followups = work.Followups
	d.Run.SourceFollowupPending = slices.ContainsFunc(work.Followups, func(f FollowupState) bool { return f.Route == "source_verification" && f.State == "pending" })
}
