package service

import (
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	werrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestFAQAdoptionIntegrationFailedFinalizationKeepsVectorsUnsearchable(t *testing.T) {
	for _, failure := range []string{"audit", "quota", "configuration", "indexed_state"} {
		t.Run(failure, func(t *testing.T) {
			f, ops, index, run, input := newAdoptionServiceIntegration(t)
			originalConfig, err := json.Marshal(f.kb.FAQConfig)
			require.NoError(t, err)
			switch failure {
			case "audit":
				require.NoError(t, f.db.Exec("CREATE TRIGGER fail_adoption_search_audit BEFORE INSERT ON audit_logs BEGIN SELECT RAISE(FAIL,'audit unavailable'); END").Error)
			case "indexed_state":
				require.NoError(t, f.db.Exec("CREATE TRIGGER fail_adoption_search_state BEFORE UPDATE OF status ON chunks WHEN NEW.status=2 BEGIN SELECT RAISE(FAIL,'state write failed'); END").Error)
			default:
				index.onIndex = func() {
					if failure == "quota" {
						require.NoError(t, f.db.Model(&types.Tenant{}).Where("id = ?", 7).UpdateColumn("storage_used", 990).Error)
					} else {
						config := *f.kb.FAQConfig
						if config.IndexMode == types.FAQIndexModeQuestionOnly {
							config.IndexMode = types.FAQIndexModeQuestionAnswer
						} else {
							config.IndexMode = types.FAQIndexModeQuestionOnly
						}
						encoded, err := json.Marshal(config)
						require.NoError(t, err)
						require.NoError(t, f.db.Table("knowledge_bases").Where("id = ?", "kb").UpdateColumn("faq_config", string(encoded)).Error)
					}
				}
			}
			got, err := ops.Adopt(f.ctx, 7, run.ID, input)
			require.NoError(t, err)
			require.Equal(t, "failed", got.Status)
			require.Len(t, index.documents, 1, "index succeeded before finalization failed")
			var pending types.Chunk
			require.NoError(t, f.db.First(&pending, "id = ?", index.documents[0].ChunkID).Error)
			require.Equal(t, int(types.ChunkStatusStored), pending.Status)
			search := &knowledgeBaseService{}
			require.False(t, search.isSearchableChunk(&pending), "retained vector hits must not expose a failed adoption")
			require.Equal(t, "processing", pending.IndexStatus)
			switch failure {
			case "audit":
				require.NoError(t, f.db.Exec("DROP TRIGGER fail_adoption_search_audit").Error)
			case "indexed_state":
				require.NoError(t, f.db.Exec("DROP TRIGGER fail_adoption_search_state").Error)
			case "quota":
				require.NoError(t, f.db.Model(&types.Tenant{}).Where("id = ?", 7).UpdateColumn("storage_used", 0).Error)
			case "configuration":
				require.NoError(t, f.db.Table("knowledge_bases").Where("id = ?", "kb").UpdateColumn("faq_config", string(originalConfig)).Error)
			}
			index.onIndex = nil
			got, err = ops.RetryAdoption(f.ctx, 7, run.ID, got.ID)
			require.NoError(t, err)
			require.Equal(t, "succeeded", got.Status)
			require.Len(t, index.documents, 1)
			var published types.Chunk
			require.NoError(t, f.db.First(&published, "id = ?", pending.ID).Error)
			require.Equal(t, int(types.ChunkStatusIndexed), published.Status)
			require.Equal(t, "ready", published.IndexStatus)
			require.True(t, search.isSearchableChunk(&published))
			var tenant types.Tenant
			require.NoError(t, f.db.First(&tenant, 7).Error)
			require.EqualValues(t, 17, tenant.StorageUsed)
			var audits int64
			require.NoError(t, f.db.Model(&types.AuditLog{}).Where("target_id = ?", pending.ID).Count(&audits).Error)
			require.EqualValues(t, 1, audits)
		})
	}
}

func TestFAQAdoptionIntegrationResumingLegacyStoredChunkClosesSearchGateBeforeReindex(t *testing.T) {
	for _, legacyStatus := range []string{"ready", ""} {
		t.Run("legacy_"+legacyStatus, func(t *testing.T) {
			f, ops, index, run, input := newAdoptionServiceIntegration(t)
			index.failAfterAppend = true
			first, err := ops.Adopt(f.ctx, 7, run.ID, input)
			require.NoError(t, err)
			require.Equal(t, "failed", first.Status)
			var pending types.Chunk
			require.NoError(t, f.db.First(&pending, "status = ?", types.ChunkStatusStored).Error)
			require.NoError(t, f.db.Model(&types.Chunk{}).Where("id = ?", pending.ID).UpdateColumn("index_status", legacyStatus).Error)
			index.failAfterAppend = false
			var during types.Chunk
			var readErr error
			index.onIndex = func() { readErr = f.db.First(&during, "id = ?", pending.ID).Error }
			got, err := ops.RetryAdoption(f.ctx, 7, run.ID, first.ID)
			require.NoError(t, err)
			require.NoError(t, readErr)
			search := &knowledgeBaseService{}
			require.False(t, search.isSearchableChunk(&during), "recovery must hide a legacy draft before touching its vectors")
			require.Equal(t, "processing", during.IndexStatus)
			require.Equal(t, "succeeded", got.Status)
			var published types.Chunk
			require.NoError(t, f.db.First(&published, "id = ?", pending.ID).Error)
			require.True(t, search.isSearchableChunk(&published))
			require.Equal(t, "ready", published.IndexStatus)
		})
	}
}

func TestFAQAdoptionIntegrationFinalizationDoesNotOverwriteConcurrentIndexStateOrRevision(t *testing.T) {
	for _, field := range []string{"index_status", "content_revision"} {
		t.Run(field, func(t *testing.T) {
			f, ops, index, run, input := newAdoptionServiceIntegration(t)
			var value any = "failed"
			if field == "content_revision" {
				value = 1
			}
			index.onIndex = func() {
				require.NoError(t, f.db.Model(&types.Chunk{}).Where("status = ?", types.ChunkStatusStored).UpdateColumn(field, value).Error)
			}
			got, err := ops.Adopt(f.ctx, 7, run.ID, input)
			require.NoError(t, err)
			require.Equal(t, "failed", got.Status)
			require.Len(t, index.documents, 1)
			var pending types.Chunk
			require.NoError(t, f.db.First(&pending, "id = ?", index.documents[0].ChunkID).Error)
			require.Equal(t, int(types.ChunkStatusStored), pending.Status)
			if field == "index_status" {
				require.Equal(t, "failed", pending.IndexStatus)
			} else {
				require.Equal(t, 1, pending.ContentRevision)
			}
			search := &knowledgeBaseService{}
			require.False(t, search.isSearchableChunk(&pending))
			var tenant types.Tenant
			require.NoError(t, f.db.First(&tenant, 7).Error)
			require.Zero(t, tenant.StorageUsed)
			var audits int64
			require.NoError(t, f.db.Model(&types.AuditLog{}).Count(&audits).Error)
			require.Zero(t, audits)
		})
	}
}

func TestFAQAdoptionIntegrationIndexedFastReturnRequiresSearchableIndexState(t *testing.T) {
	for _, status := range []string{"processing", "failed", "ready", ""} {
		t.Run("state_"+status, func(t *testing.T) {
			f, ops, index, run, input := newAdoptionServiceIntegration(t)
			published, err := ops.Adopt(f.ctx, 7, run.ID, input)
			require.NoError(t, err)
			require.Equal(t, "succeeded", published.Status)
			require.Len(t, index.documents, 1)
			vectorID := index.documents[0].ID
			require.NoError(t, f.db.Model(&types.Chunk{}).Where("seq_id = ?", published.EntryID).UpdateColumn("index_status", status).Error)
			index.failDelete = true
			index.failAfterAppend = true
			grant, err := access.ResolveKB(f.ctx, access.KBRequest{Caller: types.CallerFromContext(f.ctx)}, f.kb, types.OrgRoleEditor, nil, nil)
			require.NoError(t, err)
			entry, err := f.svc.CreateOrResumeFAQEntryForAdoption(grant.Context(f.ctx), input.KBID, &types.FAQEntryPayload{StandardQuestion: input.Question, Answers: []string{input.Answer}}, published.ID)
			if status == "processing" || status == "failed" {
				var appErr *werrors.AppError
				require.ErrorAs(t, err, &appErr)
				require.Equal(t, werrors.ErrConflict, appErr.Code)
				require.Nil(t, entry)
			} else {
				require.NoError(t, err)
				require.Equal(t, published.EntryID, entry.ID)
			}
			var current types.Chunk
			require.NoError(t, f.db.First(&current, "seq_id = ?", published.EntryID).Error)
			require.Equal(t, status, current.IndexStatus)
			require.Equal(t, int(types.ChunkStatusIndexed), current.Status)
			require.Len(t, index.documents, 1)
			require.Equal(t, vectorID, index.documents[0].ID)
			var tenant types.Tenant
			require.NoError(t, f.db.First(&tenant, 7).Error)
			require.EqualValues(t, 17, tenant.StorageUsed)
			var audits int64
			require.NoError(t, f.db.Model(&types.AuditLog{}).Count(&audits).Error)
			require.EqualValues(t, 1, audits)
		})
	}
}
