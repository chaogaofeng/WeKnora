package knowledgeops

import (
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/knowledgeops/capture"
	"github.com/stretchr/testify/require"
)

func TestCoverageDistinguishesSkippedSourcesFromCaptureLoss(t *testing.T) {
	var run Run
	require.NoError(t, json.Unmarshal([]byte(`{"status":"completed","capture_status":"missing","gaps":["no_visible_source","history_source_permissions_unbound"],"query_context":{"version":1,"retrieval":{"kind":"retrieval","status":"skipped","intent":"follow_up"}}}`), &run))
	for _, tc := range []struct {
		name         string
		loss, legacy bool
		want         string
	}{
		{"explicit skip", false, false, "not_provided"},
		{"capture failure", true, false, ""},
		{"old metadata absent", false, true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current := run
			if tc.loss {
				current.Gaps = append(append([]string{}, run.Gaps...), "evidence_persist_failed")
			}
			if tc.legacy {
				current.QueryContext = nil
			}
			coverage := captureCoverage(current, nil)
			body, _ := json.Marshal(coverage)
			var public struct {
				ContentReason string `json:"content_reason"`
			}
			require.NoError(t, json.Unmarshal(body, &public))
			require.Equal(t, tc.want, public.ContentReason)
			require.Equal(t, "missing", coverage.ContentStatus, "absence of sources never counts as complete evidence")
		})
	}
}

func TestBoundHistoryDoesNotDowngradeCapturedKnowledgeBodies(t *testing.T) {
	run := Run{Status: "completed", QAType: "rag-qa", Channel: "web", CaptureStatus: "partial", Gaps: []string{"history_source_permissions_unbound"},
		HistoryContext:  &capture.HistorySnapshot{HistoryInput: capture.HistoryInput{Turns: 1, IDsKnown: true, TurnIDs: []string{"old"}}},
		AnalysisHistory: &AnalysisHistory{Status: "complete", Turns: []AnalysisHistoryTurn{{TurnID: "old", Answer: "past"}}},
	}
	coverage := captureCoverage(run, []Evidence{{SentToModel: true, Kind: "chunk", Content: "body", Version: "1"}})
	require.Equal(t, "complete", coverage.ContentStatus)
	report := Report{Verdict: "supported", Risk: "low", Applicability: "knowledge", SourceCheck: "not_checked"}
	applySupportPolicy(run, &report)
	require.Equal(t, "supported", report.Verdict)
	route, _ := Triage(run, report, DefaultSettings(1))
	require.NotEqual(t, "needs_evidence", route, "authorized conversation context is not a missing source body")
	require.Equal(t, "partial", run.CaptureStatus, "stored capture audit is immutable")
	require.Equal(t, []string{"history_source_permissions_unbound"}, run.Gaps)
	for _, gaps := range [][]string{nil, {"capture_budget_exhausted"}, {"history_source_permissions_unbound", "capture_budget_exhausted"}} {
		limited := run
		limited.Gaps = gaps
		require.Equal(t, "partial", withResolvedHistoryCapture(limited).CaptureStatus, "only the explicitly resolved history gap can be removed")
	}
	run.AnalysisHistory = &AnalysisHistory{Status: "partial"}
	applySupportPolicy(run, &report)
	require.Equal(t, "partial", report.Verdict, "unavailable history must remain a limit on the overall judgment")
}

func TestCaptureCoverageSeparatesVersionsAcrossDetailListAndOverview(t *testing.T) {
	_, repo, engine := workbenchFixture(t)
	cases := []struct {
		id, raw, content, provenance string
		gaps                         []string
	}{
		{"known", "complete", "complete", "known", []string{}},
		{"rag-version", "partial", "complete", "pending", []string{"source_version_unknown"}},
		{"wiki-version", "partial", "complete", "pending", []string{"wiki_linked_summary_versions_unknown"}},
		{"mixed", "partial", "partial", "pending", []string{"wiki_linked_summary_versions_unknown", "capture_budget_exhausted"}},
		{"body-lost", "partial", "partial", "unknown", []string{"evidence_persist_failed"}},
		{"missing", "missing", "missing", "pending", []string{"source_version_unknown", "no_visible_source"}},
		{"unknown", "unknown", "unknown", "unknown", []string{}},
		{"unexplained", "partial", "partial", "unknown", []string{}},
		{"future-gap", "partial", "partial", "unknown", []string{"new_unrecognized_gap"}},
	}
	for _, tc := range cases {
		seedWorkbench(t, repo, tc.id, "low", "needs_evidence", "", 1)
		if tc.id == "known" {
			require.NoError(t, repo.db.Create(&Evidence{ID: "known-evidence", TenantID: 1, RunID: tc.id, Kind: "wiki", PageID: "page", KBID: "kb", Version: "1", Content: "captured", SentToModel: true}).Error)
		}
		gaps, err := json.Marshal(tc.gaps)
		require.NoError(t, err)
		require.NoError(t, repo.db.Model(&Run{}).Where("id = ?", tc.id).Updates(map[string]any{"capture_status": tc.raw, "gaps": string(gaps)}).Error)
		t.Run(tc.id, func(t *testing.T) {
			detail := workbenchGET(t, engine, "/runs/"+tc.id)
			run := detail["run"].(map[string]any)
			require.Equal(t, tc.raw, run["capture_status"], "raw capture audit must be preserved")
			coverage, ok := run["coverage"].(map[string]any)
			require.True(t, ok, "separate content and provenance assessment missing")
			require.Equal(t, tc.content, coverage["content_status"])
			require.Equal(t, tc.provenance, coverage["provenance_status"])
			if tc.id == "wiki-version" {
				require.Empty(t, coverage["content_gaps"])
				require.Equal(t, []any{"wiki_linked_summary_versions_unknown"}, coverage["provenance_gaps"])
			}
			list := workbenchGET(t, engine, "/runs?search=Question%20"+tc.id)
			require.EqualValues(t, 1, list["total"])
			require.Equal(t, coverage, list["items"].([]any)[0].(map[string]any)["coverage"])
		})
	}
	// Neither derived counts nor filters may reveal inaccessible records.
	seedWorkbench(t, repo, "private", "low", "needs_evidence", "", 1)
	overview := workbenchGET(t, engine, "/overview")
	require.EqualValues(t, 9, overview["total"])
	require.EqualValues(t, 1, overview["complete"], "legacy aggregate keeps its original meaning")
	wantContent := map[string]any{"total": float64(9), "complete": float64(3), "partial": float64(4), "missing": float64(2)}
	require.Equal(t, wantContent, overview["content_coverage"])
	require.Equal(t, wantContent, overview["content_coverage_by_mode"].(map[string]any)["wiki-qa"])
	require.Equal(t, map[string]any{"known": float64(1), "pending": float64(4), "unknown": float64(4)}, overview["provenance_counts"])
	for _, query := range []string{"content_status=complete", "content_status=complete&provenance_status=pending", "content_status=partial&provenance_status=pending"} {
		want := map[string]int{"content_status=complete": 3, "content_status=complete&provenance_status=pending": 2, "content_status=partial&provenance_status=pending": 1}[query]
		require.EqualValues(t, want, workbenchGET(t, engine, "/runs?"+query)["total"])
		require.EqualValues(t, want, workbenchGET(t, engine, "/overview?"+query)["total"])
		require.EqualValues(t, want, workbenchGET(t, engine, "/groups?"+query)["total"])
	}
}

func TestCaptureCoverageDoesNotInferRecordedVersionsFromPostHocOrAbsentEvidence(t *testing.T) {
	_, repo, engine := workbenchFixture(t)
	for _, tc := range []struct {
		id, want string
		evidence []Evidence
	}{
		{"no-evidence", "unknown", nil},
		{"post-hoc-only", "unknown", []Evidence{{ID: "post", Kind: "wiki", Version: "2", PostHoc: true}}},
		{"candidate-only", "unknown", []Evidence{{ID: "candidate", Kind: "wiki", Version: "1"}}},
		{"unversioned-visible", "pending", []Evidence{{ID: "visible", Kind: "web", Version: "unknown", Content: "captured", SentToModel: true}}},
	} {
		t.Run(tc.id, func(t *testing.T) {
			seedWorkbench(t, repo, tc.id, "low", "needs_evidence", "", 1)
			for _, e := range tc.evidence {
				e.RunID, e.TenantID = tc.id, 1
				require.NoError(t, repo.db.Create(&e).Error)
			}
			run := workbenchGET(t, engine, "/runs/"+tc.id)["run"].(map[string]any)
			coverage := run["coverage"].(map[string]any)
			require.Equal(t, "complete", coverage["content_status"], "source metadata does not imply content loss")
			require.Equal(t, tc.want, coverage["provenance_status"])
		})
	}
}
