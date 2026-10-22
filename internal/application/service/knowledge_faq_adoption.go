package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/application/service/retriever"
	werrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"github.com/milvus-io/milvus/pkg/v2/util/merr"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SetFAQAdoptionDB connects the optional, explicit human-publication capability.
// Ordinary FAQ creation/update and the public KnowledgeService interface keep
// their existing behavior and dependencies.
func (s *knowledgeService) SetFAQAdoptionDB(db *gorm.DB) { s.faqAdoptionDB = db }

// CreateOrResumeFAQEntryForAdoption owns one immutable creation intent. Stored
// rows survive failed indexing/finalization and are reindexed with the same
// source ID; the rows are never deleted/recreated or mistaken for indexed content.
func (s *knowledgeService) CreateOrResumeFAQEntryForAdoption(ctx context.Context, kbID string, payload *types.FAQEntryPayload, adoptionID string) (*types.FAQEntry, error) {
	id, err := uuid.Parse(adoptionID)
	if err != nil || id.String() != adoptionID || s.faqAdoptionDB == nil || payload == nil {
		return nil, werrors.NewBadRequestError("invalid FAQ adoption intent")
	}
	// The module publishes one standard question and one complete answer. This
	// capability cannot edit tags, aliases, recommendation, or another entry.
	if payload.ID != nil || payload.TagID != 0 || payload.TagName != "" || len(payload.SimilarQuestions) != 0 || len(payload.NegativeQuestions) != 0 || len(payload.Answers) != 1 || (payload.IsEnabled != nil && !*payload.IsEnabled) || (payload.IsRecommended != nil && !*payload.IsRecommended) {
		return nil, werrors.NewBadRequestError("invalid FAQ adoption payload")
	}
	meta, err := sanitizeFAQEntryPayload(payload)
	if err != nil {
		return nil, err
	}
	if meta.AnswerStrategy != types.AnswerStrategyAll {
		return nil, werrors.NewBadRequestError("invalid FAQ adoption answer strategy")
	}
	meta.Sanitize()
	if len(meta.Answers) != 1 {
		return nil, werrors.NewBadRequestError("invalid FAQ adoption answer")
	}
	meta.Source = "knowledge_ops:" + adoptionID
	kb, ctx, err := s.writableFAQKnowledgeBase(ctx, kbID)
	if err != nil {
		return nil, err
	}
	kb, err = faqAdoptionIndexSnapshot(s.faqAdoptionDB.WithContext(ctx), kb)
	if err != nil {
		return nil, err
	}
	release, err := s.acquireFAQCreateGuard(ctx, kb.TenantID, kb.ID, meta.StandardQuestion)
	if err != nil {
		return nil, err
	}
	defer release()
	chunkID := uuid.NewSHA1(uuid.NameSpaceOID, []byte("knowledge_ops_adoption:"+adoptionID)).String()
	var chunk types.Chunk
	err = s.faqAdoptionDB.WithContext(ctx).First(&chunk, "id = ?", chunkID).Error
	existing := err == nil
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if existing {
		if !faqAdoptionOwnsChunk(&chunk, kb, meta) {
			return nil, werrors.NewConflictError("FAQ adoption entry has changed")
		}
		if chunk.Status == int(types.ChunkStatusIndexed) {
			if chunk.IndexStatus != "ready" && chunk.IndexStatus != "" {
				return nil, werrors.NewConflictError("FAQ adoption entry index state has changed")
			}
			return s.chunkToFAQEntry(&chunk, kb, nil)
		}
		if chunk.Status != int(types.ChunkStatusStored) {
			return nil, werrors.NewConflictError("FAQ adoption entry is not resumable")
		}
		if chunk.IndexStatus != "processing" {
			// Older adoption drafts inherited the repository's ready default.
			// Close the existing search gate before any cleanup or new indexing,
			// without overwriting a row changed since this ownership check.
			q := s.faqAdoptionDB.WithContext(ctx).Model(&types.Chunk{}).Where(map[string]any{
				"id": chunk.ID, "tenant_id": chunk.TenantID, "knowledge_base_id": chunk.KnowledgeBaseID,
				"knowledge_id": chunk.KnowledgeID, "chunk_type": chunk.ChunkType, "status": types.ChunkStatusStored,
				"index_status": chunk.IndexStatus, "content_revision": chunk.ContentRevision,
				"content_hash": chunk.ContentHash, "content": chunk.Content, "tag_id": chunk.TagID,
				"flags": chunk.Flags, "is_enabled": chunk.IsEnabled, "updated_at": chunk.UpdatedAt,
			}).UpdateColumn("index_status", "processing")
			if q.Error != nil {
				return nil, q.Error
			}
			if q.RowsAffected != 1 {
				return nil, werrors.NewConflictError("FAQ adoption entry has changed")
			}
			chunk.IndexStatus = "processing"
		}
	} else {
		if err = s.checkFAQQuestionDuplicate(ctx, kb.TenantID, kb.ID, "", meta); err != nil {
			return nil, werrors.NewConflictError("FAQ question already exists")
		}
	}
	model, err := s.modelService.GetEmbeddingModel(ctx, kb.EmbeddingModelID)
	if err != nil {
		return nil, err
	}
	var knowledge *types.Knowledge
	if existing {
		knowledge, err = s.repo.GetKnowledgeByID(ctx, kb.TenantID, chunk.KnowledgeID)
	} else {
		knowledge, err = s.ensureFAQKnowledge(ctx, kb.TenantID, kb)
	}
	if err != nil {
		return nil, err
	}
	if knowledge == nil || knowledge.TenantID != kb.TenantID || knowledge.KnowledgeBaseID != kb.ID || knowledge.Type != types.KnowledgeTypeFAQ {
		return nil, werrors.NewConflictError("FAQ adoption container has changed")
	}
	if !existing {
		chunk = types.Chunk{ID: chunkID, TenantID: kb.TenantID, KnowledgeID: knowledge.ID, KnowledgeBaseID: kb.ID, Content: buildFAQChunkContent(meta, kb.FAQConfig.IndexMode), IsEnabled: true, Flags: types.ChunkFlagRecommended, ChunkType: types.ChunkTypeFAQ, Status: int(types.ChunkStatusStored), IndexStatus: "processing"}
		if err = chunk.SetFAQMetadata(meta); err != nil {
			return nil, err
		}
		if err = s.chunkService.CreateChunks(ctx, []*types.Chunk{&chunk}); err != nil {
			return nil, err
		}
	}
	engine, err := retriever.CreateRetrieveEngineForKB(ctx, &faqAdoptionIndexRegistry{RetrieveEngineRegistry: s.retrieveEngine, db: s.faqAdoptionDB}, s.ownership, kb.TenantID, kb.VectorStoreID)
	if err != nil {
		return nil, err
	}
	info, err := s.buildFAQIndexInfoList(ctx, kb, &chunk)
	if err != nil {
		return nil, err
	}
	size := engine.EstimateStorageSize(ctx, model, info)
	if size < 0 {
		return nil, fmt.Errorf("invalid FAQ adoption storage estimate")
	}
	var tenant types.Tenant
	if err = s.faqAdoptionDB.WithContext(ctx).Select("id", "storage_used", "storage_quota").First(&tenant, kb.TenantID).Error; err != nil {
		return nil, err
	}
	if tenant.StorageQuota > 0 && (size > tenant.StorageQuota || tenant.StorageUsed > tenant.StorageQuota-size) {
		return nil, types.NewStorageQuotaExceededError()
	}
	// indexFAQChunks also updates storage/Knowledge outside a shared SQL
	// transaction. Use its existing index builders and engine here so the
	// accounting and Indexed transition below can commit exactly once.
	indexCtx, cancel := context.WithTimeout(ctx, faqCreateIndexBudget)
	if existing {
		// Some stores append random physical IDs even for stable SourceIDs.
		// Only this validated, owned Stored chunk may be cleaned and rebuilt;
		// a failed cleanup must never be followed by another batch insert.
		if err = engine.DeleteByChunkIDList(indexCtx, []string{chunk.ID}, model.GetDimensions(), types.KnowledgeTypeFAQ); err != nil {
			cancel()
			return nil, fmt.Errorf("failed to clean FAQ adoption index: %w", err)
		}
	}
	err = engine.BatchIndex(indexCtx, model, info)
	cancel()
	if err != nil {
		return nil, err
	}
	if err = s.finalizeFAQAdoption(ctx, kb, meta, &chunk, size, adoptionID); err != nil {
		return nil, err
	}
	return s.chunkToFAQEntry(&chunk, kb, nil)
}

func faqAdoptionOwnsChunk(chunk *types.Chunk, kb *types.KnowledgeBase, want *types.FAQChunkMetadata) bool {
	if chunk.TenantID != kb.TenantID || chunk.KnowledgeBaseID != kb.ID || chunk.ChunkType != types.ChunkTypeFAQ || !chunk.IsEnabled {
		return false
	}
	meta, err := chunk.FAQMetadata()
	return err == nil && meta != nil && meta.Source == want.Source && meta.Version == 1 && meta.StandardQuestion == want.StandardQuestion && len(meta.Answers) == 1 && meta.Answers[0] == want.Answers[0] && len(meta.SimilarQuestions) == 0 && len(meta.NegativeQuestions) == 0 && meta.AnswerStrategy == types.AnswerStrategyAll
}

// The owning chunk row is the durable accounting latch. Any failure, including
// audit insertion or Indexed status persistence, rolls the entire finalization
// back. Repeating a successful intent cannot charge or audit it a second time.
func (s *knowledgeService) finalizeFAQAdoption(ctx context.Context, kb *types.KnowledgeBase, meta *types.FAQChunkMetadata, chunk *types.Chunk, size int64, adoptionID string) error {
	return s.faqAdoptionDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		currentKB, err := faqAdoptionIndexSnapshot(tx.Clauses(clause.Locking{Strength: "UPDATE"}), kb)
		if err != nil {
			return err
		}
		storeID := func(value *string) string {
			if value == nil {
				return ""
			}
			return *value
		}
		if currentKB.EmbeddingModelID != kb.EmbeddingModelID || storeID(currentKB.VectorStoreID) != storeID(kb.VectorStoreID) || currentKB.FAQConfig.IndexMode != kb.FAQConfig.IndexMode || currentKB.FAQConfig.QuestionIndexMode != kb.FAQConfig.QuestionIndexMode {
			return werrors.NewConflictError("FAQ adoption index configuration has changed")
		}
		var current types.Chunk
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "id = ?", chunk.ID).Error; err != nil {
			return err
		}
		if !faqAdoptionOwnsChunk(&current, kb, meta) || current.KnowledgeID != chunk.KnowledgeID || current.ContentHash != chunk.ContentHash || current.Content != chunk.Content || current.TagID != chunk.TagID || current.Flags != chunk.Flags || current.ContentRevision != chunk.ContentRevision {
			return werrors.NewConflictError("FAQ adoption entry has changed")
		}
		if current.Status == int(types.ChunkStatusIndexed) {
			if current.IndexStatus != "ready" && current.IndexStatus != "" {
				return werrors.NewConflictError("FAQ adoption entry index state has changed")
			}
			*chunk = current
			return nil
		}
		if current.Status != int(types.ChunkStatusStored) || current.IndexStatus != "processing" || chunk.IndexStatus != "processing" {
			return werrors.NewConflictError("FAQ adoption entry is not resumable")
		}
		now := time.Now().UTC()
		quota := tx.Model(&types.Tenant{}).Where("id = ? AND (storage_quota <= 0 OR storage_used + ? <= storage_quota)", kb.TenantID, size).UpdateColumn("storage_used", gorm.Expr("storage_used + ?", size))
		if quota.Error != nil {
			return quota.Error
		}
		if quota.RowsAffected != 1 {
			return types.NewStorageQuotaExceededError()
		}
		container := tx.Model(&types.Knowledge{}).Where("id = ? AND tenant_id = ? AND knowledge_base_id = ? AND type = ?", chunk.KnowledgeID, kb.TenantID, kb.ID, types.KnowledgeTypeFAQ).Updates(map[string]any{"storage_size": gorm.Expr("storage_size + ?", size), "updated_at": now, "processed_at": now})
		if container.Error != nil {
			return container.Error
		}
		if container.RowsAffected != 1 {
			return werrors.NewConflictError("FAQ adoption container has changed")
		}
		q := tx.Model(&types.Chunk{}).Where("id = ? AND tenant_id = ? AND status = ? AND index_status = ? AND content_revision = ?", chunk.ID, kb.TenantID, types.ChunkStatusStored, "processing", chunk.ContentRevision).Updates(map[string]any{"status": types.ChunkStatusIndexed, "index_status": "ready", "updated_at": now})
		if q.Error != nil {
			return q.Error
		}
		if q.RowsAffected != 1 {
			return werrors.NewConflictError("FAQ adoption entry has changed")
		}
		details, _ := json.Marshal(map[string]any{"source_type": "faq", "entry_id": current.SeqID, "adoption_id": adoptionID})
		caller := types.CallerFromContext(ctx)
		if err := tx.Create(&types.AuditLog{TenantID: kb.TenantID, ActorUserID: caller.UserID, ActorRole: string(caller.Role), Action: types.AuditActionKnowledgeCreated, ScopeType: auditScopeKnowledgeBase, ScopeID: kb.ID, TargetType: "faq_entry", TargetID: chunk.ID, Outcome: types.AuditOutcomeSuccess, Details: types.JSON(details), CreatedAt: now}).Error; err != nil {
			return err
		}
		current.Status = int(types.ChunkStatusIndexed)
		current.IndexStatus = "ready"
		current.UpdatedAt = now
		*chunk = current
		return nil
	})
}

// Read both the admitted index snapshot and the final configuration gate from
// the same persisted fields. Raw JSON decoding also accepts SQLite TEXT rows
// that the legacy FAQConfig scanner silently treats as missing configuration.
// A copy keeps request-local defaults and parsing out of the KB service cache.
func faqAdoptionIndexSnapshot(db *gorm.DB, kb *types.KnowledgeBase) (*types.KnowledgeBase, error) {
	var row struct {
		EmbeddingModelID string
		VectorStoreID    *string
		FAQConfig        []byte
	}
	if err := db.Model(&types.KnowledgeBase{}).Select("embedding_model_id", "vector_store_id", "faq_config").Where("id = ? AND tenant_id = ? AND type = ? AND is_temporary = ?", kb.ID, kb.TenantID, types.KnowledgeBaseTypeFAQ, false).Take(&row).Error; err != nil {
		return nil, err
	}
	snapshot := *kb
	snapshot.EmbeddingModelID = row.EmbeddingModelID
	snapshot.VectorStoreID = row.VectorStoreID
	snapshot.FAQConfig = nil
	if len(row.FAQConfig) > 0 {
		if err := json.Unmarshal(row.FAQConfig, &snapshot.FAQConfig); err != nil {
			return nil, err
		}
	}
	snapshot.EnsureDefaults()
	return &snapshot, nil
}

// Normalize the one proven empty-backend case per engine before the composite
// selects its first error. Ignoring a composite-level not-found could otherwise
// hide a concurrent permission or network failure from another backend.
// These wrappers are private to this explicit adoption capability.
type faqAdoptionIndexRegistry struct {
	interfaces.RetrieveEngineRegistry
	db *gorm.DB
}

func (r *faqAdoptionIndexRegistry) GetRetrieveEngineService(kind types.RetrieverEngineType) (interfaces.RetrieveEngineService, error) {
	engine, err := r.RetrieveEngineRegistry.GetRetrieveEngineService(kind)
	if err != nil {
		return nil, err
	}
	return &faqAdoptionIndexEngine{RetrieveEngineService: engine, db: r.db}, nil
}
func (r *faqAdoptionIndexRegistry) GetOrLoadByStoreID(ctx context.Context, tenant uint64, id string) (interfaces.RetrieveEngineService, error) {
	engine, err := r.RetrieveEngineRegistry.GetOrLoadByStoreID(ctx, tenant, id)
	if err != nil {
		return nil, err
	}
	return &faqAdoptionIndexEngine{RetrieveEngineService: engine, db: r.db, tenant: tenant, storeID: id}, nil
}

type faqAdoptionIndexEngine struct {
	interfaces.RetrieveEngineService
	db      *gorm.DB
	tenant  uint64
	storeID string
}

func (e *faqAdoptionIndexEngine) DeleteByChunkIDList(ctx context.Context, ids []string, dimension int, knowledgeType string) error {
	err := e.RetrieveEngineService.DeleteByChunkIDList(ctx, ids, dimension, knowledgeType)
	if errors.Is(err, merr.ErrCollectionNotFound) {
		return nil
	}
	if faqAdoptionWeaviateCleanupNeedsProbe(e.EngineType(), err) {
		missing, probeErr := faqAdoptionWeaviateCollectionMissing(ctx, e.db, e.tenant, e.storeID, dimension)
		if probeErr != nil {
			return errors.Join(err, fmt.Errorf("verify FAQ adoption collection: %w", probeErr))
		}
		if missing {
			return nil
		}
	}
	return err
}
