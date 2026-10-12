package knowledgeops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/knowledgeops/capture"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type AccessChecker func(context.Context, Run, []Evidence) error
type Service struct {
	repo             *Repository
	analyzer         *Analyzer
	access           AccessChecker
	executionContext func(context.Context, Run) (context.Context, error)
	cancel           context.CancelFunc
	done             chan struct{}
	workerMu         sync.Mutex
}

func NewService(repo *Repository, analyzer *Analyzer, access AccessChecker) *Service {
	return &Service{repo: repo, analyzer: analyzer, access: access}
}
func (s *Service) SetExecutionContext(f func(context.Context, Run) (context.Context, error)) {
	s.executionContext = f
}
func (s *Service) Start(ctx context.Context, in capture.Start) capture.Recorder {
	if in.TenantID == 0 || in.MessageID == "" {
		return nil
	}
	bounded, cancel := context.WithTimeout(context.WithoutCancel(ctx), 250*time.Millisecond)
	defer cancel()
	settings, err := s.repo.Settings(bounded, in.TenantID)
	if err != nil {
		logger.Warnf(ctx, "[KnowledgeOps] cannot read capture setting: %v", err)
		return nil
	}
	if !settings.Enabled || settings.CaptureEnabledAt == nil || in.StartedAt.Before(*settings.CaptureEnabledAt) {
		return nil
	}
	run := Run{ID: uuid.NewString(), TenantID: in.TenantID, ActorID: in.ActorID, SessionID: in.SessionID, MessageID: in.MessageID, RequestID: in.RequestID, Channel: in.Channel, QAType: in.QAType, ExecutionMode: in.ExecutionMode, AgentID: in.AgentID, ModelID: in.ModelID, Question: in.Question, StartedAt: in.StartedAt.UTC(), Status: "running", CaptureStatus: "unknown", Gaps: []string{}}
	if len([]rune(run.Question)) > 16000 {
		run.Question = string([]rune(run.Question)[:16000])
		run.Gaps = append(run.Gaps, "question_capture_budget")
	}
	if err = s.repo.CreateRun(bounded, &run); err != nil {
		logger.Warnf(ctx, "[KnowledgeOps] capture start failed (tenant=%d message=%s): %v", in.TenantID, in.MessageID, err)
		return nil
	}
	return &recorder{service: s, run: run, settings: settings, seen: map[string]bool{}}
}

type recorder struct {
	mu                     sync.Mutex
	service                *Service
	run                    Run
	settings               Settings
	seen                   map[string]bool
	chars, inputs, visible int
	finished               bool
	heartbeat              time.Time
	ioUsed                 time.Duration
}

func (r *recorder) gap(reason string) {
	if len(r.run.Gaps) >= 64 {
		return
	}
	if len(reason) > 512 {
		reason = reason[:512]
	}
	if !slices.Contains(r.run.Gaps, reason) {
		r.run.Gaps = append(r.run.Gaps, reason)
	}
}
func (r *recorder) Gap(ctx context.Context, reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.finished {
		r.gap(reason)
	}
}
func (r *recorder) RecordInput(ctx context.Context, in capture.Input) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finished {
		return
	}
	r.inputs++
	if in.ModelID != "" {
		if r.run.ModelID != "" && r.run.ModelID != "unknown" && r.run.ModelID != in.ModelID {
			r.gap("multiple_generation_models")
		}
		r.run.ModelID = in.ModelID
	}
	// The question and answer are stored on Run; source blocks are stored with
	// provenance by RecordSources. Do not duplicate history, memory or retrieval
	// text in an unbound conversation row that could bypass source revocation.

}
func (r *recorder) RecordSources(ctx context.Context, sources []capture.Source) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finished {
		return
	}
	bounded, cancel := context.WithTimeout(context.WithoutCancel(ctx), 250*time.Millisecond)
	defer cancel()
	for _, src := range sources {
		if bounded.Err() != nil {
			r.gap("capture_io_budget")
			break
		}
		version := src.Version
		if version == "" {
			version = "unknown"
		}
		e := Evidence{Kind: src.Kind, KBID: src.KBID, KnowledgeID: src.KnowledgeID, ChunkID: src.ChunkID, PageID: src.PageID, Slug: src.Slug, Version: version, Content: src.Content, Title: src.Title, ToolCallID: src.ToolCallID, Round: r.inputs, SentToModel: src.SentToModel, PostHoc: src.PostHoc, Truncated: src.Truncated, AcquiredAt: src.AcquiredAt}
		if e.AcquiredAt.IsZero() {
			e.AcquiredAt = time.Now().UTC()
		}
		if e.SentToModel && e.KBID != "" && e.Version == "unknown" {
			r.gap("source_version_unknown")
		}
		// A model-visible excerpt is still an exact capture; source truncation
		// is metadata, whereas loss during this module's capture is a gap.
		r.save(bounded, e)
	}
}
func (r *recorder) save(ctx context.Context, e Evidence) {
	if r.ioUsed >= 500*time.Millisecond {
		r.gap("capture_io_budget")
		return
	}
	if e.Content == "" && e.KBID == "" && e.PageID == "" && e.ChunkID == "" {
		return
	}
	if len(r.seen) >= 512 {
		r.gap("capture_source_count_budget")
		return
	}
	// Preserve candidates only as metadata; they are not input to the evaluator.
	if !e.SentToModel && !e.PostHoc {
		e.Content = ""
	}
	keyBytes, _ := json.Marshal([]any{e.Kind, e.KBID, e.KnowledgeID, e.ChunkID, e.PageID, e.Slug, e.Version, e.Content, e.SentToModel, e.PostHoc, e.ToolCallID})
	key := sha256.Sum256(keyBytes)
	fingerprint := hex.EncodeToString(key[:])
	if r.seen[fingerprint] {
		return
	}
	r.seen[fingerprint] = true
	body := []rune(e.Content)
	remaining := r.settings.MaxEvidenceChars - r.chars
	if remaining < 0 {
		remaining = 0
	}
	if len(body) > remaining {
		body = body[:remaining]
		e.Content = string(body)
		e.Truncated = true
		r.gap("capture_budget_exhausted")
	}
	r.chars += len(body)
	h := sha256.Sum256([]byte(e.Content))
	e.Hash = hex.EncodeToString(h[:])
	e.ID = uuid.NewString()
	e.TenantID = r.run.TenantID
	e.RunID = r.run.ID
	ioStart := time.Now()
	defer func() { r.ioUsed += time.Since(ioStart) }()
	bounded, cancel := context.WithTimeout(ctx, min(100*time.Millisecond, 500*time.Millisecond-r.ioUsed))
	defer cancel()
	if err := r.service.repo.db.WithContext(bounded).Create(&e).Error; err != nil {
		r.gap("evidence_persist_failed")
		logger.Warnf(ctx, "[KnowledgeOps] evidence persist failed run=%s: %v", r.run.ID, err)
		return
	}
	if time.Since(r.heartbeat) > 30*time.Second {
		r.heartbeat = time.Now().UTC()
		if err := r.service.repo.db.WithContext(bounded).Model(&Run{}).Where("id = ? AND status = ?", r.run.ID, "running").Update("updated_at", r.heartbeat).Error; err != nil {
			r.gap("capture_heartbeat_failed")
		}
	}
	if e.SentToModel && e.Content != "" && e.Kind != "conversation" {
		r.visible++
	}
}
func (r *recorder) Finish(ctx context.Context, end capture.Completion) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finished {
		return
	}
	r.finished = true
	if end.FinishedAt.IsZero() {
		end.FinishedAt = time.Now().UTC()
	}
	if end.Status != "completed" && end.Status != "cancelled" && end.Status != "failed" {
		end.Status = "failed"
		r.gap("unknown_end_status")
	}
	if r.inputs == 0 {
		r.gap("model_input_not_captured")
	}
	status := "complete"
	if len(r.run.Gaps) > 0 {
		status = "partial"
	}
	if r.visible == 0 {
		status = "missing"
		r.gap("no_visible_source")
	}
	if len([]rune(end.Answer)) > 24000 {
		end.Answer = string([]rune(end.Answer)[:24000])
		r.gap("answer_capture_budget")
		status = "partial"
	}
	if end.Status != "completed" {
		r.gap("run_" + end.Status)
		if status == "complete" {
			status = "partial"
		}
	}
	gaps, _ := json.Marshal(r.run.Gaps)
	bounded, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	err := r.service.repo.db.WithContext(bounded).Transaction(func(tx *gorm.DB) error {
		q := tx.Model(&Run{}).Where("id = ? AND status = ?", r.run.ID, "running").Updates(map[string]any{"answer": end.Answer, "model_id": r.run.ModelID, "status": end.Status, "capture_status": status, "gaps": string(gaps), "finished_at": end.FinishedAt, "updated_at": time.Now().UTC()})
		if q.Error != nil {
			return q.Error
		}
		if q.RowsAffected == 0 {
			return ErrStale
		}
		return NewRepository(tx).Enqueue(bounded, r.run, 1)
	})
	if err != nil {
		logger.Warnf(ctx, "[KnowledgeOps] capture finish failed run=%s: %v", r.run.ID, err)
	}
}
func (s *Service) Detail(ctx context.Context, tenant uint64, id string) (Detail, error) {
	return s.loadDetail(ctx, tenant, id, true)
}
func (s *Service) loadDetail(ctx context.Context, tenant uint64, id string, full bool) (Detail, error) {
	d := Detail{}
	var err error
	d.Run, err = s.repo.Run(ctx, tenant, id)
	if err != nil {
		return d, err
	}
	if full {
		d.Evidence, err = s.repo.Evidence(ctx, tenant, id)
	} else {
		err = s.repo.db.WithContext(ctx).Omit("content", "title").Where("tenant_id = ? AND run_id = ?", tenant, id).Find(&d.Evidence).Error
	}
	if err != nil {
		return d, err
	}
	if s.access == nil {
		return Detail{}, ErrForbidden
	}
	if err = s.access(ctx, d.Run, d.Evidence); err != nil {
		return Detail{}, ErrForbidden
	}
	d.Run.Coverage = captureCoverage(d.Run, d.Evidence)
	if full {
		d.Analyses, err = s.repo.Analyses(ctx, tenant, id)
	} else {
		err = s.repo.db.WithContext(ctx).Where("tenant_id = ? AND run_id = ?", tenant, id).Order("version DESC").Limit(1).Find(&d.Analyses).Error
	}
	if err != nil {
		return Detail{}, err
	}
	for i := range d.Analyses {
		assessment := assessCauses(d.Analyses[i].Report)
		d.Analyses[i].CauseAssessment = &assessment
	}
	if len(d.Analyses) > 0 {
		d.Run.Analysis = &d.Analyses[0]
	}
	var pending int64
	if err = s.repo.db.WithContext(ctx).Model(&Task{}).Where("tenant_id = ? AND run_id = ? AND status IN ?", tenant, id, []string{"pending", "running"}).Count(&pending).Error; err != nil {
		return Detail{}, err
	}
	d.PendingTasks = int(pending)
	d.Reviews = []Review{}
	err = s.repo.db.WithContext(ctx).Where("tenant_id = ? AND run_id = ?", tenant, id).Order("created_at DESC,id").Find(&d.Reviews).Error
	if err == nil {
		populateCurrentWork(&d)
	}
	return d, err
}
func (s *Service) RunOnce(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
	task, err := s.repo.Claim(ctx, time.Now().UTC(), 5*time.Minute)
	if err != nil || task == nil {
		return err
	}
	run, err := s.repo.Run(ctx, task.TenantID, task.RunID)
	if err != nil {
		return err
	}
	a := Analysis{Status: "failed", Route: "technical_followup", RouteReason: "诊断失败，需技术排查", Report: Report{Verdict: "unverifiable", SourceCheck: "unresolved", Risk: "unknown", Scope: "诊断未完成"}}
	settings, err := s.repo.Settings(ctx, task.TenantID)
	var evidence []Evidence
	if err == nil {
		evidence, err = s.repo.Evidence(ctx, task.TenantID, task.RunID)
	}
	// Each analysis gets fresh supplements; prior versions stay in the audit trail.
	evidence = slices.DeleteFunc(evidence, func(e Evidence) bool { return e.PostHoc })
	execCtx := ctx
	if err == nil && s.executionContext != nil {
		execCtx, err = s.executionContext(ctx, run)
	}
	if err == nil && s.access != nil {
		err = s.access(execCtx, run, evidence)
	} else if err == nil {
		err = ErrForbidden
	}
	if err == nil && s.analyzer == nil {
		err = fmt.Errorf("analysis service unavailable")
	}
	var sourceAudit *SourceAudit
	if err == nil && s.analyzer.sources != nil {
		sourceCtx, cancel := context.WithTimeout(execCtx, sourceLookupTimeout)
		supplement, sourceErr := s.analyzer.sources(sourceCtx, run, evidence)
		cancel()
		sourceAudit = sourceAuditForAttempt(sourceErr)
		if sourceAudit.CharLimit == 0 {
			sourceAudit.CharLimit = s.analyzer.sourcePolicy.MaxChars
		}
		policy := s.analyzer.sourcePolicy
		policy.MaxChars = sourceAudit.CharLimit
		sourceAudit.LookupPolicy = &policy
		sourceAudit.SelectionStrategy = sourceSelectionStrategy
		for _, gap := range run.Gaps {
			if gap == "wiki_linked_summary_versions_unknown" || gap == "source_version_unknown" {
				sourceAudit.Issues = append(sourceAudit.Issues, "source_linked_versions_unknown")
				break
			}
		}
		if sourceErr != nil {
			run.Gaps = append(run.Gaps, "source_lookup_incomplete")
		}
		for _, e := range supplement {
			e.ID = uuid.NewString()
			e.TenantID = run.TenantID
			e.RunID = run.ID
			e.PostHoc = true
			e.SentToModel = false
			e.AcquiredAt = time.Now().UTC()
			h := sha256.Sum256([]byte(e.Content))
			e.Hash = hex.EncodeToString(h[:])
			if saveErr := s.repo.db.WithContext(ctx).Create(&e).Error; saveErr != nil {
				err = saveErr
				break
			}
			evidence = append(evidence, e)
			sourceAudit.EvidenceIDs = append(sourceAudit.EvidenceIDs, e.ID)
			sourceAudit.Chars += len([]rune(e.Content))
		}
	}
	if err == nil {
		a, err = s.analyzer.Analyze(execCtx, run, evidence, settings)
	}
	if err != nil {
		a.Status = "failed"
		a.Route = "technical_followup"
		a.RouteReason = "诊断执行失败，未形成有效结论"
		a.Error = analysisErrorCode(err)
		a.Report = Report{Verdict: "unverifiable", SourceCheck: "unresolved", Risk: "unknown", Scope: "诊断未完成"}
		if highRisk(run, a.Report) {
			a.Route = "human_review"
			a.RouteReason = "高风险业务结论须人工复核；诊断执行失败同时需技术排查"
			a.Report.Risk = "high"
		}
		logger.Warnf(ctx, "[KnowledgeOps] analysis failed run=%s: %v", run.ID, err)
	}
	a.Report.SourceAudit = sourceAudit
	a.ModelID = settings.ModelID
	return s.repo.SaveAnalysis(ctx, *task, a)
}
func (s *Service) StartWorker() {
	s.workerMu.Lock()
	defer s.workerMu.Unlock()
	if s.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.done = make(chan struct{})
	go func() {
		defer close(s.done)
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.repo.RecoverInterrupted(ctx, time.Now().UTC()); err != nil && ctx.Err() == nil {
					logger.Warnf(ctx, "[KnowledgeOps] recovery: %v", err)
				}
				if err := s.RunOnce(ctx); err != nil && ctx.Err() == nil {
					logger.Warnf(ctx, "[KnowledgeOps] worker: %v", err)
				}
			}
		}
	}()
}
func (s *Service) Stop() {
	s.workerMu.Lock()
	cancel, done := s.cancel, s.done
	s.workerMu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}
