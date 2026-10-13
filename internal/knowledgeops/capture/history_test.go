package capture

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/stretchr/testify/require"
)

func TestModelInputRecordsHistoryIdentityWithoutConfusingToolRounds(t *testing.T) {
	r := &memoryRecorder{}
	ctx := Begin(context.Background(), &memorySink{enabled: true, rec: r}, Start{})
	RecordModelInput(ctx, []chat.Message{
		{Role: "user", Content: "old question", TurnID: "a"},
		{Role: "assistant", Content: "old answer", TurnID: "a"},
		{Role: "tool", Name: "search_knowledge", Content: "old tool", TurnID: "a"},
		{Role: "user", Content: "summary", Kind: chat.MessageKindCompactionSummary},
		{Role: "user", Content: "current"},
		{Role: "assistant", Content: "current tool invocation"},
	}, nil)
	body, err := json.Marshal(r.inputs[0])
	require.NoError(t, err)
	var input map[string]any
	require.NoError(t, json.Unmarshal(body, &input))
	history, ok := input["history"].(map[string]any)
	require.True(t, ok, "history metadata must survive capture")
	require.EqualValues(t, 1, history["turns"])
	require.Equal(t, []any{"a"}, history["turn_ids"])
	require.Equal(t, true, history["compacted"])
	require.NotContains(t, history, "content", "history text must not be copied without source authorization")
}
