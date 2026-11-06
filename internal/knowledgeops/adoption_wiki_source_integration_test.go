package knowledgeops

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// An intentional independent human page must not become a recurring missing-
// original task, and its approval must never become original-source consistency.
func TestWikiAdoptionSourceIndependentPageHasAccurateScope(t *testing.T) {
	svc, _, ctx, run, input := wikiAdoptionIntegrationFixture(t)
	published, err := svc.Adopt(ctx, 1, run.ID, input)
	require.NoError(t, err)
	evidence := wikiAdoptionCapturedEvidence(published)
	supplement, sourceErr := (&RuntimeAdapter{DB: svc.repo.db}).Sources(ctx, run, evidence)
	require.Empty(t, supplement)
	audit := sourceAuditForAttempt(sourceErr)
	require.Empty(t, audit.Issues)
	require.Equal(t, "human_reviewed", wikiAdoptionAuditJSON(t, audit)["comparison_scope"])
	require.Equal(t, []any{published.WikiPageID}, wikiAdoptionAuditJSON(t, audit)["human_page_ids"])
	run.QAType, run.Channel, run.CaptureStatus = "wiki-qa", "web", "complete"
	report := Report{Verdict: "supported", Risk: "low", SourceCheck: "not_checked", SourceAudit: audit}
	route, _ := Triage(run, report, DefaultSettings(1))
	require.Equal(t, "human_review", route, "normal answer review remains required when automatic recording is disabled")
	run.Analysis = &Analysis{Status: "succeeded", Report: report}
	require.False(t, sourceFollowupPending(run))
	require.Equal(t, "human_reviewed", sourceCheckDisplay(Detail{Run: run, Evidence: evidence}))
	require.Equal(t, "not_checked", run.Analysis.Report.SourceCheck)
}

// Metadata is user-editable. Only the durable succeeded publication and exact
// current/captured version can attest independent human content.
func TestWikiAdoptionSourceRejectsForgedOrChangedLineage(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Service, Adoption, []Evidence)
	}{
		{"unpublished_snapshot", func(s *Service, p Adoption, _ []Evidence) {
			require.NoError(t, s.repo.db.Model(&Adoption{}).Where("id = ?", p.ID).Update("status", "failed").Error)
		}},
		{"forged_metadata", func(s *Service, p Adoption, _ []Evidence) {
			require.NoError(t, s.repo.db.Model(&types.WikiPage{}).Where("id = ?", p.WikiPageID).Update("page_metadata", types.JSON(`{"kind":"knowledge_ops_adoption","adoption_id":"missing"}`)).Error)
		}},
		{"changed_body", func(s *Service, p Adoption, _ []Evidence) {
			require.NoError(t, s.repo.db.Model(&types.WikiPage{}).Where("id = ?", p.WikiPageID).Update("content", "externally changed answer").Error)
		}},
		{"changed_title", func(s *Service, p Adoption, _ []Evidence) {
			require.NoError(t, s.repo.db.Model(&types.WikiPage{}).Where("id = ?", p.WikiPageID).Update("title", "externally changed question").Error)
		}},
		{"same_body_later_version", func(s *Service, p Adoption, e []Evidence) {
			require.NoError(t, s.repo.db.Model(&types.WikiPage{}).Where("id = ?", p.WikiPageID).Update("version", 2).Error)
			e[0].Version = "2"
		}},
		{"captured_version_drift", func(s *Service, p Adoption, _ []Evidence) {
			require.NoError(t, s.repo.db.Model(&types.WikiPage{}).Where("id = ?", p.WikiPageID).Update("version", 2).Error)
		}},
		{"unknown_captured_version", func(_ *Service, _ Adoption, e []Evidence) { e[0].Version = "" }},
		{"wrong_publication_target", func(s *Service, p Adoption, _ []Evidence) {
			require.NoError(t, s.repo.db.Model(&Adoption{}).Where("id = ?", p.ID).Update("kb_id", "another-kb").Error)
		}},
		{"wrong_publication_tenant", func(s *Service, p Adoption, _ []Evidence) {
			require.NoError(t, s.repo.db.Model(&Adoption{}).Where("id = ?", p.ID).Update("target_tenant_id", 2).Error)
		}},
		{"wrong_publication_page", func(s *Service, p Adoption, _ []Evidence) {
			require.NoError(t, s.repo.db.Model(&Adoption{}).Where("id = ?", p.ID).Update("wiki_page_id", "different-page").Error)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, ctx, run, input := wikiAdoptionIntegrationFixture(t)
			published, err := svc.Adopt(ctx, 1, run.ID, input)
			require.NoError(t, err)
			evidence := wikiAdoptionCapturedEvidence(published)
			tc.mutate(svc, published, evidence)
			_, sourceErr := (&RuntimeAdapter{DB: svc.repo.db}).Sources(ctx, run, evidence)
			audit := sourceAuditForAttempt(sourceErr)
			require.NotEmpty(t, audit.Issues)
			require.NotEqual(t, "human_reviewed", wikiAdoptionAuditJSON(t, audit)["comparison_scope"])
		})
	}
}

// The model must not manufacture a trusted scope even when no runtime adapter
// is configured. Otherwise it can bypass source work by writing audit JSON.
func TestWikiAdoptionSourceDiscardsModelAuthoredAudit(t *testing.T) {
	model := &fakeChat{payload: `{"verdict":"supported","risk":"low","source_check":"not_checked","scope":"captured answer","source_audit":{"comparison_scope":"human_reviewed","human_page_ids":["forged"],"issues":[]},"claims":[{"text":"A","position":"answer","verdict":"supported","reason":"captured","evidence_ids":["captured"]}]}`}
	analyzer := NewAnalyzer(func(context.Context, string) (chat.Chat, error) { return model, nil }, nil)
	run := Run{Status: "completed", QAType: "wiki-qa", Channel: "web", CaptureStatus: "complete"}
	result, err := analyzer.Analyze(context.Background(), run, []Evidence{{ID: "captured", Kind: "wiki", SentToModel: true, Content: "A"}}, DefaultSettings(1))
	require.NoError(t, err)
	require.Nil(t, result.Report.SourceAudit)
	require.Equal(t, "source_verification", result.Route)
}

// An independent-page scope only exempts the missing-original fallback. All
// real limitations and answer-quality routes must still take precedence.
func TestWikiAdoptionSourceScopeDoesNotSuppressRealProblems(t *testing.T) {
	svc, _, ctx, original, input := wikiAdoptionIntegrationFixture(t)
	published, err := svc.Adopt(ctx, 1, original.ID, input)
	require.NoError(t, err)
	_, sourceErr := (&RuntimeAdapter{DB: svc.repo.db}).Sources(ctx, original, wikiAdoptionCapturedEvidence(published))
	audit := sourceAuditForAttempt(sourceErr)
	for _, tc := range []struct {
		name, source, risk, want string
		gaps, unknowns           []string
	}{
		{name: "version_gap", source: "not_checked", risk: "low", gaps: []string{"source_version_unknown"}, want: "source_verification"},
		{name: "lookup_gap", source: "not_checked", risk: "low", gaps: []string{"source_lookup_incomplete"}, want: "source_verification"},
		{name: "unresolved", source: "unresolved", risk: "low", want: "source_verification"},
		{name: "conflict", source: "inconsistent", risk: "low", want: "human_review"},
		{name: "risk", source: "not_checked", risk: "high", want: "human_review"},
		{name: "uncertainty", source: "not_checked", risk: "low", unknowns: []string{"effective date remains uncertain"}, want: "needs_evidence"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run := Run{Status: "completed", QAType: "wiki-qa", Channel: "web", CaptureStatus: "complete", Gaps: tc.gaps}
			report := Report{Verdict: "supported", Risk: tc.risk, SourceCheck: tc.source, SourceAudit: audit, Unknowns: tc.unknowns}
			route, _ := Triage(run, report, DefaultSettings(1))
			require.Equal(t, tc.want, route)
			if len(tc.gaps) > 0 || tc.source == "unresolved" {
				run.Analysis = &Analysis{Status: "succeeded", Report: report}
				require.True(t, sourceFollowupPending(run))
				require.NotEqual(t, "human_reviewed", sourceCheckDisplay(Detail{Run: run}))
			}
		})
	}
}

// Independent content cannot exempt a second Wiki page from its real source
// comparison. The original document is still returned as post-hoc evidence.
func TestWikiAdoptionSourceMixedPagesStillReadOriginalDocuments(t *testing.T) {
	svc, wiki, ctx, run, input := wikiAdoptionIntegrationFixture(t)
	published, err := svc.Adopt(ctx, 1, run.ID, input)
	require.NoError(t, err)
	for _, statement := range []string{
		`CREATE TABLE knowledges (id TEXT PRIMARY KEY, knowledge_base_id TEXT, tenant_id INTEGER, title TEXT, deleted_at DATETIME)`,
		`INSERT INTO knowledges(id,knowledge_base_id,tenant_id,title) VALUES ('original-doc','wiki',1,'Original document')`,
		`ALTER TABLE chunks ADD COLUMN content TEXT`,
		`ALTER TABLE chunks ADD COLUMN content_revision INTEGER`,
		`ALTER TABLE chunks ADD COLUMN chunk_index INTEGER`,
		`INSERT INTO chunks(id,knowledge_id,knowledge_base_id,tenant_id,chunk_type,is_enabled,content,content_revision,chunk_index) VALUES ('original-chunk','original-doc','wiki',1,'text',true,'Original source statement',1,0)`,
	} {
		require.NoError(t, svc.repo.db.Exec(statement).Error)
	}
	original, err := wiki.CreatePage(ctx, &types.WikiPage{TenantID: 1, KnowledgeBaseID: "wiki", Slug: "summary/original-doc", Title: "Original summary", Content: "Summary statement", PageType: types.WikiPageTypeSummary, SourceRefs: types.StringArray{"original-doc|Original document"}})
	require.NoError(t, err)
	evidence := append(wikiAdoptionCapturedEvidence(published), Evidence{ID: "original-page", Kind: "wiki", KBID: "wiki", PageID: original.ID, Version: "1", Content: "Summary statement", SentToModel: true})
	supplement, sourceErr := (&RuntimeAdapter{DB: svc.repo.db}).Sources(ctx, run, evidence)
	require.False(t, sourceReadIncomplete(sourceErr))
	require.Len(t, supplement, 1)
	require.Equal(t, "Original source statement", supplement[0].Content)
	require.Equal(t, "original-doc", supplement[0].KnowledgeID)
	require.True(t, supplement[0].PostHoc)
	audit := sourceAuditForAttempt(sourceErr)
	require.Equal(t, "mixed", audit.ComparisonScope)
	report := Report{Verdict: "supported", Risk: "low", SourceCheck: "not_checked", SourceAudit: audit}
	run.QAType, run.Channel, run.CaptureStatus = "wiki-qa", "web", "complete"
	route, _ := Triage(run, report, DefaultSettings(1))
	require.Equal(t, "source_verification", route)

	// Mixed successful scope metadata must not swallow a later real source
	// failure; legacy consumers can still unwrap the SourceReadError.
	require.NoError(t, svc.repo.db.Exec("DELETE FROM chunks WHERE id = ?", "original-chunk").Error)
	_, sourceErr = (&RuntimeAdapter{DB: svc.repo.db}).Sources(ctx, run, evidence)
	require.True(t, sourceReadIncomplete(sourceErr))
	var readErr *SourceReadError
	require.ErrorAs(t, sourceErr, &readErr)
	require.Contains(t, readErr.Issues, "source_chunks_missing")
	require.Equal(t, "mixed", sourceAuditForAttempt(sourceErr).ComparisonScope)
}

// The worker must carry its attestation before model validation and triage,
// rather than merely attaching a badge after an incorrect source-work route.
func TestWikiAdoptionSourceWorkerPreservesTrustedScopeBeforeTriage(t *testing.T) {
	svc, _, ctx, run, input := wikiAdoptionIntegrationFixture(t)
	published, err := svc.Adopt(ctx, 1, run.ID, input)
	require.NoError(t, err)
	evidence := wikiAdoptionCapturedEvidence(published)
	later := Run{ID: uuid.NewString(), TenantID: 1, ActorID: "web_user:publisher", MessageID: uuid.NewString(), SessionID: "later", Status: "completed", QAType: "wiki-qa", Channel: "web", CaptureStatus: "complete", Question: integrationWikiQuestion, Answer: integrationWikiAnswer, StartedAt: time.Now().UTC()}
	require.NoError(t, svc.repo.CreateRun(ctx, &later))
	evidence[0].TenantID, evidence[0].RunID = 1, later.ID
	require.NoError(t, svc.repo.db.Create(&evidence[0]).Error)
	settings := DefaultSettings(1)
	settings.Enabled, settings.ModelID = true, "fixture"
	require.NoError(t, svc.repo.SaveSettings(ctx, 1, settings))
	require.NoError(t, svc.repo.Enqueue(ctx, later, 1))
	model := &fakeChat{payload: `{"verdict":"supported","risk":"low","source_check":"not_checked","scope":"captured answer","source_audit":{"issues":["source_lookup_failed"]},"claims":[{"text":"A","position":"answer","verdict":"supported","reason":"captured","evidence_ids":["captured-page"]}]}`}
	svc.analyzer = NewAnalyzer(func(context.Context, string) (chat.Chat, error) { return model, nil }, (&RuntimeAdapter{DB: svc.repo.db}).Sources)
	require.NoError(t, svc.RunOnce(ctx))
	analyses, err := svc.repo.Analyses(ctx, 1, later.ID)
	require.NoError(t, err)
	require.Len(t, analyses, 1)
	analysis := analyses[0]
	require.Equal(t, "succeeded", analysis.Status, analysis.Error)
	require.Equal(t, "human_review", analysis.Route)
	require.Equal(t, "not_checked", analysis.Report.SourceCheck)
	require.Empty(t, analysis.Report.SourceAudit.Issues)
	require.Equal(t, "human_reviewed", wikiAdoptionAuditJSON(t, analysis.Report.SourceAudit)["comparison_scope"])
	later.Analysis = &analysis
	require.False(t, sourceFollowupPending(later))
	require.Equal(t, "human_reviewed", sourceCheckDisplay(Detail{Run: later, Evidence: evidence}))
	stored, err := svc.repo.Evidence(ctx, 1, later.ID)
	require.NoError(t, err)
	require.Len(t, stored, 1, "no invented post-hoc source is created")
}

// The origin may commit its page and lose its request before recording success.
// An exact successful reuse is still a valid attestation of that same page.
func TestWikiAdoptionSourceSuccessfulReuseAttestsInterruptedOrigin(t *testing.T) {
	svc, wiki, ctx, run, input := wikiAdoptionIntegrationFixture(t)
	firstCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	svc.adoption.Wiki = wikiAdoptionCancelAfterCommit{writer: wiki, cancel: cancel}
	first, err := svc.Adopt(firstCtx, 1, run.ID, input)
	svc.adoption.Wiki = wiki
	require.NoError(t, err)
	require.Equal(t, "failed", first.Status)
	require.Equal(t, "wiki_lookup_failed", first.Error)
	var originSnapshot Adoption
	require.NoError(t, svc.repo.db.First(&originSnapshot, "id = ?", first.ID).Error)
	page, err := wiki.GetPageByID(ctx, wikiAdoptionPageID(first.ID))
	require.NoError(t, err)
	evidence := wikiAdoptionCapturedEvidence(Adoption{WikiPageID: page.ID})
	_, sourceErr := (&RuntimeAdapter{DB: svc.repo.db}).Sources(ctx, run, evidence)
	require.Contains(t, sourceAuditForAttempt(sourceErr).Issues, "source_linkage_missing", "a failed origin alone cannot attest publication")

	var analysis Analysis
	var review Review
	require.NoError(t, svc.repo.db.First(&analysis, "id = ?", input.AnalysisID).Error)
	require.NoError(t, svc.repo.db.First(&review, "id = ?", input.ReviewID).Error)
	other, otherAnalysis, otherReview := cloneWikiAdoptionSource(t, svc, run, analysis, review)
	input.AnalysisID, input.ReviewID = otherAnalysis.ID, otherReview.ID
	second, err := svc.Adopt(ctx, 1, other.ID, input)
	require.NoError(t, err)
	require.Equal(t, "succeeded", second.Status)
	require.True(t, second.Reused)
	require.Equal(t, page.ID, second.WikiPageID)
	thirdRun, thirdAnalysis, thirdReview := cloneWikiAdoptionSource(t, svc, run, analysis, review)
	input.AnalysisID, input.ReviewID = thirdAnalysis.ID, thirdReview.ID
	third, err := svc.Adopt(ctx, 1, thirdRun.ID, input)
	require.NoError(t, err)
	require.Equal(t, "succeeded", third.Status)
	require.True(t, third.Reused)
	require.Equal(t, page.ID, third.WikiPageID)
	_, sourceErr = (&RuntimeAdapter{DB: svc.repo.db}).Sources(ctx, other, evidence)
	audit := sourceAuditForAttempt(sourceErr)
	require.Empty(t, audit.Issues, "successful exact reuse must not become a missing-original task")
	require.Equal(t, "human_reviewed", audit.ComparisonScope)
	other.QAType, other.Channel, other.CaptureStatus = "wiki-qa", "web", "complete"
	other.Analysis = &Analysis{Status: "succeeded", Report: Report{SourceCheck: "not_checked", SourceAudit: audit}}
	require.False(t, sourceFollowupPending(other))
	require.Equal(t, "human_reviewed", sourceCheckDisplay(Detail{Run: other, Evidence: evidence}))

	// Reuse does not rewrite the interrupted snapshot or the original page.
	var storedFirst Adoption
	require.NoError(t, svc.repo.db.First(&storedFirst, "id = ?", first.ID).Error)
	require.Equal(t, originSnapshot, storedFirst)
	storedPage, err := wiki.GetPageByID(ctx, page.ID)
	require.NoError(t, err)
	require.Equal(t, page, storedPage)

	for _, tc := range []struct {
		name          string
		model         any
		id, column    string
		bad, original any
	}{
		{"metadata_cannot_claim_reuser_identity", &types.WikiPage{}, page.ID, "page_metadata", types.JSON(`{"kind":"knowledge_ops_adoption","adoption_id":"` + second.ID + `"}`), page.PageMetadata},
		{"metadata_cannot_claim_missing_origin", &types.WikiPage{}, page.ID, "page_metadata", types.JSON(`{"kind":"knowledge_ops_adoption","adoption_id":"missing"}`), page.PageMetadata},
		{"origin_must_match_target", &Adoption{}, first.ID, "kb_id", "another-kb", first.KBID},
		{"origin_must_match_answer", &Adoption{}, first.ID, "answer", "different origin answer", first.Answer},
		{"success_must_match_current_version", &types.WikiPage{}, page.ID, "version", 2, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, svc.repo.db.Model(tc.model).Where("id = ?", tc.id).UpdateColumn(tc.column, tc.bad).Error)
			t.Cleanup(func() {
				require.NoError(t, svc.repo.db.Model(tc.model).Where("id = ?", tc.id).UpdateColumn(tc.column, tc.original).Error)
			})
			_, sourceErr := (&RuntimeAdapter{DB: svc.repo.db}).Sources(ctx, other, evidence)
			audit := sourceAuditForAttempt(sourceErr)
			require.NotEmpty(t, audit.Issues)
			require.NotEqual(t, "human_reviewed", audit.ComparisonScope)
		})
	}
}

type wikiAdoptionCancelAfterCommit struct {
	writer WikiAdoptionService
	cancel context.CancelFunc
}

func (w wikiAdoptionCancelAfterCommit) CreatePage(ctx context.Context, page *types.WikiPage) (*types.WikiPage, error) {
	created, err := w.writer.CreatePage(ctx, page)
	if err == nil {
		w.cancel()
	}
	return created, err
}

func wikiAdoptionCapturedEvidence(p Adoption) []Evidence {
	return []Evidence{{ID: "captured-page", Kind: "wiki", KBID: "wiki", PageID: p.WikiPageID, Version: "1", Content: integrationWikiAnswer, SentToModel: true}}
}

func wikiAdoptionAuditJSON(t *testing.T, audit *SourceAudit) map[string]any {
	t.Helper()
	data, err := json.Marshal(audit)
	require.NoError(t, err)
	var result map[string]any
	require.NoError(t, json.Unmarshal(data, &result))
	return result
}
