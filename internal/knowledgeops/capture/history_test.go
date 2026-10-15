package capture

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
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

func TestRAGHistoryIdentityRequiresEveryRenderedTurn(t *testing.T) {
	for _, tc := range []struct {
		name  string
		ids   []string
		known bool
	}{
		{"bound", []string{"a", "b"}, true},
		{"legacy", []string{"", ""}, false},
		{"mixed", []string{"a", ""}, false},
		{"duplicate", []string{"a", "a"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &memoryRecorder{}
			var messages []chat.Message
			for _, id := range tc.ids {
				messages = append(messages, chat.Message{Role: "user", Content: "old", TurnID: id}, chat.Message{Role: "assistant", Content: "answer", TurnID: id})
			}
			messages = append(messages, chat.Message{Role: "user", Content: "current"})
			RecordRAGModelInput(WithRecorder(context.Background(), r), messages, nil, "model", len(tc.ids))
			require.Equal(t, tc.known, r.inputs[0].History.IDsKnown)
			require.Equal(t, len(tc.ids), r.inputs[0].History.Turns)
		})
	}
}

func TestHistorySupplementGapsDescribeActualModelInput(t *testing.T) {
	attachments := (types.MessageAttachments{{FileName: "scope.txt", Content: "limited to Shanghai"}}).BuildPrompt()
	for _, tc := range []struct {
		name         string
		message      chat.Message
		want, absent string
	}{
		{"historical attachment", chat.Message{Role: "user", TurnID: "old", Content: "Q" + attachments}, "history_attachment_context_omitted", "attachment_source_permissions_unbound"},
		{"historical caption", chat.Message{Role: "user", TurnID: "old", Content: "Q\n\n[用户上传图片内容]\nlimited to Shanghai"}, "history_image_context_omitted", "image_context_omitted"},
		{"historical multimodal text", chat.Message{Role: "user", TurnID: "old", MultiContent: []chat.MessageContentPart{{Type: "text", Text: "Q" + attachments}}}, "history_attachment_context_omitted", "attachment_source_permissions_unbound"},
		{"historical image", chat.Message{Role: "user", TurnID: "old", Content: "Q", Images: []string{"data:image/png;base64,not-real"}}, "history_image_context_omitted", "image_context_omitted"},
		{"historical image part", chat.Message{Role: "user", TurnID: "old", MultiContent: []chat.MessageContentPart{{Type: "image_url"}}}, "history_image_context_omitted", "image_context_omitted"},
		{"current image", chat.Message{Role: "user", Content: "Q", Images: []string{"data:image/png;base64,not-real"}}, "image_context_omitted", "history_image_context_omitted"},
		{"assistant quote", chat.Message{Role: "assistant", TurnID: "old", Content: "<attachments>quoted example</attachments>"}, "history_source_permissions_unbound", "history_attachment_context_omitted"},
		{"question about tag", chat.Message{Role: "user", TurnID: "old", Content: "解释 <attachments> 标签"}, "history_source_permissions_unbound", "history_attachment_context_omitted"},
		{"question about caption label", chat.Message{Role: "user", TurnID: "old", Content: "[用户上传图片内容]这个提示是什么意思？"}, "history_source_permissions_unbound", "history_image_context_omitted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &memoryRecorder{}
			RecordModelInput(WithRecorder(context.Background(), r), []chat.Message{tc.message}, nil, "m")
			require.Contains(t, r.gaps, tc.want)
			require.NotContains(t, r.gaps, tc.absent)
			metadata, err := json.Marshal(r.inputs[0].History)
			require.NoError(t, err)
			require.NotContains(t, string(metadata), "limited to Shanghai")
			require.Empty(t, r.sources, "unbound history is not knowledge evidence")
		})
	}
}
