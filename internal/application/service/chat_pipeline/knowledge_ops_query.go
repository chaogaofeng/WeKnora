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
