package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/utils"
	sdk "github.com/weaviate/weaviate-go-client/v5/weaviate"
	"github.com/weaviate/weaviate-go-client/v5/weaviate/fault"
	"gorm.io/gorm"
)

// A missing Weaviate class produces a 422 on batch deletion, which is also
// used for invalid filters. Neither that status nor error message proves the
// class is absent. Probe only these structured responses, never transport or
// permission failures, and require an independent schema GET to return 404.
func faqAdoptionWeaviateCleanupNeedsProbe(kind types.RetrieverEngineType, err error) bool {
	var clientErr *fault.WeaviateClientError
	return kind == types.WeaviateRetrieverEngineType && errors.As(err, &clientErr) && clientErr.IsUnexpectedStatusCode && (clientErr.StatusCode == http.StatusNotFound || clientErr.StatusCode == http.StatusUnprocessableEntity)
}

func faqAdoptionWeaviateCollectionMissing(ctx context.Context, db *gorm.DB, tenant uint64, storeID string, dimension int) (bool, error) {
	store := types.BuildEnvVectorStores("weaviate", os.Getenv)[0]
	if storeID != "" && !types.IsEnvStoreID(storeID) {
		store = types.VectorStore{}
		// Read the same tenant-owned, immutable binding used to build the engine.
		// Raw JSON bytes support legacy SQLite TEXT configurations as well.
		var row struct {
			ConnectionConfig []byte
			IndexConfig      []byte
		}
		if err := db.WithContext(ctx).Model(&types.VectorStore{}).Select("connection_config", "index_config").Where("id = ? AND tenant_id = ? AND engine_type = ?", storeID, tenant, types.WeaviateRetrieverEngineType).Take(&row).Error; err != nil {
			return false, err
		}
		if err := store.ConnectionConfig.Scan(row.ConnectionConfig); err != nil {
			return false, err
		}
		if err := store.IndexConfig.Scan(row.IndexConfig); err != nil {
			return false, err
		}
	} else {
		// Match the env engine's opt-in authentication behavior.
		if !strings.EqualFold(strings.TrimSpace(os.Getenv("WEAVIATE_AUTH_ENABLED")), "true") {
			store.ConnectionConfig.APIKey = ""
		} else {
			store.ConnectionConfig.APIKey = strings.TrimSpace(store.ConnectionConfig.APIKey)
		}
	}
	cc := store.ConnectionConfig
	if cc.Host == "" {
		cc.Host = "weaviate:8080"
	}
	if cc.Scheme == "" {
		cc.Scheme = "http"
	}
	headers := map[string]string{}
	if cc.APIKey != "" {
		headers["Authorization"] = "Bearer " + cc.APIKey
	}
	client, err := sdk.NewClient(sdk.Config{
		Host: cc.Host, Scheme: cc.Scheme, Headers: headers,
		ConnectionClient: utils.NewSSRFSafeHTTPClient(utils.DefaultSSRFSafeHTTPClientConfig()),
	})
	if err != nil {
		return false, err
	}
	base := types.ResolveCollectionName(&store.IndexConfig, "WEAVIATE_COLLECTION", "Weknora_embeddings")
	// ClassExistenceChecker treats every non-200, including 403, as absent.
	// ClassGetter preserves the typed HTTP status needed for this decision.
	_, err = client.Schema().ClassGetter().WithClassName(fmt.Sprintf("%s_%d", base, dimension)).Do(ctx)
	var clientErr *fault.WeaviateClientError
	if errors.As(err, &clientErr) && clientErr.IsUnexpectedStatusCode && clientErr.StatusCode == http.StatusNotFound {
		return true, nil
	}
	return false, err
}
