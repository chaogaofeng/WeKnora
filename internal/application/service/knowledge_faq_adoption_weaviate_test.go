package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	weaviateRepo "github.com/Tencent/WeKnora/internal/application/repository/retriever/weaviate"
	"github.com/Tencent/WeKnora/internal/application/service/retriever"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/utils"
	"github.com/stretchr/testify/require"
	sdk "github.com/weaviate/weaviate-go-client/v5/weaviate"
)

type adoptionWeaviateEmbedder struct {
	parentChildEmbedder
	cancel context.CancelFunc
}

func (e *adoptionWeaviateEmbedder) BatchEmbedWithPool(ctx context.Context, model embedding.Embedder, content []string) ([][]float32, error) {
	if e.cancel != nil {
		e.cancel()
		return nil, errors.New("embedding unavailable before collection creation")
	}
	return e.parentChildEmbedder.BatchEmbedWithPool(ctx, model, content)
}

// Keep the real hybrid indexer, repository, and SDK. Only the external HTTP
// server and unavailable embedding provider are replaced, so an absent class
// has the same 422 delete / 404 schema behavior as Weaviate itself.
func TestFAQAdoptionIntegrationWeaviateRecoveryRequiresConfirmedMissingCollection(t *testing.T) {
	for _, scenario := range []struct {
		name         string
		env          bool
		deleteStatus int
		probeStatus  int
		success      bool
	}{
		{"bound_missing_collection", false, 422, 404, true},
		{"bound_delete_not_found", false, 404, 404, true},
		{"env_missing_collection", true, 422, 404, true},
		{"delete_permission_denied", false, 403, 404, false},
		{"delete_network_failure", false, 0, 404, false},
		{"probe_permission_denied", false, 422, 403, false},
		{"probe_network_failure", false, 422, 0, false},
		{"probe_server_failure", false, 422, 503, false},
		{"collection_exists", false, 422, 200, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			f, ops, _, run, input := newAdoptionServiceIntegration(t)
			t.Setenv("SSRF_WHITELIST", "127.0.0.1")
			t.Setenv("WEAVIATE_API_KEY", "unrelated-env-key")
			utils.ResetSSRFWhitelistForTest()
			t.Cleanup(utils.ResetSSRFWhitelistForTest)
			wantAuth := "Bearer bound-key"
			if scenario.env {
				wantAuth = ""
			}
			var mu sync.Mutex
			exists, created, writes := false, 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				if r.Header.Get("Authorization") != wantAuth {
					t.Errorf("unexpected Weaviate authentication for %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusForbidden)
					return
				}
				switch {
				case r.URL.Path == "/v1/meta":
					_, _ = w.Write([]byte(`{"version":"1.30.0"}`))
				case r.Method == http.MethodDelete && r.URL.Path == "/v1/batch/objects":
					if scenario.deleteStatus == 0 {
						panic(http.ErrAbortHandler)
					}
					w.WriteHeader(scenario.deleteStatus)
					_, _ = w.Write([]byte(`{"error":[{"message":"validate: failed to get class: Adoption_1"}]}`))
				case r.Method == http.MethodGet && r.URL.Path == "/v1/schema/Adoption_1":
					if exists {
						_, _ = w.Write([]byte(`{"class":"Adoption_1"}`))
						return
					}
					if scenario.probeStatus == 0 {
						panic(http.ErrAbortHandler)
					}
					w.WriteHeader(scenario.probeStatus)
					if scenario.probeStatus == 200 {
						_, _ = w.Write([]byte(`{"class":"Adoption_1"}`))
					} else {
						_, _ = w.Write([]byte(`{"error":[{"message":"schema request failed"}]}`))
					}
				case r.Method == http.MethodPost && r.URL.Path == "/v1/schema":
					var body struct{ Class string }
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Class != "Adoption_1" {
						t.Errorf("unexpected collection creation: %#v, %v", body, err)
						w.WriteHeader(400)
						return
					}
					exists = true
					created++
					_, _ = w.Write([]byte(`{"class":"Adoption_1"}`))
				case r.Method == http.MethodPost && r.URL.Path == "/v1/batch/objects":
					writes++
					_, _ = w.Write([]byte(`[]`))
				default:
					t.Errorf("unexpected Weaviate request: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			t.Cleanup(server.Close)
			host := strings.TrimPrefix(server.URL, "http://")
			store := types.VectorStore{ID: *f.kb.VectorStoreID, TenantID: 7, Name: "adoption", EngineType: types.WeaviateRetrieverEngineType, ConnectionConfig: types.ConnectionConfig{Host: host, Scheme: "http", APIKey: "bound-key"}, IndexConfig: types.IndexConfig{CollectionPrefix: "Adoption"}}
			require.NoError(t, f.db.AutoMigrate(&types.VectorStore{}))
			require.NoError(t, f.db.Create(&store).Error)
			// SQLite deployments can retain JSON as TEXT rather than a BLOB;
			// the private probe must still use the stored endpoint and credential.
			require.NoError(t, f.db.Exec("UPDATE vector_stores SET connection_config=CAST(connection_config AS TEXT),index_config=CAST(index_config AS TEXT) WHERE id = ?", store.ID).Error)
			headers := map[string]string{}
			if wantAuth != "" {
				headers["Authorization"] = wantAuth
			}
			client, err := sdk.NewClient(sdk.Config{Scheme: "http", Host: host, Headers: headers})
			require.NoError(t, err)
			index := retriever.NewKVHybridRetrieveEngine(weaviateRepo.NewWeaviateRetrieveEngineRepository(client, &store.IndexConfig), types.WeaviateRetrieverEngineType)
			if scenario.env {
				t.Setenv("WEAVIATE_HOST", host)
				t.Setenv("WEAVIATE_SCHEME", "http")
				t.Setenv("WEAVIATE_COLLECTION", "Adoption")
				t.Setenv("WEAVIATE_AUTH_ENABLED", "false")
				require.NoError(t, f.db.Table("knowledge_bases").Where("id = ?", "kb").UpdateColumn("vector_store_id", nil).Error)
				f.ctx = context.WithValue(f.ctx, types.TenantInfoContextKey, &types.Tenant{ID: 7, RetrieverEngines: types.RetrieverEngines{Engines: []types.RetrieverEngineParams{{RetrieverEngineType: types.WeaviateRetrieverEngineType, RetrieverType: types.VectorRetrieverType}}}})
				f.svc.retrieveEngine = &adoptionIndexRegistryByKind{engines: map[types.RetrieverEngineType]interfaces.RetrieveEngineService{types.WeaviateRetrieverEngineType: index}}
			} else {
				f.svc.retrieveEngine = &fakeFanoutRegistry{byStore: map[string]interfaces.RetrieveEngineService{store.ID: index}}
			}
			firstCtx, cancel := context.WithCancel(f.ctx)
			defer cancel()
			embedder := &adoptionWeaviateEmbedder{cancel: cancel}
			f.svc.modelService = parentChildModelService{embedder: embedder}
			first, err := ops.Adopt(firstCtx, 7, run.ID, input)
			require.NoError(t, err)
			require.Equal(t, "failed", first.Status)
			var stored types.Chunk
			require.NoError(t, f.db.First(&stored, "status = ?", types.ChunkStatusStored).Error)
			embedder.cancel = nil
			got, err := ops.RetryAdoption(f.ctx, 7, run.ID, first.ID)
			require.NoError(t, err)
			var current types.Chunk
			require.NoError(t, f.db.First(&current, "id = ?", stored.ID).Error)
			var tenant types.Tenant
			require.NoError(t, f.db.First(&tenant, 7).Error)
			var audits int64
			require.NoError(t, f.db.Model(&types.AuditLog{}).Where("target_id = ?", stored.ID).Count(&audits).Error)
			mu.Lock()
			defer mu.Unlock()
			if scenario.success {
				require.Equal(t, "succeeded", got.Status)
				require.Equal(t, stored.SeqID, got.EntryID)
				require.Equal(t, int(types.ChunkStatusIndexed), current.Status)
				require.Equal(t, 1, created)
				require.Equal(t, 1, writes)
				require.Positive(t, tenant.StorageUsed)
				require.EqualValues(t, 1, audits)
			} else {
				require.Equal(t, "failed", got.Status)
				require.Equal(t, int(types.ChunkStatusStored), current.Status)
				require.Zero(t, created)
				require.Zero(t, writes)
				require.Zero(t, tenant.StorageUsed)
				require.Zero(t, audits)
			}
		})
	}
}
