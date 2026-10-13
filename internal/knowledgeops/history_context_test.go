package knowledgeops

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/knowledgeops/capture"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/stretchr/testify/require"
)

func TestHistorySnapshotPersistsActualInputWithoutRawHistory(t *testing.T) {
	s, repo := enabledTestService(t)
	ctx := capture.Begin(context.Background(), s, capture.Start{TenantID: 1, MessageID: "history-snapshot", StartedAt: time.Now().UTC()})
	capture.RecordRAGModelInput(ctx, []chat.Message{{Role: "user", Content: "private past"}, {Role: "assistant", Content: "private past answer"}, {Role: "user", Content: "thanks"}}, nil, "model", 2)
	capture.Finish(ctx, "You are welcome", nil)
	var run Run
	require.NoError(t, repo.db.Where("message_id = ?", "history-snapshot").Take(&run).Error)
	require.NotNil(t, run.HistoryContext)
	require.Equal(t, 2, run.HistoryContext.Turns)
	require.False(t, run.HistoryContext.IDsKnown)
	require.Equal(t, 1, run.HistoryContext.ModelCalls)
	bytes, err := json.Marshal(run.HistoryContext)
	require.NoError(t, err)
	require.NotContains(t, string(bytes), "private past")
	var n int64
	require.NoError(t, repo.db.Model(&Evidence{}).Where("run_id = ?", run.ID).Count(&n).Error)
	require.Zero(t, n, "history is not copied into unbound evidence")
	// Absence is preserved for old or interrupted records; zero is meaningful only for a captured input.
	legacy := Run{ID: "legacy-context", TenantID: 1, MessageID: "legacy-context"}
	require.NoError(t, repo.CreateRun(context.Background(), &legacy))
	stored, err := repo.Run(context.Background(), 1, legacy.ID)
	require.NoError(t, err)
	require.Nil(t, stored.HistoryContext)
}
