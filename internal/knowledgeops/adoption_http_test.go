package knowledgeops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func adoptionRouter(svc *Service, ctx context.Context) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Request = c.Request.WithContext(ctx) })
	svc.RegisterHTTP(router.Group("/knowledge-ops"))
	return router
}
func adoptionRequest(t *testing.T, router *gin.Engine, method, path string, body any) (int, map[string]any) {
	t.Helper()
	data, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(method, path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	result := httptest.NewRecorder()
	router.ServeHTTP(result, req)
	var response map[string]any
	require.NoError(t, json.Unmarshal(result.Body.Bytes(), &response))
	return result.Code, response
}

// A machine verdict cannot authorize the human publication endpoint.
func TestAdoptionHTTPRequiresAnExplicitHumanAcceptance(t *testing.T) {
	svc, _, ctx, run, a, r := adoptionFixture(t)
	require.NoError(t, svc.repo.db.Delete(&r).Error)
	require.NoError(t, svc.repo.db.Model(&a).Update("report", `{"verdict":"supported"}`).Error)
	status, response := adoptionRequest(t, adoptionRouter(svc, ctx), http.MethodPost, "/knowledge-ops/runs/"+run.ID+"/adoptions", adoptionInput(a, r))
	require.Equal(t, http.StatusBadRequest, status)
	require.Equal(t, "invalid_input", response["error"])
}
func TestAdoptionHTTPSeparatesPublicationFailureAndReviewSuccess(t *testing.T) {
	svc, faq, ctx, run, a, r := adoptionFixture(t)
	faq.fail = errors.New("index unavailable")
	router := adoptionRouter(svc, ctx)
	status, response := adoptionRequest(t, router, http.MethodPost, "/knowledge-ops/runs/"+run.ID+"/adoptions", adoptionInput(a, r))
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, true, response["success"])
	data := response["data"].(map[string]any)
	require.Equal(t, "failed", data["status"])
	require.Equal(t, r.ID, data["review_id"])
	faq.fail = nil
	status, response = adoptionRequest(t, router, http.MethodPost, "/knowledge-ops/runs/"+run.ID+"/adoptions/"+data["id"].(string)+"/retry", nil)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, "succeeded", response["data"].(map[string]any)["status"])
	status, response = adoptionRequest(t, router, http.MethodGet, "/knowledge-ops/adoption-targets", nil)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, []any{map[string]any{"id": "faq", "name": "FAQ"}}, response["data"].(map[string]any)["items"])
}

func TestAdoptionHTTPRejectsUncleanedAnswerCitations(t *testing.T) {
	svc, _, ctx, run, analysis, review := adoptionFixture(t)
	input := adoptionInput(analysis, review)
	input.Answer = `Approved answer <kb doc="Guide" chunk_id="chunk" />`
	status, response := adoptionRequest(t, adoptionRouter(svc, ctx), http.MethodPost, "/knowledge-ops/runs/"+run.ID+"/adoptions", input)
	require.Equal(t, http.StatusBadRequest, status)
	require.Equal(t, "invalid_input", response["error"])
	var count int64
	require.NoError(t, svc.repo.db.Model(&Adoption{}).Count(&count).Error)
	require.Zero(t, count)
}
