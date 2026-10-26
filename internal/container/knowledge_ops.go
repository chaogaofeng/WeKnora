package container

import (
	"context"

	"github.com/Tencent/WeKnora/internal/knowledgeops"
	"github.com/Tencent/WeKnora/internal/knowledgeops/capture"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/models/utils/ollama"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"go.uber.org/dig"
	"gorm.io/gorm"
)

// Keep all dependency wiring here; main container and router need one hook each.
func registerKnowledgeOps(c *dig.Container) {
	must(c.Provide(func(db *gorm.DB, models interfaces.ModelService, local *ollama.OllamaService, sessions interfaces.SessionRepository, members interfaces.TenantMemberRepository, shares interfaces.KBShareService, agents interfaces.AgentShareService, knowledge interfaces.KnowledgeService) *knowledgeops.Service {
		if setter, ok := knowledge.(interface{ SetFAQAdoptionDB(*gorm.DB) }); ok {
			setter.SetFAQAdoptionDB(db)
		}
		policy := knowledgeops.SourceLookupPolicyFromEnv()
		adapter := &knowledgeops.RuntimeAdapter{DB: db, Sessions: sessions, Members: members, Shares: shares, Agents: agents, SourcePolicy: policy}
		factory := knowledgeops.NewDiagnosticModelFactory(db, local, func(ctx context.Context, id string) (chat.Chat, error) {
			model, err := models.GetModelByID(ctx, id)
			if err != nil {
				return nil, err
			}
			if model.Type != types.ModelTypeKnowledgeQA {
				return nil, knowledgeops.ErrInvalid
			}
			return models.GetChatModel(ctx, id)
		})
		svc := knowledgeops.NewService(knowledgeops.NewRepository(db), knowledgeops.NewAnalyzer(factory, adapter.Sources, policy), adapter.Check)
		svc.SetAdoptionAdapter(&knowledgeops.AdoptionAdapter{DB: db, FAQ: knowledge, Shares: shares})
		svc.SetExecutionContext(adapter.ExecutionContext)
		return svc
	}))
	must(c.Invoke(func(svc *knowledgeops.Service, sessions interfaces.SessionService, cleaner interfaces.ResourceCleaner) {
		if setter, ok := sessions.(interface{ SetKnowledgeOpsSink(capture.Sink) }); ok {
			setter.SetKnowledgeOpsSink(svc)
		}
		svc.StartWorker()
		cleaner.RegisterWithName("knowledge-ops", func() error { svc.Stop(); return nil })
	}))
}
