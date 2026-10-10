package knowledgeops

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// RegisterHTTP expects the application's authenticated Admin route group.
// API keys are deliberately not registered as an allowed console authority.
func (s *Service) RegisterHTTP(g *gin.RouterGroup) {
	g.Use(func(c *gin.Context) {
		caller := types.CallerFromContext(c.Request.Context())
		p, ok := types.PrincipalFromContext(c.Request.Context())
		if caller.TenantID == 0 || caller.UserID == "" || !caller.Role.HasPermission(types.TenantRoleAdmin) || (ok && p.Type != types.PrincipalWebUser) {
			c.AbortWithStatusJSON(403, gin.H{"success": false, "error": "forbidden"})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	g.GET("/settings", s.getSettings)
	g.PUT("/settings", s.putSettings)
	g.GET("/overview", s.overview)
	g.GET("/runs", s.listRuns)
	g.GET("/runs/:id", s.detail)
	g.POST("/runs/:id/retry", s.retry)
	g.POST("/runs/:id/reviews", s.review)
	g.GET("/reviews", s.listReviewRecords)
	g.GET("/groups", s.listGroups)
}
func tenant(c *gin.Context) uint64 { return types.CallerFromContext(c.Request.Context()).TenantID }
func reply(c *gin.Context, data any, err error) {
	if err == nil {
		c.JSON(200, gin.H{"success": true, "data": data})
		return
	}
	status := 500
	code := "knowledge_ops_failed"
	switch {
	case errors.Is(err, ErrForbidden), errors.Is(err, gorm.ErrRecordNotFound):
		status = 404
		code = "not_found"
	case errors.Is(err, ErrStale):
		status = 409
		code = "version_changed"
	case errors.Is(err, ErrInvalid):
		status = 400
		code = "invalid_input"
	case errors.Is(err, context.DeadlineExceeded):
		status = 504
		code = "request_timeout"
	}
	c.JSON(status, gin.H{"success": false, "error": code})
}
func (s *Service) getSettings(c *gin.Context) {
	v, e := s.repo.Settings(c.Request.Context(), tenant(c))
	reply(c, v, e)
}
func (s *Service) putSettings(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16384)
	var v Settings
	if c.ShouldBindJSON(&v) != nil {
		reply(c, nil, ErrInvalid)
		return
	}
	if v.Enabled {
		if s.analyzer == nil || s.analyzer.models == nil {
			reply(c, nil, ErrInvalid)
			return
		}
		if _, err := s.analyzer.models(c.Request.Context(), v.ModelID); err != nil {
			reply(c, nil, ErrInvalid)
			return
		}
	}
	if err := s.repo.SaveSettings(c.Request.Context(), tenant(c), v); err != nil {
		reply(c, nil, err)
		return
	}
	s.getSettings(c)
}
func (s *Service) detail(c *gin.Context) {
	v, e := s.Detail(c.Request.Context(), tenant(c), c.Param("id"))
	v.Run.ReviewState = reviewState(v)
	reply(c, v, e)
}
func (s *Service) retry(c *gin.Context) {
	if _, err := s.Detail(c.Request.Context(), tenant(c), c.Param("id")); err != nil {
		reply(c, nil, err)
		return
	}
	settings, err := s.repo.Settings(c.Request.Context(), tenant(c))
	if err != nil {
		reply(c, nil, err)
		return
	}
	if !settings.Enabled {
		reply(c, nil, ErrInvalid)
		return
	}
	reply(c, gin.H{"queued": true}, s.repo.Retry(c.Request.Context(), tenant(c), c.Param("id")))
}
func (s *Service) review(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16384)
	var v Review
	if c.ShouldBindJSON(&v) != nil {
		reply(c, nil, ErrInvalid)
		return
	}
	if _, err := s.Detail(c.Request.Context(), tenant(c), c.Param("id")); err != nil {
		reply(c, nil, err)
		return
	}
	v.RunID = c.Param("id")
	v.ReviewerID = types.CallerFromContext(c.Request.Context()).UserID
	err := s.repo.AddReview(c.Request.Context(), tenant(c), &v)
	reply(c, v, err)
}
func pagination(c *gin.Context) (int, int) {
	p, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	n, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if p < 1 {
		p = 1
	}
	if n < 1 || n > 100 {
		n = 20
	}
	if p > 100000 {
		p = 100000
	}
	return p, n
}

// walk checks permissions before exposing even counts, snippets, or review rows.
// Keyset batches bound database memory; request cancellation bounds total work.
func (s *Service) walk(c *gin.Context, visit func(Detail)) error {
	ctx := c.Request.Context()
	last := ""
	var lastTime time.Time
	for {
		q := s.repo.db.WithContext(ctx).Where("tenant_id = ?", tenant(c))
		if last != "" {
			q = q.Where("started_at < ? OR (started_at = ? AND id < ?)", lastTime, lastTime, last)
		}
		for _, f := range []string{"channel", "qa_type", "capture_status"} {
			if v := c.Query(f); v != "" {
				q = q.Where(f+" = ?", v)
			}
		}
		for _, f := range []string{"from", "to"} {
			if v := c.Query(f); v != "" {
				t, err := time.Parse(time.RFC3339, v)
				if err != nil {
					return ErrInvalid
				}
				op := ">="
				if f == "to" {
					op = "<"
				}
				q = q.Where("started_at "+op+" ?", t)
			}
		}
		var rows []Run
		if err := q.Order("started_at DESC,id DESC").Limit(100).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		for _, r := range rows {
			last = r.ID
			lastTime = r.StartedAt
			d, err := s.loadDetail(ctx, tenant(c), r.ID, false)
			if errors.Is(err, ErrForbidden) || errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			if status := c.Query("content_status"); status != "" && status != d.Run.Coverage.ContentStatus {
				continue
			}
			if status := c.Query("provenance_status"); status != "" && status != d.Run.Coverage.ProvenanceStatus {
				continue
			}
			if search := strings.TrimSpace(c.Query("search")); search != "" && !strings.Contains(strings.ToLower(r.Question), strings.ToLower(search)) {
				continue
			}
			a := d.Run.Analysis
			if route := c.Query("route"); route != "" {
				match := a != nil && a.Route == route
				for _, pendingRoute := range queueRoutes(d) {
					if pendingRoute == route {
						match = true
					}
				}
				if c.Query("view") == "queue" {
					match = false
					for _, pendingRoute := range queueRoutes(d) {
						if pendingRoute == route {
							match = true
						}
					}
				}
				if !match {
					continue
				}
			}
			if verdict := c.Query("verdict"); verdict != "" && (a == nil || a.Report.Verdict != verdict) {
				continue
			}
			d.Run.ReviewState = reviewState(d)
			if c.Query("view") == "queue" && len(queueRoutes(d)) == 0 {
				continue
			}
			if risk := c.Query("risk"); risk != "" && (a == nil || a.Report.Risk != risk) {
				continue
			}
			if category := c.Query("cause_category"); category != "" {
				found := false
				if a != nil {
					for _, cause := range a.Report.Causes {
						if cause.Category == category {
							found = true
						}
					}
				}
				if !found {
					continue
				}
			}
			if group := c.Query("group_key"); group != "" && (a == nil || a.GroupKey != group) {
				continue
			}
			visit(d)
		}
	}
}
func reviewState(d Detail) string {
	state := humanReviewState(d)
	if state == "reviewed" && sourceFollowupPending(d.Run) {
		return "partially_reviewed"
	}
	return state
}

// Human review completion is independent of outstanding source verification.
func humanReviewState(d Detail) string {
	if d.Run.Analysis == nil {
		return "pending_analysis"
	}
	a := d.Run.Analysis
	answer, cause := false, len(a.Report.Causes) == 0
	seen := map[string]bool{}
	for _, r := range d.Reviews {
		if r.AnalysisID != a.ID || seen[r.Target] {
			continue
		}
		seen[r.Target] = true
		if r.Target == "answer" {
			answer = r.Action != "needs_more_evidence"
		}
		if r.Target == "cause" {
			cause = r.Action != "needs_more_evidence"
		}
	}
	if answer && cause {
		return "reviewed"
	}
	if answer || cause && len(a.Report.Causes) > 0 {
		return "partially_reviewed"
	}
	return "unreviewed"
}
func (s *Service) listRuns(c *gin.Context) {
	p, n := pagination(c)
	all := []Run{}
	offset := (p - 1) * n
	total := 0
	queue := c.Query("view") == "queue"
	err := s.walk(c, func(d Detail) {
		if queue || (total >= offset && len(all) < n) {
			all = append(all, d.Run)
		}
		total++
	})
	if err != nil {
		reply(c, nil, err)
		return
	}
	if queue {
		sort.Slice(all, func(i, j int) bool { return queueBefore(all[i], all[j]) })
		start := min(offset, len(all))
		all = all[start:min(start+n, len(all))]
	}
	reply(c, Page[Run]{Items: all, Total: total}, nil)
}
func (s *Service) overview(c *gin.Context) {
	v := Overview{WorkbenchStats: newWorkbenchStats(), ByMode: map[string]int{}, ByRoute: map[string]int{}, ByVerdict: map[string]int{}}
	err := s.walk(c, func(d Detail) {
		v.WorkbenchStats.observe(d)
		v.Total++
		switch d.Run.CaptureStatus {
		case "complete":
			v.Complete++
		case "partial":
			v.Partial++
		default:
			v.Missing++
		}
		v.ByMode[d.Run.QAType]++
		if a := d.Run.Analysis; a != nil {
			v.ByRoute[a.Route]++
			v.ByVerdict[a.Report.Verdict]++
		}
		v.PendingTasks += d.PendingTasks
		v.ReviewCount += len(d.Reviews)
	})
	reply(c, v, err)
}
func sortRuns(v []Run) {
	sort.Slice(v, func(i, j int) bool {
		if v[i].StartedAt.Equal(v[j].StartedAt) {
			return v[i].ID > v[j].ID
		}
		return v[i].StartedAt.After(v[j].StartedAt)
	})
}
func sortReviews(v []Review) {
	sort.Slice(v, func(i, j int) bool { return v[i].CreatedAt.After(v[j].CreatedAt) })
}
