package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type folderSummaryChatBase = chat.Chat
type folderSummaryTextModel struct{ folderSummaryChatBase }

func (folderSummaryTextModel) Chat(context.Context, []chat.Message, *chat.ChatOptions) (*types.ChatResponse, error) {
	return &types.ChatResponse{Content: "A navigation overview without model-generated links."}, nil
}

func TestFolderSummaryProjectionPersistsKnowledgeBaseTenant(t *testing.T) {
	ctx, h, db := newWikiRevisionTestService(t)
	svc := &folderSummaryWikiService{wikiService: h.svc, repo: &fakeFolderCountsRepo{}}
	kb := &types.KnowledgeBase{ID: "kb", TenantID: 7}
	require.NoError(t, svc.projectPage(ctx, 7, kb, &folderNode{Path: "A", Slug: "folder_summary/a"}, "summary", &types.FolderSummarySnapshot{}, nil, false))
	var page types.WikiPage
	require.NoError(t, db.First(&page, "slug = ?", "folder_summary/a").Error)
	require.EqualValues(t, 7, page.TenantID)
	require.Equal(t, "kb", page.KnowledgeBaseID)
	require.Contains(t, page.Content, "summary")
}

func TestFolderSummaryProjectionRejectsWrongOwnerBeforeWriting(t *testing.T) {
	for _, owner := range []uint64{0, 8} {
		t.Run(fmt.Sprintf("owner=%d", owner), func(t *testing.T) {
			ctx, h, db := newWikiRevisionTestService(t)
			svc := &folderSummaryWikiService{wikiService: h.svc, repo: &fakeFolderCountsRepo{}}
			require.Error(t, svc.projectPage(ctx, 7, &types.KnowledgeBase{ID: "kb", TenantID: owner}, &folderNode{Slug: "folder_summary/a"}, "summary", nil, nil, false))
			var count int64
			require.NoError(t, db.Model(&types.WikiPage{}).Count(&count).Error)
			require.Zero(t, count)
		})
	}
}

func TestFolderRootMapPersistsOwnerAndDeterministicSourceLinks(t *testing.T) {
	ctx, h, db := newWikiRevisionTestService(t)
	_, err := h.svc.CreatePage(ctx, &types.WikiPage{TenantID: 7, KnowledgeBaseID: "kb", Slug: "folder_summary/a", PageType: types.WikiPageTypeFolderSummary, Content: "child"})
	require.NoError(t, err)
	svc := &folderSummaryWikiService{wikiService: h.svc}
	require.NoError(t, svc.generateRootMap(ctx, folderSummaryTextModel{}, &types.KnowledgeBase{ID: "kb", TenantID: 7}, []*folderNode{{Path: "A", Slug: "folder_summary/a"}}))
	var page types.WikiPage
	require.NoError(t, db.First(&page, "slug = ?", "folder_summary/index").Error)
	require.EqualValues(t, 7, page.TenantID)
	require.Contains(t, page.OutLinks, "folder_summary/a", "root-map sources cannot depend on whether the model wrote a link")
	require.Contains(t, page.Content, "[[folder_summary/a|A]]")
}
