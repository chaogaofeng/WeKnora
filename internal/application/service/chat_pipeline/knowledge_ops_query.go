package chatpipeline

import (
	"context"

	"github.com/Tencent/WeKnora/internal/knowledgeops/capture"
	"github.com/Tencent/WeKnora/internal/types"
)

// observeKnowledgeOpsRewrite adapts the pipeline callback to the optional
// observer. All capture state and policy live in knowledgeops/capture; neither
// persistence nor diagnosis belongs in the QA pipeline.
func observeKnowledgeOpsRewrite(ctx context.Context, cm *types.ChatManage, next func() *PluginError) (context.Context, func() *PluginError) {
	ctx, finish := capture.BeginRewrite(ctx, cm.Query, cm.EnableRewrite || len(cm.Images) > 0)
	return ctx, func() *PluginError {
		finish(cm.RewriteQuery)
		return next()
	}
}

// Observe the decision at the retrieval stage, not at rewrite time: a rewrite
// alone does not establish whether downstream retrieval was entered or skipped.
func observeKnowledgeOpsRetrieval(ctx context.Context, cm *types.ChatManage) {
	event := capture.QueryEvent{Kind: "retrieval", Status: "required", Intent: string(cm.Intent), Reason: "intent_requires_retrieval"}
	if !cm.NeedsRetrieval() {
		event.Status, event.Reason = "skipped", "intent_no_search"
		if cm.Intent == types.IntentFollowUp {
			event.Reason = "context_only_follow_up"
		}
	}
	capture.RecordQuery(ctx, event)
}
