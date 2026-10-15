package chatpipeline

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/knowledgeops/capture"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type opsHistoryMessages struct {
	interfaces.MessageService
	messages []*types.Message
}

func (m opsHistoryMessages) GetRecentMessagesBySession(context.Context, string, int) ([]*types.Message, error) {
	return m.messages, nil
}

func TestRAGHistoryIdentitySurvivesLoadingAndModelEncoding(t *testing.T) {
	now := time.Now()
	history, err := loadAndProcessHistory(context.Background(), opsHistoryMessages{messages: []*types.Message{
		{ID: "old-answer", RequestID: "old-request", Role: "assistant", Content: "第三点是销售辅助", CreatedAt: now},
		{ID: "old-user", RequestID: "old-request", Role: "user", Content: "合作成果有哪些", CreatedAt: now.Add(-time.Second)},
	}}, "session", 5, 20)
	require.NoError(t, err)
	cm := &types.ChatManage{PipelineState: types.PipelineState{History: history, UserContent: "展开第三点"}}
	messages, codec := prepareMessagesWithModelContext(context.Background(), cm)
	r := &opsRecorder{}
	ctx := capture.WithRecorder(context.Background(), r)
	capture.RecordRAGModelInput(ctx, codec.EncodeMessages(messages), codec.CompactKnownText, "model", len(history))
	require.Len(t, r.inputs, 1)
	require.True(t, r.inputs[0].History.IDsKnown, "diagnosis must be able to locate the exact history used by generation")
	require.Equal(t, []string{"old-answer"}, r.inputs[0].History.TurnIDs)
	require.Equal(t, 1, r.inputs[0].History.Turns)
	require.Empty(t, r.sources, "past assistant text is context, never authoritative evidence")
}
