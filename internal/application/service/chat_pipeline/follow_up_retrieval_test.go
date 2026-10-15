package chatpipeline

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestFollowUpRequiresRetrievalUnlessExplicitlyLimitedToAvailableHistory(t *testing.T) {
	for _, tc := range []struct {
		name, output           string
		history, wantRetrieval bool
	}{
		{"expand old model output", `{"rewrite_query":"详细展开第三个方向","intent":"follow_up"}`, true, true},
		{"new facts", `{"rewrite_query":"第三个方向有什么条件","intent":"follow_up","needs_retrieval":true}`, true, true},
		{"context-only recap", `{"rewrite_query":"复述第三点","intent":"follow_up","needs_retrieval":false}`, true, false},
		{"recap without history", `{"rewrite_query":"复述第三点","intent":"follow_up","needs_retrieval":false}`, false, true},
		{"malformed opt-out", `{"rewrite_query":"详细展开第三点","intent":"follow_up","needs_retrieval":"false"}`, true, true},
		{"missing rewrite", `{"intent":"follow_up","needs_retrieval":false}`, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cm := &types.ChatManage{PipelineState: types.PipelineState{Intent: types.IntentKBSearch}}
			if tc.history {
				cm.History = []*types.History{{Query: "有哪些成果", Answer: "第三点是销售辅助"}}
			}
			(&PluginQueryUnderstand{}).parseOutput(cm, tc.output)
			require.Equal(t, tc.wantRetrieval, cm.NeedsRetrieval())
		})
	}
}
