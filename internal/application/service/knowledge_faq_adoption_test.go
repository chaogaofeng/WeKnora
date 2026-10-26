package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/knowledgeops"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"github.com/milvus-io/milvus/pkg/v2/util/merr"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type adoptionTestChunkService struct {
	interfaces.ChunkService
	repo interfaces.ChunkRepository
}

func (s *adoptionTestChunkService) CreateChunks(ctx context.Context, rows []*types.Chunk) error {
	return s.repo.CreateChunks(ctx, rows)
}
func (s *adoptionTestChunkService) UpdateChunk(ctx context.Context, row *types.Chunk) error {
	return s.repo.UpdateChunk(ctx, row)
}

// A deliberately unavailable rollback reproduces the old create path's second
// failure window. The resumable adoption path should not depend on deletion.
func (s *adoptionTestChunkService) DeleteChunk(context.Context, string) error {
	return errors.New("rollback unavailable")
}

type adoptionTestIndex struct {
	onIndex func()
	parentChildRetrieveEngine
	fail            bool
	failAfterAppend bool
	failDelete      bool
	deleteErr       error
	documents       []*types.IndexInfo
}

func (e *adoptionTestIndex) EstimateStorageSize(context.Context, embedding.Embedder, []*types.IndexInfo, []types.RetrieverType) int64 {
	return 17
}
func (e *adoptionTestIndex) BatchIndex(_ context.Context, _ embedding.Embedder, rows []*types.IndexInfo, _ []types.RetrieverType) error {
	if e.onIndex != nil {
		e.onIndex()
	}
	if e.fail {
		return errors.New("index unavailable")
	}
	// Keep each physical insert; stable SourceID does not deduplicate this store.
	for _, row := range rows {
		copy := *row
		copy.ID = uuid.NewString()
		e.documents = append(e.documents, &copy)
	}
	if e.failAfterAppend {
		return errors.New("index response lost after appending vectors")
	}
	return nil
}
func (e *adoptionTestIndex) DeleteByChunkIDList(_ context.Context, ids []string, dimension int, knowledgeType string) error {
	if dimension != 1 || knowledgeType != types.KnowledgeTypeFAQ {
		return errors.New("wrong cleanup scope")
	}
	if e.deleteErr != nil {
		return e.deleteErr
	}
	if e.failDelete {
		return errors.New("index cleanup unavailable")
	}
	kept := e.documents[:0]
	for _, doc := range e.documents {
		remove := false
		for _, id := range ids {
			if doc.ChunkID == id && doc.KnowledgeType == knowledgeType {
				remove = true
			}
		}
		if !remove {
			kept = append(kept, doc)
		}
	}
	e.documents = kept
	return nil
}
func newAdoptionServiceIntegration(t *testing.T) (*faqWriteFixture, *knowledgeops.Service, *adoptionTestIndex, knowledgeops.Run, knowledgeops.AdoptionInput) {
	t.Helper()
	f := newFAQWriteFixture(t)
	raw, err := f.db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { raw.Close() })
	require.NoError(t, f.db.AutoMigrate(&types.KnowledgeBase{}, &types.Tenant{}, &types.AuditLog{}))
	require.NoError(t, f.db.Create(f.kb).Error)
	require.NoError(t, f.db.Create(&types.Tenant{ID: 7, Name: "owner", StorageQuota: 1000}).Error)
	for _, name := range []string{"000034_knowledge_ops", "000035_knowledge_ops_history", "000036_knowledge_ops_queries", "000037_knowledge_ops_adoptions", "000038_knowledge_ops_followups", "000039_knowledge_ops_calibration"} {
		body, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "sqlite", name+".up.sql"))
		require.NoError(t, err)
		for _, sql := range strings.Split(string(body), ";") {
			if strings.TrimSpace(sql) != "" {
				require.NoError(t, f.db.Exec(sql).Error)
			}
		}
	}
	f.svc.SetFAQAdoptionDB(f.db)
	f.svc.chunkService = &adoptionTestChunkService{repo: f.svc.chunkRepo}
	f.svc.modelService = parentChildModelService{embedder: parentChildEmbedder{}}
	f.svc.tenantRepo = repository.NewTenantRepository(f.db)
	index := &adoptionTestIndex{}
	f.svc.retrieveEngine = &fakeFanoutRegistry{byStore: map[string]interfaces.RetrieveEngineService{*f.kb.VectorStoreID: index}}
	f.ctx = types.WithCaller(context.Background(), types.Caller{TenantID: 7, UserID: "publisher", Role: types.TenantRoleAdmin})
	f.ctx = context.WithValue(f.ctx, types.TenantInfoContextKey, &types.Tenant{ID: 7, StorageQuota: 1000})
	repo := knowledgeops.NewRepository(f.db)
	ops := knowledgeops.NewService(repo, nil, func(context.Context, knowledgeops.Run, []knowledgeops.Evidence) error { return nil })
	ops.SetAdoptionAdapter(&knowledgeops.AdoptionAdapter{DB: f.db, FAQ: f.svc})
	now := time.Now().UTC()
	run := knowledgeops.Run{ID: uuid.NewString(), TenantID: 7, ActorID: "web_user:source", MessageID: uuid.NewString(), Question: "Q", Answer: "A", Status: "completed", StartedAt: now, FinishedAt: &now}
	require.NoError(t, repo.CreateRun(f.ctx, &run))
	analysis := knowledgeops.Analysis{ID: uuid.NewString(), TenantID: 7, RunID: run.ID, Version: 1, Status: "succeeded", CreatedAt: now}
	require.NoError(t, f.db.Create(&analysis).Error)
	review := knowledgeops.Review{ID: uuid.NewString(), TenantID: 7, RunID: run.ID, AnalysisID: analysis.ID, Target: "answer", Action: "accepted", Verdict: "usable", Reason: "checked", ReviewerID: "publisher", CreatedAt: now}
	require.NoError(t, f.db.Create(&review).Error)
	input := knowledgeops.AdoptionInput{AnalysisID: analysis.ID, ReviewID: review.ID, KBID: "kb", Question: "approved question", Answer: "approved answer"}
	return f, ops, index, run, input
}

func TestFAQAdoptionIntegrationRecoversStoredWithoutDuplicatesOrFalseSuccess(t *testing.T) {
	for _, failure := range []string{"indexed_state_write_failed", "index_failed_rollback_unavailable"} {
		t.Run(failure, func(t *testing.T) {
			f, ops, index, run, input := newAdoptionServiceIntegration(t)
			if failure == "indexed_state_write_failed" {
				require.NoError(t, f.db.Exec("CREATE TRIGGER fail_adoption_index_state BEFORE UPDATE OF status ON chunks WHEN NEW.status=2 BEGIN SELECT RAISE(FAIL,'state write failed'); END").Error)
			} else {
				index.fail = true
			}
			first, err := ops.Adopt(f.ctx, 7, run.ID, input)
			require.NoError(t, err)
			require.Equal(t, "failed", first.Status)
			var pending []types.Chunk
			require.NoError(t, f.db.Where("tenant_id = ? AND knowledge_base_id = ? AND status = ?", 7, "kb", types.ChunkStatusStored).Find(&pending).Error)
			require.Len(t, pending, 1)
			originalID := pending[0].ID
			listed, err := f.svc.ListFAQEntries(types.WithExecutionTenant(f.ctx, 7), "kb", &types.Pagination{Page: 1, PageSize: 20}, nil, 0, input.Question, "standard_question", "", nil)
			require.NoError(t, err)
			require.Zero(t, listed.Total)
			if failure == "indexed_state_write_failed" {
				require.NoError(t, f.db.Exec("DROP TRIGGER fail_adoption_index_state").Error)
			} else {
				index.fail = false
			}
			recovered, err := ops.RetryAdoption(f.ctx, 7, run.ID, first.ID)
			require.NoError(t, err)
			require.Equal(t, "succeeded", recovered.Status)
			var ready types.Chunk
			require.NoError(t, f.db.First(&ready, "id = ?", originalID).Error)
			require.Equal(t, int(types.ChunkStatusIndexed), ready.Status)
			require.Equal(t, ready.SeqID, recovered.EntryID)
			var count int64
			require.NoError(t, f.db.Model(&types.Chunk{}).Where("knowledge_base_id = ? AND status IN ?", "kb", []int{1, 2}).Count(&count).Error)
			require.EqualValues(t, 1, count)
			require.Len(t, index.documents, 1)
			_, err = ops.RetryAdoption(f.ctx, 7, run.ID, first.ID)
			require.NoError(t, err)
			var tenant types.Tenant
			require.NoError(t, f.db.First(&tenant, 7).Error)
			require.EqualValues(t, 17, tenant.StorageUsed)
			var knowledge types.Knowledge
			require.NoError(t, f.db.First(&knowledge, "id = ?", ready.KnowledgeID).Error)
			require.EqualValues(t, 17, knowledge.StorageSize)
			require.NoError(t, f.db.Model(&types.AuditLog{}).Where("action = ? AND target_id = ?", types.AuditActionKnowledgeCreated, originalID).Count(&count).Error)
			require.EqualValues(t, 1, count)
		})
	}
}

func TestFAQAdoptionIntegrationDoesNotTakeOverAnExistingStoredFAQ(t *testing.T) {
	f, ops, index, run, input := newAdoptionServiceIntegration(t)
	external := &types.Chunk{ID: uuid.NewString(), TenantID: 7, KnowledgeID: "faq", KnowledgeBaseID: "kb", ChunkType: types.ChunkTypeFAQ, Status: int(types.ChunkStatusStored), IsEnabled: true}
	require.NoError(t, external.SetFAQMetadata(&types.FAQChunkMetadata{StandardQuestion: input.Question, Answers: []string{input.Answer}, AnswerStrategy: types.AnswerStrategyAll, Source: "faq", Version: 1}))
	require.NoError(t, f.svc.chunkRepo.CreateChunks(f.ctx, []*types.Chunk{external}))
	index.documents = []*types.IndexInfo{{ID: "external-vector", ChunkID: external.ID, KnowledgeType: types.KnowledgeTypeFAQ}}
	got, err := ops.Adopt(f.ctx, 7, run.ID, input)
	require.NoError(t, err)
	require.Equal(t, "failed", got.Status)
	require.Equal(t, "faq_question_conflict", got.Error)
	got, err = ops.RetryAdoption(f.ctx, 7, run.ID, got.ID)
	require.NoError(t, err)
	require.Equal(t, "failed", got.Status)
	var after types.Chunk
	require.NoError(t, f.db.First(&after, "id = ?", external.ID).Error)
	require.Equal(t, int(types.ChunkStatusStored), after.Status)
	require.Equal(t, external.Metadata, after.Metadata)
	var count int64
	require.NoError(t, f.db.Model(&types.Chunk{}).Where("status = ?", types.ChunkStatusStored).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.Len(t, index.documents, 1)
	require.Equal(t, "external-vector", index.documents[0].ID)
}
func TestFAQAdoptionIntegrationDoesNotOverwriteAnEditedOwnedDraft(t *testing.T) {
	for _, change := range []string{"answer", "marker"} {
		t.Run(change, func(t *testing.T) {
			f, ops, index, run, input := newAdoptionServiceIntegration(t)
			index.fail = true
			got, err := ops.Adopt(f.ctx, 7, run.ID, input)
			require.NoError(t, err)
			require.Equal(t, "failed", got.Status)
			var stored types.Chunk
			require.NoError(t, f.db.First(&stored, "status = ?", types.ChunkStatusStored).Error)
			meta, err := stored.FAQMetadata()
			require.NoError(t, err)
			if change == "answer" {
				meta.Answers = []string{"another person's edited answer"}
			} else {
				meta.Source = "faq"
			}
			require.NoError(t, stored.SetFAQMetadata(meta))
			require.NoError(t, f.svc.chunkRepo.UpdateChunk(f.ctx, &stored))
			index.fail = false
			index.documents = []*types.IndexInfo{{ID: "edited-vector", ChunkID: stored.ID, KnowledgeType: types.KnowledgeTypeFAQ}}
			after, err := ops.RetryAdoption(f.ctx, 7, run.ID, got.ID)
			require.NoError(t, err)
			require.Equal(t, "failed", after.Status)
			var current types.Chunk
			require.NoError(t, f.db.First(&current, "id = ?", stored.ID).Error)
			require.Equal(t, stored.Metadata, current.Metadata)
			require.Equal(t, int(types.ChunkStatusStored), current.Status)
			require.Len(t, index.documents, 1)
			require.Equal(t, "edited-vector", index.documents[0].ID)
		})
	}
}
func TestFAQAdoptionIntegrationAuditFailureRollsBackStateAndAccounting(t *testing.T) {
	f, ops, _, run, input := newAdoptionServiceIntegration(t)
	require.NoError(t, f.db.Exec("CREATE TRIGGER fail_adoption_audit BEFORE INSERT ON audit_logs BEGIN SELECT RAISE(FAIL,'audit unavailable'); END").Error)
	got, err := ops.Adopt(f.ctx, 7, run.ID, input)
	require.NoError(t, err)
	require.Equal(t, "failed", got.Status)
	var tenant types.Tenant
	require.NoError(t, f.db.First(&tenant, 7).Error)
	require.Zero(t, tenant.StorageUsed)
	var container types.Knowledge
	require.NoError(t, f.db.First(&container, "id = ?", "faq").Error)
	require.Zero(t, container.StorageSize)
	var stored types.Chunk
	require.NoError(t, f.db.First(&stored, "status = ?", types.ChunkStatusStored).Error)
	require.NoError(t, f.db.Exec("DROP TRIGGER fail_adoption_audit").Error)
	got, err = ops.RetryAdoption(f.ctx, 7, run.ID, got.ID)
	require.NoError(t, err)
	require.Equal(t, "succeeded", got.Status)
	require.NoError(t, f.db.First(&tenant, 7).Error)
	require.EqualValues(t, 17, tenant.StorageUsed)
	var count int64
	require.NoError(t, f.db.Model(&types.AuditLog{}).Where("target_id = ?", stored.ID).Count(&count).Error)
	require.EqualValues(t, 1, count)
}
func TestFAQAdoptionIntegrationRepeatedIndexFailureKeepsOneUnchargedDraft(t *testing.T) {
	f, ops, index, run, input := newAdoptionServiceIntegration(t)
	index.fail = true
	got, err := ops.Adopt(f.ctx, 7, run.ID, input)
	require.NoError(t, err)
	require.Equal(t, "failed", got.Status)
	got, err = ops.RetryAdoption(f.ctx, 7, run.ID, got.ID)
	require.NoError(t, err)
	require.Equal(t, "failed", got.Status)
	var count int64
	require.NoError(t, f.db.Model(&types.Chunk{}).Where("status = ?", types.ChunkStatusStored).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, f.db.Model(&types.AuditLog{}).Count(&count).Error)
	require.Zero(t, count)
	var tenant types.Tenant
	require.NoError(t, f.db.First(&tenant, 7).Error)
	require.Zero(t, tenant.StorageUsed)
	require.Empty(t, index.documents)
}
func TestFAQAdoptionIntegrationChecksQuotaBeforeIndexing(t *testing.T) {
	f, ops, index, run, input := newAdoptionServiceIntegration(t)
	require.NoError(t, f.db.Model(&types.Tenant{}).Where("id = ?", 7).UpdateColumn("storage_quota", 16).Error)
	got, err := ops.Adopt(f.ctx, 7, run.ID, input)
	require.NoError(t, err)
	require.Equal(t, "failed", got.Status)
	require.Empty(t, index.documents)
	var tenant types.Tenant
	require.NoError(t, f.db.First(&tenant, 7).Error)
	require.Zero(t, tenant.StorageUsed)
}

func TestFAQAdoptionIntegrationRefusesFinalizationAfterTargetTransfer(t *testing.T) {
	f, ops, index, run, input := newAdoptionServiceIntegration(t)
	index.onIndex = func() {
		require.NoError(t, f.db.Model(&types.KnowledgeBase{}).Where("id = ?", "kb").UpdateColumn("tenant_id", 8).Error)
	}
	got, err := ops.Adopt(f.ctx, 7, run.ID, input)
	require.NoError(t, err)
	require.Equal(t, "failed", got.Status)
	var tenant types.Tenant
	require.NoError(t, f.db.First(&tenant, 7).Error)
	require.Zero(t, tenant.StorageUsed)
	var stored types.Chunk
	require.NoError(t, f.db.First(&stored, "status = ?", types.ChunkStatusStored).Error)
}

// Qdrant/Milvus append fresh vector primary keys even for an unchanged SourceID.
// Repeated failed finalization must replace only the owned chunk's vectors.
func TestFAQAdoptionIntegrationAppendOnlyIndexRecoveryDoesNotDuplicateVectors(t *testing.T) {
	for _, failure := range []string{"status_commit", "partial_index"} {
		t.Run(failure, func(t *testing.T) {
			f, ops, index, run, input := newAdoptionServiceIntegration(t)
			index.documents = []*types.IndexInfo{{ID: "unrelated-vector", ChunkID: "unrelated-chunk", SourceID: "unrelated-source", KnowledgeType: types.KnowledgeTypeFAQ}}
			if failure == "status_commit" {
				require.NoError(t, f.db.Exec("CREATE TRIGGER fail_adoption_index_state BEFORE UPDATE OF status ON chunks WHEN NEW.status=2 BEGIN SELECT RAISE(FAIL,'state write failed'); END").Error)
			} else {
				index.failAfterAppend = true
			}
			got, err := ops.Adopt(f.ctx, 7, run.ID, input)
			require.NoError(t, err)
			require.Equal(t, "failed", got.Status)
			require.Len(t, index.documents, 2)
			for attempt := 0; attempt < 3; attempt++ {
				got, err = ops.RetryAdoption(f.ctx, 7, run.ID, got.ID)
				require.NoError(t, err)
				require.Equal(t, "failed", got.Status)
				require.Len(t, index.documents, 2, "failed recovery must retain one owned vector and the unrelated vector")
			}
			if failure == "status_commit" {
				require.NoError(t, f.db.Exec("DROP TRIGGER fail_adoption_index_state").Error)
			} else {
				index.failAfterAppend = false
			}
			got, err = ops.RetryAdoption(f.ctx, 7, run.ID, got.ID)
			require.NoError(t, err)
			require.Equal(t, "succeeded", got.Status)
			require.Len(t, index.documents, 2)
			require.Equal(t, "unrelated-vector", index.documents[0].ID)
			vectorID := index.documents[1].ID
			index.failDelete = true
			index.failAfterAppend = true
			grant, err := access.ResolveKB(f.ctx, access.KBRequest{Caller: types.CallerFromContext(f.ctx)}, f.kb, types.OrgRoleEditor, nil, nil)
			require.NoError(t, err)
			entry, err := f.svc.CreateOrResumeFAQEntryForAdoption(grant.Context(f.ctx), input.KBID, &types.FAQEntryPayload{StandardQuestion: input.Question, Answers: []string{input.Answer}}, got.ID)
			require.NoError(t, err)
			require.Equal(t, got.EntryID, entry.ID)
			require.Len(t, index.documents, 2)
			require.Equal(t, vectorID, index.documents[1].ID, "already Indexed publication must leave its vectors untouched")
			var tenant types.Tenant
			require.NoError(t, f.db.First(&tenant, 7).Error)
			require.EqualValues(t, 17, tenant.StorageUsed)
			var knowledge types.Knowledge
			require.NoError(t, f.db.First(&knowledge, "id = ?", entry.KnowledgeID).Error)
			require.EqualValues(t, 17, knowledge.StorageSize)
			var count int64
			require.NoError(t, f.db.Model(&types.AuditLog{}).Where("target_id = ?", entry.ChunkID).Count(&count).Error)
			require.EqualValues(t, 1, count)
		})
	}
}
func TestFAQAdoptionIntegrationCleanupFailureStopsBeforeAppendingMoreVectors(t *testing.T) {
	f, ops, index, run, input := newAdoptionServiceIntegration(t)
	index.failAfterAppend = true
	got, err := ops.Adopt(f.ctx, 7, run.ID, input)
	require.NoError(t, err)
	require.Equal(t, "failed", got.Status)
	require.Len(t, index.documents, 1)
	vectorID := index.documents[0].ID
	index.failAfterAppend = false
	index.failDelete = true
	got, err = ops.RetryAdoption(f.ctx, 7, run.ID, got.ID)
	require.NoError(t, err)
	require.Equal(t, "failed", got.Status)
	require.Len(t, index.documents, 1)
	require.Equal(t, vectorID, index.documents[0].ID)
	var tenant types.Tenant
	require.NoError(t, f.db.First(&tenant, 7).Error)
	require.Zero(t, tenant.StorageUsed)
	var count int64
	require.NoError(t, f.db.Model(&types.AuditLog{}).Count(&count).Error)
	require.Zero(t, count)
	index.failDelete = false
	got, err = ops.RetryAdoption(f.ctx, 7, run.ID, got.ID)
	require.NoError(t, err)
	require.Equal(t, "succeeded", got.Status)
	require.Len(t, index.documents, 1)
}

func TestFAQAdoptionIntegrationRefusesFinalizationAfterIndexConfigurationChanges(t *testing.T) {
	for _, change := range []string{"vector_store", "embedding_model", "faq_index_mode", "question_index_mode"} {
		t.Run(change, func(t *testing.T) {
			f, ops, index, run, input := newAdoptionServiceIntegration(t)
			index.onIndex = func() {
				switch change {
				case "vector_store":
					require.NoError(t, f.db.Table("knowledge_bases").Where("id = ?", "kb").UpdateColumn("vector_store_id", "different-store").Error)
				case "embedding_model":
					require.NoError(t, f.db.Model(&types.KnowledgeBase{}).Where("id = ?", "kb").UpdateColumn("embedding_model_id", "different-model").Error)
				default:
					config := *f.kb.FAQConfig
					if change == "faq_index_mode" {
						if config.IndexMode == types.FAQIndexModeQuestionOnly {
							config.IndexMode = types.FAQIndexModeQuestionAnswer
						} else {
							config.IndexMode = types.FAQIndexModeQuestionOnly
						}
					} else {
						if config.QuestionIndexMode == types.FAQQuestionIndexModeSeparate {
							config.QuestionIndexMode = types.FAQQuestionIndexModeCombined
						} else {
							config.QuestionIndexMode = types.FAQQuestionIndexModeSeparate
						}
					}
					encoded, err := json.Marshal(config)
					require.NoError(t, err)
					require.NoError(t, f.db.Model(&types.KnowledgeBase{}).Where("id = ?", "kb").UpdateColumn("faq_config", string(encoded)).Error)
				}
			}
			got, err := ops.Adopt(f.ctx, 7, run.ID, input)
			require.NoError(t, err)
			require.Equal(t, "failed", got.Status)
			var tenant types.Tenant
			require.NoError(t, f.db.First(&tenant, 7).Error)
			require.Zero(t, tenant.StorageUsed)
			var stored types.Chunk
			require.NoError(t, f.db.First(&stored, "status = ?", types.ChunkStatusStored).Error)
			var count int64
			require.NoError(t, f.db.Model(&types.AuditLog{}).Count(&count).Error)
			require.Zero(t, count)
		})
	}
}

type adoptionPersistedKBLookup struct {
	interfaces.KnowledgeBaseService
	db *gorm.DB
}

func (s *adoptionPersistedKBLookup) GetKnowledgeBaseByID(ctx context.Context, id string) (*types.KnowledgeBase, error) {
	var kb types.KnowledgeBase
	if err := s.db.WithContext(ctx).First(&kb, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &kb, nil
}

type adoptionExpectedModelLookup struct{ interfaces.ModelService }

func (s adoptionExpectedModelLookup) GetEmbeddingModel(_ context.Context, id string) (embedding.Embedder, error) {
	if id != "persisted-model" {
		return nil, errors.New("index used a stale embedding model binding")
	}
	return parentChildEmbedder{}, nil
}
func TestFAQAdoptionIntegrationUsesExistingNonDefaultSQLiteTextConfiguration(t *testing.T) {
	f, ops, index, run, input := newAdoptionServiceIntegration(t)
	require.NoError(t, f.db.Table("knowledge_bases").Where("id = ?", "kb").Updates(map[string]any{
		"embedding_model_id": "persisted-model",
		"vector_store_id":    "persisted-store",
		"faq_config":         `{"index_mode":"question_only","question_index_mode":"separate"}`,
	}).Error)
	var storageType string
	require.NoError(t, f.db.Raw("SELECT typeof(faq_config) FROM knowledge_bases WHERE id = ?", "kb").Scan(&storageType).Error)
	require.Equal(t, "text", storageType)
	// Use the actual legacy GORM scanner rather than a pre-populated KB fake.
	f.svc.kbService = &adoptionPersistedKBLookup{db: f.db}
	f.svc.modelService = adoptionExpectedModelLookup{}
	f.svc.retrieveEngine = &fakeFanoutRegistry{byStore: map[string]interfaces.RetrieveEngineService{"persisted-store": index}}
	f.svc.ownership = &fakeOwnership{owned: map[string]uint64{"persisted-store": 7}}
	got, err := ops.Adopt(f.ctx, 7, run.ID, input)
	require.NoError(t, err)
	require.Equal(t, "succeeded", got.Status)
	require.Len(t, index.documents, 1)
	require.Equal(t, "approved question", index.documents[0].Content)
	require.NotContains(t, index.documents[0].Content, "approved answer")
	var ready types.Chunk
	require.NoError(t, f.db.First(&ready, "seq_id = ?", got.EntryID).Error)
	require.Equal(t, "Q: approved question\n", ready.Content)
	require.Equal(t, int(types.ChunkStatusIndexed), ready.Status)
}

func TestFAQAdoptionIntegrationMissingMilvusCollectionCanRecover(t *testing.T) {
	missing := fmt.Errorf("milvus delete: %w", merr.CheckRPCCall(merr.Status(merr.ErrCollectionNotFound), nil))
	require.ErrorIs(t, missing, merr.ErrCollectionNotFound)
	for _, scenario := range []struct {
		name    string
		cleanup error
		success bool
	}{
		{"typed_collection_missing", missing, true},
		{"permission_denied", fmt.Errorf("milvus delete: %w", merr.ErrPrivilegeNotPermitted), false},
		{"network_unavailable", &net.OpError{Op: "delete", Net: "tcp", Err: errors.New("connection reset")}, false},
		{"untyped_collection_missing", errors.New("collection not found"), false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			f, ops, index, run, input := newAdoptionServiceIntegration(t)
			// First embedding/index failure occurs before the backend creates any
			// collection. It leaves one owned Stored row and no physical vectors.
			index.fail = true
			got, err := ops.Adopt(f.ctx, 7, run.ID, input)
			require.NoError(t, err)
			require.Equal(t, "failed", got.Status)
			require.Empty(t, index.documents)
			var stored types.Chunk
			require.NoError(t, f.db.First(&stored, "status = ?", types.ChunkStatusStored).Error)
			index.fail = false
			index.deleteErr = scenario.cleanup
			got, err = ops.RetryAdoption(f.ctx, 7, run.ID, got.ID)
			require.NoError(t, err)
			var tenant types.Tenant
			require.NoError(t, f.db.First(&tenant, 7).Error)
			var audits int64
			require.NoError(t, f.db.Model(&types.AuditLog{}).Where("target_id = ?", stored.ID).Count(&audits).Error)
			if scenario.success {
				require.Equal(t, "succeeded", got.Status)
				require.Equal(t, stored.SeqID, got.EntryID)
				require.Len(t, index.documents, 1)
				require.EqualValues(t, 17, tenant.StorageUsed)
				require.EqualValues(t, 1, audits)
			} else {
				require.Equal(t, "failed", got.Status)
				require.Empty(t, index.documents)
				require.Zero(t, tenant.StorageUsed)
				require.Zero(t, audits)
			}
		})
	}
}

type adoptionIndexByKind struct {
	*adoptionTestIndex
	kind types.RetrieverEngineType
}

func (e *adoptionIndexByKind) EngineType() types.RetrieverEngineType { return e.kind }

type adoptionIndexRegistryByKind struct {
	interfaces.RetrieveEngineRegistry
	engines map[types.RetrieverEngineType]interfaces.RetrieveEngineService
}

func (r *adoptionIndexRegistryByKind) GetRetrieveEngineService(kind types.RetrieverEngineType) (interfaces.RetrieveEngineService, error) {
	return r.engines[kind], nil
}
func TestFAQAdoptionIntegrationMissingCollectionDoesNotHideAnotherBackendFailure(t *testing.T) {
	f, ops, first, run, input := newAdoptionServiceIntegration(t)
	second := &adoptionTestIndex{}
	require.NoError(t, f.db.Table("knowledge_bases").Where("id = ?", "kb").UpdateColumn("vector_store_id", nil).Error)
	engines := []types.RetrieverEngineParams{
		{RetrieverEngineType: types.MilvusRetrieverEngineType, RetrieverType: types.VectorRetrieverType},
		{RetrieverEngineType: types.PostgresRetrieverEngineType, RetrieverType: types.VectorRetrieverType},
	}
	f.ctx = context.WithValue(f.ctx, types.TenantInfoContextKey, &types.Tenant{ID: 7, StorageQuota: 1000, RetrieverEngines: types.RetrieverEngines{Engines: engines}})
	f.svc.retrieveEngine = &adoptionIndexRegistryByKind{engines: map[types.RetrieverEngineType]interfaces.RetrieveEngineService{
		types.MilvusRetrieverEngineType:   &adoptionIndexByKind{adoptionTestIndex: first, kind: types.MilvusRetrieverEngineType},
		types.PostgresRetrieverEngineType: &adoptionIndexByKind{adoptionTestIndex: second, kind: types.PostgresRetrieverEngineType},
	}}
	first.fail = true
	second.fail = true
	got, err := ops.Adopt(f.ctx, 7, run.ID, input)
	require.NoError(t, err)
	require.Equal(t, "failed", got.Status)
	first.fail = false
	second.fail = false
	first.deleteErr = fmt.Errorf("milvus delete: %w", merr.ErrCollectionNotFound)
	second.deleteErr = merr.ErrPrivilegeNotPermitted
	got, err = ops.RetryAdoption(f.ctx, 7, run.ID, got.ID)
	require.NoError(t, err)
	require.Equal(t, "failed", got.Status)
	require.Empty(t, first.documents)
	require.Empty(t, second.documents)
	var tenant types.Tenant
	require.NoError(t, f.db.First(&tenant, 7).Error)
	require.Zero(t, tenant.StorageUsed)
}
