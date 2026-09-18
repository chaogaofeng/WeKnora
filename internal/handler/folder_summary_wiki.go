package handler

// [folder-summary] REST endpoints for the folder-summary wiki projection.

import (
	"net/http"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/gin-gonic/gin"
)

type FolderSummaryWikiHandler struct {
	folderSummaryService service.FolderSummaryWikiService
}

func NewFolderSummaryWikiHandler(
	folderSummaryService service.FolderSummaryWikiService,
) *FolderSummaryWikiHandler {
	return &FolderSummaryWikiHandler{
		folderSummaryService: folderSummaryService,
	}
}

type folderSummaryRefreshRequest struct {
	// Path scopes the refresh to a subtree; "" = whole KB.
	Path string `json:"path"`
}

// Generate enqueues a full folder-summary generation pass.
func (h *FolderSummaryWikiHandler) Generate(c *gin.Context) {
	ctx := c.Request.Context()
	kbID := c.Param("id")
	if kbID == "" {
		c.Error(errors.NewBadRequestError("Knowledge base ID is required"))
		return
	}
	if err := h.folderSummaryService.Generate(ctx, kbID); err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	c.JSON(http.StatusAccepted, gin.H{
		"success": true,
		"message": "folder summary generation scheduled",
	})
}

// Refresh enqueues a subtree refresh for one folder path.
func (h *FolderSummaryWikiHandler) Refresh(c *gin.Context) {
	ctx := c.Request.Context()
	kbID := c.Param("id")
	if kbID == "" {
		c.Error(errors.NewBadRequestError("Knowledge base ID is required"))
		return
	}
	var req folderSummaryRefreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	if err := h.folderSummaryService.Refresh(ctx, kbID, req.Path); err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	c.JSON(http.StatusAccepted, gin.H{
		"success": true,
		"message": "folder summary refresh scheduled",
	})
}

// ListTree returns the folder list with each folder's projection state.
func (h *FolderSummaryWikiHandler) ListTree(c *gin.Context) {
	ctx := c.Request.Context()
	kbID := c.Param("id")
	if kbID == "" {
		c.Error(errors.NewBadRequestError("Knowledge base ID is required"))
		return
	}
	tree, err := h.folderSummaryService.ListTree(ctx, kbID)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": tree})
}
