package knowledgeops

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/knowledgeops/capture"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/stretchr/testify/require"
)

func TestCompletedHumanReviewKeepsSourceFollowupOpen(t *testing.T) {
	for _, route := range []string{"needs_evidence", "human_review"} {
		d := Detail{Run: Run{Gaps: []string{"wiki_linked_summary_versions_unknown"}, Analysis: &Analysis{ID: "a", Status: "succeeded", Route: route, Report: Report{Verdict: "supported", SourceCheck: "unresolved"}}}, Reviews: []Review{{AnalysisID: "a", Target: "answer", Action: "confirmed"}}}
		require.Equal(t, "reviewed", reviewState(d))
		require.Contains(t, queueRoutes(d), "source_verification")
	}
	d := Detail{Run: Run{Analysis: &Analysis{ID: "a", Status: "succeeded", Report: Report{SourceCheck: "consistent", Unknowns: []string{"business decision pending"}, ReviewQuestions: []string{"approve amount?"}}}}, Reviews: []Review{{AnalysisID: "a", Target: "answer", Action: "confirmed"}}}
	require.Equal(t, "reviewed", reviewState(d))
	require.NotContains(t, queueRoutes(d), "source_verification")
}

func TestSourcePendingDoesNotReopenCompletedHumanReview(t *testing.T) {
	for _, route := range []string{"human_review", "quality_sample"} {
		t.Run(route, func(t *testing.T) {
			d := Detail{Run: Run{Analysis: &Analysis{ID: "a", Status: "succeeded", Route: route, Report: Report{Verdict: "supported", SourceCheck: "unresolved"}}}, Reviews: []Review{{AnalysisID: "a", Target: "answer", Action: "confirmed"}}}
			require.Equal(t, "reviewed", reviewState(d))
			require.Equal(t, []string{"source_verification"}, queueRoutes(d))
			stats := newWorkbenchStats()
			stats.observe(d)
			if route == "quality_sample" {
				require.Equal(t, 1, stats.SampleReviewed)
			}

			// Automatic causes and historical cause reviews cannot reopen answer review.
			d.Run.Analysis.Report.Causes = []Cause{{Category: "retrieval", Status: "suspected"}}
			d.Reviews = append(d.Reviews, Review{AnalysisID: "old", Target: "cause", Action: "confirmed"})
			require.Equal(t, "reviewed", reviewState(d))
			require.Equal(t, []string{"source_verification"}, queueRoutes(d))
			d.Reviews = append([]Review{{AnalysisID: "a", Target: "cause", Action: "confirmed"}}, d.Reviews...)
			require.Equal(t, "reviewed", reviewState(d))
			require.Equal(t, []string{"source_verification"}, queueRoutes(d))
			// A later request for evidence reopens the human task.
			d.Reviews = append([]Review{{AnalysisID: "a", Target: "answer", Action: "needs_more_evidence"}}, d.Reviews...)
			require.Equal(t, "needs_more_evidence", reviewState(d), "a new human evidence request reopens answer review")
			require.Contains(t, queueRoutes(d), route)
		})
	}
}

func TestFailedDiagnosisOnlyKeepsEstablishedSourceGaps(t *testing.T) {
	for _, mode := range []string{"rag-qa", "wiki-qa", "hybrid-rag-wiki"} {
		for _, gap := range []string{"none", "audit", "capture", "derived"} {
			t.Run(mode+"/"+gap, func(t *testing.T) {
				run := Run{QAType: mode, Status: "completed", CaptureStatus: "complete", Analysis: &Analysis{ID: "a", Status: "failed", Error: "invalid_analysis_json", Route: "technical_followup", Report: Report{Verdict: "unverifiable", SourceCheck: "unresolved", SourceAudit: &SourceAudit{Issues: []string{}}}}}
				switch gap {
				case "audit":
					run.Analysis.Report.SourceAudit.Issues = []string{"source_lookup_failed"}
				case "capture":
					run.Gaps = []string{"wiki_linked_summary_versions_unknown"}
				case "derived":
					run.Coverage.ProvenanceGaps = []string{"source_version_unknown"}
				}
				require.Equal(t, gap != "none", sourceFollowupPending(run))
				d := Detail{Run: run, Reviews: []Review{{AnalysisID: "a", Target: "answer", Action: "confirmed"}}}
				routes := queueRoutes(d)
				require.Contains(t, routes, "technical_followup", "human confirmation cannot hide diagnosis failures")
				if gap == "none" {
					require.NotContains(t, routes, "source_verification")
				} else {
					require.Contains(t, routes, "source_verification")
				}
			})
		}
	}
}

func TestSourceAuditFreshPerAttemptAndSurvivesFailure(t *testing.T) {
	svc, repo := enabledTestService(t)
	ctx := context.Background()
	svc.analyzer.sources = func(context.Context, Run, []Evidence) ([]Evidence, error) {
		return []Evidence{{Content: "正文", Kind: "source_document"}}, &SourceReadError{Issues: []string{"source_char_budget_exhausted"}, CharLimit: 16000}
	}
	rec := svc.Start(ctx, capture.Start{TenantID: 1, MessageID: "audit", StartedAt: time.Now()})
	rec.RecordSources(ctx, []capture.Source{{Kind: "chunk", Content: "回答", SentToModel: true}})
	rec.Finish(ctx, capture.Completion{Status: "completed", Answer: "回答"})
	for attempt := 0; attempt < 2; attempt++ {
		require.NoError(t, svc.RunOnce(ctx))
		var run Run
		require.NoError(t, repo.db.First(&run).Error)
		d, err := svc.Detail(ctx, 1, run.ID)
		require.NoError(t, err)
		audit := d.Analyses[0].Report.SourceAudit
		require.NotNil(t, audit)
		require.Len(t, audit.EvidenceIDs, 1)
		require.Equal(t, 2, audit.Chars)
		require.Equal(t, 16000, audit.CharLimit)
		require.NotNil(t, audit.LookupPolicy)
		require.Equal(t, audit.CharLimit, audit.LookupPolicy.MaxChars)
		require.NotEmpty(t, audit.SelectionStrategy)
		require.Contains(t, audit.Issues, "source_char_budget_exhausted")
		if attempt == 0 {
			require.NoError(t, repo.Retry(ctx, 1, run.ID))
			svc.analyzer.models = func(context.Context, string) (chat.Chat, error) { return &fakeChat{payload: "not json"}, nil }
		} else {
			require.Equal(t, "failed", d.Analyses[0].Status)
			require.NotEqual(t, d.Analyses[0].Report.SourceAudit.EvidenceIDs, d.Analyses[1].Report.SourceAudit.EvidenceIDs)
		}
	}
}

func TestAnalyzerIgnoresForgedSourceAudit(t *testing.T) {
	report := Report{Verdict: "supported", Scope: "body", Risk: "low", SourceCheck: "not_checked", SourceAudit: &SourceAudit{EvidenceIDs: []string{"forged"}, Chars: 999, CharLimit: 999}, Claims: []Claim{{Text: "A", Position: "answer", Verdict: "supported", EvidenceIDs: []string{"e"}, Reason: "body"}}}
	body, _ := json.Marshal(report)
	a := NewAnalyzer(func(context.Context, string) (chat.Chat, error) { return &fakeChat{payload: string(body)}, nil }, nil)
	result, err := a.Analyze(context.Background(), Run{QAType: "rag-qa", Channel: "web", CaptureStatus: "complete"}, []Evidence{{ID: "e", Content: "A", SentToModel: true, Kind: "chunk"}}, DefaultSettings(1))
	require.NoError(t, err)
	require.Nil(t, result.Report.SourceAudit)
}

func TestSourceReadAuditSanitizesUnstructuredAndUnknownErrors(t *testing.T) {
	for _, err := range []error{errors.New("secret upstream SQL"), &SourceReadError{Issues: []string{"private raw error"}}} {
		audit := sourceAuditForAttempt(err)
		require.Equal(t, []string{"source_lookup_failed"}, audit.Issues)
		require.Equal(t, 0, audit.CharLimit)
	}
}

func TestSourceAuditIsNotInferredForHistoricalReports(t *testing.T) {
	svc, repo := enabledTestService(t)
	ctx := context.Background()
	require.NoError(t, repo.CreateRun(ctx, &Run{ID: "historical", TenantID: 1, MessageID: "h"}))
	require.NoError(t, repo.db.Create(&Analysis{ID: "old", RunID: "historical", TenantID: 1, Version: 1, Report: Report{SourceCheck: "unresolved"}}).Error)
	require.NoError(t, repo.db.Create(&Evidence{ID: "archive", RunID: "historical", TenantID: 1, PostHoc: true, Content: "archive"}).Error)
	d, err := svc.Detail(ctx, 1, "historical")
	require.NoError(t, err)
	require.Nil(t, d.Analyses[0].Report.SourceAudit)
	require.True(t, d.Run.SourceFollowupPending)
	require.Len(t, d.Evidence, 1)
}

func TestMalformedModelSourceAuditCannotRejectOtherwiseValidReport(t *testing.T) {
	report, err := decodeAnalysisReport(`{"verdict":"unverifiable","scope":"body","risk":"low","source_check":"not_checked","source_audit":{"chars":"forged","unexpected":"secret"}}`)
	require.NoError(t, err)
	require.Nil(t, report.SourceAudit)
}

func TestDetailDerivedUnknownSourceVersionKeepsFollowupPending(t *testing.T) {
	for _, version := range []string{"", "unknown"} {
		t.Run("version="+version, func(t *testing.T) {
			svc, repo := enabledTestService(t)
			ctx := context.Background()
			require.NoError(t, repo.CreateRun(ctx, &Run{ID: "derived", TenantID: 1, MessageID: "derived", Status: "completed", CaptureStatus: "complete"}))
			a := Analysis{ID: "verified", RunID: "derived", TenantID: 1, Version: 1, Status: "succeeded", Route: "quality_sample", Report: Report{Verdict: "supported", SourceCheck: "consistent", SourceAudit: &SourceAudit{Issues: []string{}}}}
			require.NoError(t, repo.db.Create(&a).Error)
			require.NoError(t, repo.db.Create(&Evidence{ID: "visible", RunID: "derived", TenantID: 1, Kind: "chunk", Version: version, SentToModel: true, Content: "body"}).Error)
			require.NoError(t, repo.AddReview(ctx, 1, &Review{RunID: "derived", AnalysisID: "verified", Target: "answer", Verdict: "supported", Action: "confirmed", Reason: "confirmed answer", ReviewerID: "reviewer"}))
			d, err := svc.Detail(ctx, 1, "derived")
			require.NoError(t, err)
			require.Empty(t, d.Run.Gaps)
			require.Contains(t, d.Run.Coverage.ProvenanceGaps, "source_version_unknown")
			require.True(t, d.Run.SourceFollowupPending)
			require.Equal(t, "reviewed", reviewState(d))
			require.Contains(t, queueRoutes(d), "source_verification")
		})
	}
}
