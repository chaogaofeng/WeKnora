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

func TestSourceComparisonUsesAvailableEvidenceWithoutDiscardingClaims(t *testing.T) {
	for _, tc := range []struct {
		name, modelCheck, wantCheck string
		postHocContent              string
		incomplete                  bool
	}{
		{name: "RAG consistency without comparison", modelCheck: "consistent", wantCheck: "not_checked"},
		{name: "conflict without comparison", modelCheck: "inconsistent", wantCheck: "not_checked"},
		{name: "empty supplement", modelCheck: "consistent", wantCheck: "not_checked", postHocContent: " "},
		{name: "failed lookup", modelCheck: "consistent", wantCheck: "unresolved", incomplete: true},
		{name: "partial lookup", modelCheck: "consistent", wantCheck: "unresolved", postHocContent: "A", incomplete: true},
		{name: "available comparison", modelCheck: "consistent", wantCheck: "consistent", postHocContent: "A"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			report := Report{Verdict: "supported", Scope: "captured evidence", Risk: "low", SourceCheck: tc.modelCheck, Claims: []Claim{{Text: "A", Position: "answer", Verdict: "supported", EvidenceIDs: []string{"e"}, Reason: "supported by captured source"}}}
			body, err := json.Marshal(report)
			require.NoError(t, err)
			model := &fakeChat{payload: string(body)}
			analyzer := NewAnalyzer(func(context.Context, string) (chat.Chat, error) { return model, nil }, nil)
			run := Run{Status: "completed", CaptureStatus: "partial", QAType: "rag-qa", Channel: "web", Gaps: []string{"source_version_unknown"}}
			if tc.incomplete {
				run.Gaps = append(run.Gaps, "source_lookup_incomplete")
			}
			evidence := []Evidence{{ID: "e", Kind: "chunk", Content: "A", SentToModel: true}}
			if tc.postHocContent != "" {
				evidence = append(evidence, Evidence{ID: "later", Kind: "source_document", Content: tc.postHocContent, PostHoc: true})
			}
			result, err := analyzer.Analyze(context.Background(), run, evidence, DefaultSettings(1))
			require.NoError(t, err)
			require.Equal(t, "succeeded", result.Status)
			require.Equal(t, tc.wantCheck, result.Report.SourceCheck)
			require.Equal(t, report.Claims, result.Report.Claims)
			require.Equal(t, "supported", result.Report.Verdict)
			require.Equal(t, "needs_evidence", result.Route)
			require.Equal(t, "rag-qa/web/low/"+tc.wantCheck, result.SampleStratum)
			if tc.wantCheck != tc.modelCheck {
				require.Contains(t, result.Report.Unknowns[0], tc.modelCheck, "retain the model's original source judgment and correction reason")
			}
		})
	}
}

func TestSourceComparisonCorrectionDoesNotHideInvalidEvidenceOrEnums(t *testing.T) {
	for _, tc := range []struct{ name, sourceCheck, evidenceID, wantError string }{
		{"invalid citation", "consistent", "missing", "claim refers to unread or post-hoc evidence"},
		{"invalid source status", "invented", "e", "invalid source check"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			report := Report{Verdict: "supported", Scope: "captured evidence", Risk: "low", SourceCheck: tc.sourceCheck, Claims: []Claim{{Text: "A", Position: "answer", Verdict: "supported", EvidenceIDs: []string{tc.evidenceID}, Reason: "observed"}}}
			body, err := json.Marshal(report)
			require.NoError(t, err)
			model := &fakeChat{payload: string(body)}
			analyzer := NewAnalyzer(func(context.Context, string) (chat.Chat, error) { return model, nil }, nil)
			_, err = analyzer.Analyze(context.Background(), Run{Status: "completed", CaptureStatus: "complete", Gaps: []string{"source_lookup_incomplete"}}, []Evidence{{ID: "e", Content: "A", Kind: "chunk", SentToModel: true}}, DefaultSettings(1))
			require.ErrorContains(t, err, tc.wantError)
		})
	}
}

func TestDiagnosisFailurePersistsSafeActionableReason(t *testing.T) {
	for _, tc := range []struct {
		name, response, wantCode string
		callError                error
	}{
		{name: "JSON format", response: "not json", wantCode: "invalid_analysis_json"},
		{name: "schema", response: `{}`, wantCode: "invalid_analysis_report"},
		{name: "upstream", callError: errors.New("HTTP 401 secret-token and private response body"), wantCode: "analysis_model_failed"},
		{name: "timeout", callError: context.DeadlineExceeded, wantCode: "analysis_timeout"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo := enabledTestService(t)
			model := &fakeChat{payload: tc.response, err: tc.callError}
			svc.analyzer.models = func(context.Context, string) (chat.Chat, error) { return model, nil }
			ctx := context.Background()
			rec := svc.Start(ctx, capture.Start{TenantID: 1, MessageID: tc.name, StartedAt: time.Now()})
			require.NotNil(t, rec)
			rec.Finish(ctx, capture.Completion{Status: "completed", Answer: "A"})
			require.NoError(t, svc.RunOnce(ctx))
			var result Analysis
			require.NoError(t, repo.db.First(&result).Error)
			require.Equal(t, "failed", result.Status)
			require.Equal(t, tc.wantCode, result.Error)
			require.NotContains(t, result.Error, "secret-token")
		})
	}
}
