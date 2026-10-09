package knowledgeops

import (
	"context"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/stretchr/testify/require"
)

func TestAnalysisAcceptsPresentationWrappersWithoutRelaxingValidation(t *testing.T) {
	valid := `{"verdict":"supported","claims":[{"text":"A {with braces}","position":"answer","verdict":"supported","evidence_ids":["e"],"reason":"observed"}],"source_check":"not_checked","risk":"low","scope":"captured"}`
	for _, tc := range []struct {
		name, response, errorCode string
	}{
		{"plain", valid, ""},
		{"json fence", "```json\n" + valid + "\n```", ""},
		{"plain fence", "```\n" + valid + "\n```", ""},
		{"prose and fence", "诊断结果如下：\n```json\n" + valid + "\n```\n以上仅针对已采集证据。", ""},
		{"prose", "诊断结果：\n" + valid + "\n以上为本次诊断。", ""},
		{"truncated", "```json\n" + strings.TrimSuffix(valid, "}"), "invalid_analysis_json"},
		{"two objects", valid + "\n" + valid, "invalid_analysis_json"},
		{"incomplete second object", valid + "\n{", "invalid_analysis_json"},
		{"trailing scalar", valid + "\nfalse", "invalid_analysis_json"},
		{"array", "[" + valid + "]", "invalid_analysis_json"},
		{"unknown field", strings.TrimSuffix(valid, "}") + `,"invented":true}`, "invalid_analysis_json"},
		{"unknown evidence", "```json\n" + strings.Replace(valid, `"e"`, `"not-seen"`, 1) + "\n```", "invalid_analysis_evidence"},
		{"no report", "不能生成诊断", "invalid_analysis_json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := &fakeChat{payload: tc.response}
			a := NewAnalyzer(func(context.Context, string) (chat.Chat, error) { return model, nil }, nil)
			out, err := a.Analyze(context.Background(), Run{Status: "completed", CaptureStatus: "complete"}, []Evidence{{ID: "e", Content: "A {with braces}", SentToModel: true}}, DefaultSettings(1))
			if tc.errorCode != "" {
				require.Error(t, err)
				require.Equal(t, tc.errorCode, analysisErrorCode(err))
				return
			}
			require.NoError(t, err)
			require.Equal(t, "succeeded", out.Status)
			require.Equal(t, "supported", out.Report.Verdict)
			require.Equal(t, "A {with braces}", out.Report.Claims[0].Text)
		})
	}
}
