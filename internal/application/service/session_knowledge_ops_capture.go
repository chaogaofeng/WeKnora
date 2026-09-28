package service

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/knowledgeops/capture"
	"github.com/Tencent/WeKnora/internal/types"
)

// The session's tenant owns the run even when a shared agent retrieves from
// another tenant. Source authorization remains tied to each source's identity.
func (s *sessionService) startKnowledgeOps(ctx context.Context, req *types.QARequest, mode string) context.Context {
	if s.captureSink == nil || req == nil || req.Session == nil {
		return ctx
	}
	actor := "unknown"
	channel := "unknown"
	if principal, ok := types.PrincipalFromContext(ctx); ok {
		actor = principal.StorageID()
		switch principal.Type {
		case types.PrincipalWebUser:
			channel = "web"
		case types.PrincipalAPITenant, types.PrincipalAPIPlatform, types.PrincipalAPIExternalUser:
			channel = "api"
		case types.PrincipalEmbedChannel, types.PrincipalEmbedSession, types.PrincipalEmbedVisitor:
			channel = "embed"
		case types.PrincipalMCPEndpoint:
			channel = "mcp"
		}
	}
	if req.Session.IMPlatform != "" {
		channel = req.Session.IMPlatform
	}
	requestID, _ := types.RequestIDFromContext(ctx)
	qaType := "rag-qa"
	agentID := ""
	if req.CustomAgent != nil {
		agentID = req.CustomAgent.ID
		if mode == "smart-reasoning" {
			qaType = req.CustomAgent.Config.AgentType
			if qaType == "" {
				qaType = "unknown"
			}
		}
	}
	// Effective model identity is filled from the first actual model call.
	modelID := "unknown"
	ctx = capture.Begin(ctx, s.captureSink, capture.Start{TenantID: req.Session.TenantID, ActorID: actor, SessionID: req.Session.ID, MessageID: req.AssistantMessageID, RequestID: requestID, Channel: channel, QAType: qaType, ExecutionMode: mode, AgentID: agentID, ModelID: modelID, Question: req.Query, StartedAt: time.Now().UTC()})
	if len(req.ImageURLs) > 0 {
		capture.Gap(ctx, "image_context_omitted")
	}
	if len(req.Attachments) > 0 {
		capture.Gap(ctx, "attachment_source_permissions_unbound")
	}
	if req.QuotedContext != "" {
		capture.Gap(ctx, "quoted_context_source_permissions_unbound")
	}
	return ctx
}

// SetKnowledgeOpsSink is a construction-time optional dependency hook. The
// container calls it before serving requests; runtime enablement lives in Sink.
func (s *sessionService) SetKnowledgeOpsSink(sink capture.Sink) { s.captureSink = sink }
