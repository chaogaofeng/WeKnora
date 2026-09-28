package knowledgeops

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/knowledgeops/capture"
	"github.com/stretchr/testify/require"
)

func TestCaptureDisabledAndNewRunBoundary(t *testing.T) {
	repo := testStore(t)
	svc := NewService(repo, nil, nil)
	ctx := context.Background()
	start := capture.Start{TenantID: 1, SessionID: "session", MessageID: "old", StartedAt: time.Now()}
	require.Nil(t, svc.Start(ctx, start))
	s := DefaultSettings(1)
	s.Enabled = true
	s.ModelID = "m"
	require.NoError(t, repo.SaveSettings(ctx, 1, s))
	require.Nil(t, svc.Start(ctx, start))
	start.StartedAt = time.Now().Add(time.Second)
	start.MessageID = "new"
	rec := svc.Start(ctx, start)
	require.NotNil(t, rec)
	rec.RecordInput(ctx, capture.Input{Messages: []capture.InputMessage{{Role: "user", Content: "Q"}}})
	rec.RecordSources(ctx, []capture.Source{{Kind: "wiki", KBID: "kb", PageID: "p", Version: "2", Content: "A", SentToModel: true, AcquiredAt: time.Now()}})
	rec.Finish(ctx, capture.Completion{Answer: "A", Status: "completed", FinishedAt: time.Now()})
	rec.Finish(ctx, capture.Completion{Answer: "changed", Status: "completed", FinishedAt: time.Now()})
	var runs []Run
	require.NoError(t, repo.db.Find(&runs).Error)
	require.Len(t, runs, 1)
	require.Equal(t, "A", runs[0].Answer)
	require.Equal(t, "complete", runs[0].CaptureStatus)
	var tasks []Task
	require.NoError(t, repo.db.Find(&tasks).Error)
	require.Len(t, tasks, 1)
}
func TestCaptureFailureCannotBecomeComplete(t *testing.T) {
	repo := testStore(t)
	svc := NewService(repo, nil, nil)
	ctx := context.Background()
	s := DefaultSettings(1)
	s.Enabled = true
	s.ModelID = "m"
	require.NoError(t, repo.SaveSettings(ctx, 1, s))
	rec := svc.Start(ctx, capture.Start{TenantID: 1, MessageID: "m", StartedAt: time.Now()})
	require.NotNil(t, rec)
	rec.Gap(ctx, "unsupported tool context")
	rec.RecordSources(ctx, []capture.Source{{Kind: "wiki", Content: "body", SentToModel: true}})
	rec.Finish(ctx, capture.Completion{Answer: "A", Status: "completed"})
	var r Run
	require.NoError(t, repo.db.First(&r).Error)
	require.Equal(t, "partial", r.CaptureStatus)
	require.Contains(t, r.Gaps, "unsupported tool context")
}
