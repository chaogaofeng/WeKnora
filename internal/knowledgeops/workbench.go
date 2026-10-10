package knowledgeops

import (
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// Workbench fields are derived read models; no business or evidence tables change.
type Coverage struct {
	Total    int `json:"total"`
	Complete int `json:"complete"`
	Partial  int `json:"partial"`
	Missing  int `json:"missing"`
}
type WorkbenchStats struct {
	ContentCoverage       Coverage             `json:"content_coverage"`
	ContentCoverageByMode map[string]*Coverage `json:"content_coverage_by_mode"`
	ProvenanceCounts      map[string]int       `json:"provenance_counts"`
	CoverageByMode        map[string]*Coverage `json:"coverage_by_mode"`
	SourceChecks          map[string]int       `json:"source_checks"`
	Causes                map[string]int       `json:"causes"`
	QueueCounts           map[string]int       `json:"queue_counts"`
	PendingRuns           int                  `json:"pending_runs"`
	DiagnosisStatus       map[string]int       `json:"diagnosis_status"`
	SampleTotal           int                  `json:"sample_total"`
	SampleReviewed        int                  `json:"sample_reviewed"`
	SampleCorrected       int                  `json:"sample_corrected"`
}

func newWorkbenchStats() WorkbenchStats {
	return WorkbenchStats{ContentCoverageByMode: map[string]*Coverage{}, ProvenanceCounts: map[string]int{}, CoverageByMode: map[string]*Coverage{}, SourceChecks: map[string]int{}, Causes: map[string]int{}, QueueCounts: map[string]int{}, DiagnosisStatus: map[string]int{}}
}

func (v *Coverage) observe(status string) {
	v.Total++
	switch status {
	case "complete":
		v.Complete++
	case "partial":
		v.Partial++
	default:
		v.Missing++
	}
}
func queueRoutes(d Detail) []string {
	a := d.Run.Analysis
	result := []string{}
	if a != nil && humanReviewState(d) != "reviewed" && a.Route != "auto_record" {
		result = append(result, a.Route)
	}
	if sourceFollowupPending(d.Run) && !slices.Contains(result, "source_verification") {
		result = append(result, "source_verification")
	}
	// Technical failures stay actionable even after a business conclusion is recorded.
	if (a != nil && a.Status == "failed") || d.Run.Status == "failed" || d.Run.Status == "cancelled" {
		found := false
		for _, r := range result {
			if r == "technical_followup" {
				found = true
			}
		}
		if !found {
			result = append(result, "technical_followup")
		}
	}
	if a != nil {
		seen := map[string]bool{}
		for _, r := range d.Reviews {
			if r.AnalysisID != a.ID || seen[r.Target] {
				continue
			}
			seen[r.Target] = true
			if r.Action == "needs_more_evidence" {
				found := false
				for _, route := range result {
					if route == "needs_evidence" {
						found = true
					}
				}
				if !found {
					result = append(result, "needs_evidence")
				}
				break
			}
		}
	}
	return result
}
func (v *WorkbenchStats) observe(d Detail) {
	mode := d.Run.QAType
	if mode == "" {
		mode = "unknown"
	}
	assessment := captureCoverage(d.Run, d.Evidence)
	v.ContentCoverage.observe(assessment.ContentStatus)
	if v.ContentCoverageByMode[mode] == nil {
		v.ContentCoverageByMode[mode] = &Coverage{}
	}
	v.ContentCoverageByMode[mode].observe(assessment.ContentStatus)
	v.ProvenanceCounts[assessment.ProvenanceStatus]++
	if v.CoverageByMode[mode] == nil {
		v.CoverageByMode[mode] = &Coverage{}
	}
	coverage := v.CoverageByMode[mode]
	coverage.Total++
	switch d.Run.CaptureStatus {
	case "complete":
		coverage.Complete++
	case "partial":
		coverage.Partial++
	default:
		coverage.Missing++
	}
	routes := queueRoutes(d)
	if len(routes) > 0 {
		v.PendingRuns++
	}
	for _, route := range routes {
		v.QueueCounts[route]++
	}
	a := d.Run.Analysis
	if a == nil {
		v.DiagnosisStatus["pending"]++
		return
	}
	v.DiagnosisStatus[a.Status]++
	// Execution failures have no valid quality finding. Keep them in diagnosis
	// status and actionable queues, never in answer/source/cause distributions.
	if a.Status != "succeeded" {
		return
	}
	source := a.Report.SourceCheck
	if source == "" {
		source = "unknown"
	} else if source == "not_checked" && d.Run.QAType == "rag-qa" && !slices.ContainsFunc(d.Evidence, func(e Evidence) bool {
		return e.SentToModel && !e.PostHoc && e.PageID != ""
	}) {
		// The runtime compares Wiki pages to their linked originals. Plain RAG
		// with no consumed Wiki page has no such comparison; this is not a fault.
		source = "not_applicable"
	}
	v.SourceChecks[source]++
	seen := map[string]bool{}
	for _, cause := range assessCauses(a.Report).Candidates {
		if !seen[cause.Category] {
			v.Causes[cause.Category]++
			seen[cause.Category] = true
		}
	}
	if a.Route == "quality_sample" {
		v.SampleTotal++
		if humanReviewState(d) == "reviewed" {
			v.SampleReviewed++
		}
		for _, r := range d.Reviews {
			if r.AnalysisID == a.ID && r.Action == "corrected" {
				v.SampleCorrected++
				break
			}
		}
	}
}
func queueBefore(a, b Run) bool {
	risk := func(r Run) int {
		if r.Analysis == nil {
			return 3
		}
		switch r.Analysis.Report.Risk {
		case "high":
			return 0
		case "medium":
			return 1
		case "low":
			return 2
		}
		return 3
	}
	if risk(a) != risk(b) {
		return risk(a) < risk(b)
	}
	// No unsupported business-impact score: oldest analysis first within a risk tier.
	at, bt := a.StartedAt, b.StartedAt
	if a.Analysis != nil {
		at = a.Analysis.CreatedAt
	}
	if b.Analysis != nil {
		bt = b.Analysis.CreatedAt
	}
	if !at.Equal(bt) {
		return at.Before(bt)
	}
	return a.ID < b.ID
}

type IssueGroup struct {
	Key            string `json:"key"`
	Count          int    `json:"count"`
	Representative Run    `json:"representative"`
}

func (s *Service) listGroups(c *gin.Context) {
	query := c.Request.URL.Query()
	query.Set("view", "queue")
	c.Request.URL.RawQuery = query.Encode()
	groups := map[string]*IssueGroup{}
	err := s.walk(c, func(d Detail) {
		key := "run:" + d.Run.ID
		if group := effectiveGroupKey(d.Run.Analysis); group != "" {
			key = group
		}
		g := groups[key]
		if g == nil {
			g = &IssueGroup{Key: key, Representative: d.Run}
			groups[key] = g
		}
		g.Count++
		if queueBefore(d.Run, g.Representative) {
			g.Representative = d.Run
		}
	})
	if err != nil {
		reply(c, nil, err)
		return
	}
	all := []IssueGroup{}
	for _, g := range groups {
		all = append(all, *g)
	}
	sort.Slice(all, func(i, j int) bool { return queueBefore(all[i].Representative, all[j].Representative) })
	p, n := pagination(c)
	start := min((p-1)*n, len(all))
	reply(c, Page[IssueGroup]{Items: all[start:min(start+n, len(all))], Total: len(all)}, nil)
}

type ReviewRecord struct {
	Review
	Question        string  `json:"question"`
	AnalysisVersion int     `json:"analysis_version"`
	MachineVerdict  string  `json:"machine_verdict"`
	MachineCauses   []Cause `json:"machine_causes"`
}

func reviewTime(c *gin.Context, key string) (*time.Time, error) {
	value := c.Query(key)
	if value == "" {
		return nil, nil
	}
	t, e := time.Parse(time.RFC3339, value)
	if e != nil {
		return nil, ErrInvalid
	}
	return &t, nil
}
func (s *Service) listReviewRecords(c *gin.Context) {
	from, err := reviewTime(c, "review_from")
	if err != nil {
		reply(c, nil, err)
		return
	}
	to, err := reviewTime(c, "review_to")
	if err != nil || (from != nil && to != nil && !from.Before(*to)) {
		reply(c, nil, ErrInvalid)
		return
	}
	records := []ReviewRecord{}
	err = s.walk(c, func(d Detail) {
		for _, r := range d.Reviews {
			if v := c.Query("target"); v != "" && r.Target != v {
				continue
			}
			if v := c.Query("action"); v != "" && r.Action != v {
				continue
			}
			if v := c.Query("reviewer_id"); v != "" && r.ReviewerID != v {
				continue
			}
			if from != nil && r.CreatedAt.Before(*from) || to != nil && !r.CreatedAt.Before(*to) {
				continue
			}
			if v := strings.TrimSpace(c.Query("review_search")); v != "" && !strings.Contains(strings.ToLower(d.Run.Question+" "+r.Reason), strings.ToLower(v)) {
				continue
			}
			records = append(records, ReviewRecord{Review: r, Question: d.Run.Question})
		}
	})
	if err != nil {
		reply(c, nil, err)
		return
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].CreatedAt.Equal(records[j].CreatedAt) {
			return records[i].ID > records[j].ID
		}
		return records[i].CreatedAt.After(records[j].CreatedAt)
	})
	p, n := pagination(c)
	total := len(records)
	start := min((p-1)*n, total)
	records = records[start:min(start+n, total)]
	// Resolve the exact historical version, never substitute the latest machine decision.
	for i := range records {
		row := &records[i]
		var a Analysis
		if err = s.repo.db.WithContext(c.Request.Context()).Where("tenant_id = ? AND run_id = ? AND id = ?", tenant(c), row.RunID, row.AnalysisID).First(&a).Error; err != nil {
			reply(c, nil, err)
			return
		}
		row.AnalysisVersion = a.Version
		row.MachineVerdict = a.Report.Verdict
		row.MachineCauses = a.Report.Causes
	}
	reply(c, Page[ReviewRecord]{Items: records, Total: total}, nil)
}
