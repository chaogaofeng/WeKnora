package service

// [folder-summary] Folder-summary wiki projection service.
//
// Generates hierarchical summaries of the knowledge base's document folders
// (aggregating folder_path — main's native model, no separate folder tables)
// and projects them into the wiki as read-only navigation pages:
//
//	folder_summary/<slugified-path>   per-folder summary (leaf + parent layers)
//	folder_summary/index              the KB-wide map page
//
// Generation runs bottom-up: leaf folders summarize their direct files;
// parent folders aggregate their own files plus child-folder summaries; the
// root map page aggregates top-level folders. Each page carries a template
// built link listing (documents link to their summary/<id> pages, child
// folders to their folder_summary/<slug> pages) plus an LLM-written overview.
// Listings are template-assembled so they cannot hallucinate; the LLM only
// writes the prose.
//
// Retrieval: pages participate in wiki_search (lexical) and wiki_read_page
// navigation like any other wiki page. No vector chunk is created.
//
// Sync policy: manual (generate/refresh APIs); stale detection compares the
// folder's max(updated_at)/file count against the projection page. Pages a
// user hand-edited (PageMetadata folder_summary_manual_edit) are skipped.

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
)

// FolderSummaryWikiService is the public contract (also used by the handler).
type FolderSummaryWikiService interface {
	// Generate enqueues a full generation pass for the KB.
	Generate(ctx context.Context, kbID string) error
	// Refresh enqueues a subtree refresh starting at the given folder path
	// ("" refreshes everything, i.e. behaves like Generate).
	Refresh(ctx context.Context, kbID string, folderPath string) error
	// ListTree returns the folder hierarchy with each folder's projection
	// page state (missing / fresh / stale / manual_edit) for the UI panel.
	ListTree(ctx context.Context, kbID string) (*types.FolderSummaryTree, error)
	// Handle is the asynq worker entry.
	Handle(ctx context.Context, t *asynq.Task) error
}

type folderSummaryWikiService struct {
	repo        interfaces.KnowledgeRepository
	kbService   interfaces.KnowledgeBaseService
	wikiService interfaces.WikiPageService
	modelSvc    interfaces.ModelService
	tenantRepo  interfaces.TenantRepository
	task        interfaces.TaskEnqueuer
}

// NewFolderSummaryWikiService builds the service. wikiService may be nil on
// non-wiki deployments — generate then degrades to a no-op with a log line.
func NewFolderSummaryWikiService(
	repo interfaces.KnowledgeRepository,
	kbService interfaces.KnowledgeBaseService,
	wikiService interfaces.WikiPageService,
	modelSvc interfaces.ModelService,
	tenantRepo interfaces.TenantRepository,
	task interfaces.TaskEnqueuer,
) FolderSummaryWikiService {
	return &folderSummaryWikiService{
		repo:        repo,
		kbService:   kbService,
		wikiService: wikiService,
		modelSvc:    modelSvc,
		tenantRepo:  tenantRepo,
		task:        task,
	}
}

// folderSummaryPayload is the asynq task payload.
type folderSummaryPayload struct {
	TenantID        uint64 `json:"tenant_id"`
	KnowledgeBaseID string `json:"knowledge_base_id"`
	// RootPath scopes the pass to a subtree ("" = whole KB).
	RootPath string `json:"root_path,omitempty"`
}

// ---------------------------------------------------------------------------
// API entry points
// ---------------------------------------------------------------------------

func (s *folderSummaryWikiService) Generate(ctx context.Context, kbID string) error {
	return s.enqueue(ctx, kbID, "")
}

func (s *folderSummaryWikiService) Refresh(ctx context.Context, kbID string, folderPath string) error {
	return s.enqueue(ctx, kbID, folderPath)
}

func (s *folderSummaryWikiService) enqueue(ctx context.Context, kbID, rootPath string) error {
	tenantID, _ := ctx.Value(types.TenantIDContextKey).(uint64)
	if tenantID == 0 {
		return fmt.Errorf("tenant context missing")
	}
	payload, err := json.Marshal(folderSummaryPayload{
		TenantID:        tenantID,
		KnowledgeBaseID: kbID,
		RootPath:        rootPath,
	})
	if err != nil {
		return err
	}
	_, err = s.task.Enqueue(asynq.NewTask(types.TypeFolderSummaryGenerate, payload),
		asynq.MaxRetry(2),
		asynq.Timeout(60*time.Minute),
		asynq.Queue(types.QueueSummary),
	)
	return err
}

// ---------------------------------------------------------------------------
// Worker
// ---------------------------------------------------------------------------

func (s *folderSummaryWikiService) Handle(ctx context.Context, t *asynq.Task) error {
	var p folderSummaryPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("folder summary: bad payload: %w", err)
	}
	if s.wikiService == nil {
		logger.Infof(ctx, "[folder-summary] skipped: wiki service not configured")
		return nil
	}
	kb, err := s.kbService.GetKnowledgeBaseByIDOnly(ctx, p.KnowledgeBaseID)
	if err != nil || kb == nil {
		return fmt.Errorf("folder summary: kb %s not found: %v", p.KnowledgeBaseID, err)
	}
	if !kb.IsWikiEnabled() {
		logger.Infof(ctx, "[folder-summary] skipped: wiki not enabled for kb %s", p.KnowledgeBaseID)
		return nil
	}
	if strings.TrimSpace(kb.SummaryModelID) == "" {
		return fmt.Errorf("folder summary: kb %s has no summary_model_id configured", p.KnowledgeBaseID)
	}

	// Tenant context for the model call. asynq worker contexts carry no
	// tenant, so everything the model/repository layers need must be seeded
	// from the payload: TenantID (MustTenantIDFromContext panics without it)
	// and TenantInfo.
	ctx = context.WithValue(ctx, types.TenantIDContextKey, p.TenantID)
	if tenant, err := s.tenantRepo.GetTenantByID(ctx, p.TenantID); err == nil && tenant != nil {
		ctx = context.WithValue(ctx, types.TenantInfoContextKey, tenant)
	}

	tree, err := s.buildFolderTree(ctx, p.TenantID, p.KnowledgeBaseID)
	if err != nil {
		return fmt.Errorf("folder summary: build tree: %w", err)
	}

	// Bottom-up: deepest folders first so parents aggregate finished children.
	scoped := scopeTree(tree, p.RootPath)
	sort.SliceStable(scoped, func(i, j int) bool {
		return scoped[i].Depth > scoped[j].Depth
	})

	model, err := s.modelSvc.GetChatModel(ctx, kb.SummaryModelID)
	if err != nil {
		return fmt.Errorf("folder summary: resolve summary model: %w", err)
	}

	var generated, skipped, failed int
	for _, node := range scoped {
		content, snapshot, err := s.generateFolderContent(ctx, p.TenantID, model, kb, node)
		if err != nil {
			logger.Warnf(ctx, "[folder-summary] generate %s failed: %v", node.Path, err)
			failed++
			continue
		}
		if content == "" && node.Parent != "" {
			// Empty leaf (no files, no child summaries yet): skip silently.
			continue
		}
		page, existed, err := s.findProjectionPage(ctx, kb.ID, node)
		if err != nil {
			logger.Warnf(ctx, "[folder-summary] lookup page %s failed: %v", node.Slug, err)
			failed++
			continue
		}
		if existed && types.IsFolderSummaryPageManualEdit(page) {
			logger.Infof(ctx, "[folder-summary] skip %s: manually edited", node.Slug)
			skipped++
			continue
		}
		if err := s.projectPage(ctx, p.TenantID, kb, node, content, snapshot, page, existed); err != nil {
			logger.Warnf(ctx, "[folder-summary] project %s failed: %v", node.Slug, err)
			failed++
			continue
		}
		generated++
	}

	// Root map page (only on a full pass or when refreshing the root).
	if p.RootPath == "" {
		if err := s.generateRootMap(ctx, model, kb, tree); err != nil {
			logger.Warnf(ctx, "[folder-summary] root map failed: %v", err)
			failed++
		} else {
			generated++
		}
	}
	logger.Infof(ctx, "[folder-summary] pass done: kb=%s folders=%d generated=%d skipped=%d failed=%d",
		p.KnowledgeBaseID, len(scoped), generated, skipped, failed)
	if failed > 0 && generated == 0 && skipped == 0 {
		return fmt.Errorf("folder summary: all %d folders failed", failed)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Folder tree assembly (folder_path aggregation — main's native model)
// ---------------------------------------------------------------------------

// folderNode is one layer of the aggregation tree.
type folderNode struct {
	Path   string // "" = KB root
	Slug   string // folder_summary/<slugified path>
	Depth  int
	Parent string
	Files  []*types.Knowledge
}

func (s *folderSummaryWikiService) buildFolderTree(ctx context.Context, tenantID uint64, kbID string) ([]*folderNode, error) {
	// One aggregate query for the folder list + counts...
	counts, err := s.repo.ListKnowledgeFolderCounts(ctx, tenantID, kbID)
	if err != nil {
		return nil, err
	}
	// ...one full listing for per-file metadata (title/description).
	files, err := s.repo.ListKnowledgeByKnowledgeBaseID(ctx, tenantID, kbID)
	if err != nil {
		return nil, err
	}

	byPath := map[string][]*types.Knowledge{}
	for _, k := range files {
		if k == nil {
			continue
		}
		byPath[k.FolderPath] = append(byPath[k.FolderPath], k)
	}

	// Materialize every distinct folder path (from counts AND from files —
	// counts already derive from the same table, so counts alone suffice, but
	// files keep us safe against empty-folder corner cases).
	pathSet := map[string]bool{}
	for _, c := range counts {
		if c != nil && c.FolderPath != "" {
			pathSet[c.FolderPath] = true
		}
	}
	for p := range byPath {
		if p != "" {
			pathSet[p] = true
		}
	}

	nodes := make([]*folderNode, 0, len(pathSet))
	for path := range pathSet {
		depth := strings.Count(path, "/") + 1
		parent := ""
		if idx := strings.LastIndex(path, "/"); idx >= 0 {
			parent = path[:idx]
		}
		nodes = append(nodes, &folderNode{
			Path:   path,
			Slug:   types.WikiFolderSummarySlug(folderSummaryPathSlug(path)),
			Depth:  depth,
			Parent: parent,
			Files:  byPath[path],
		})
	}
	// The KB root node aggregates top-level folders for the map page.
	rootFiles, hasRootFiles := byPath[""]
	nodes = append(nodes, &folderNode{
		Path:   "",
		Slug:   types.WikiFolderSummarySlug(""),
		Depth:  0,
		Parent: "",
		Files:  rootFiles,
	})
	_ = hasRootFiles
	return nodes, nil
}

// scopeTree keeps nodes under rootPath (plus the node for rootPath itself).
func scopeTree(nodes []*folderNode, rootPath string) []*folderNode {
	if rootPath == "" {
		return nodes
	}
	prefix := rootPath + "/"
	out := make([]*folderNode, 0, len(nodes))
	for _, n := range nodes {
		if n.Path == rootPath || strings.HasPrefix(n.Path, prefix) {
			out = append(out, n)
		}
	}
	return out
}

// folderSummaryPathSlug converts a folder path to a wiki slug segment using
// the same slugify rules as wiki ingest (CJK preserved).
func folderSummaryPathSlug(path string) string {
	s := slugify(path)
	s = strings.ReplaceAll(s, "/", "-")
	if s == "" {
		s = "root"
	}
	return s
}

// ---------------------------------------------------------------------------
// Generation
// ---------------------------------------------------------------------------

func (s *folderSummaryWikiService) generateFolderContent(
	ctx context.Context,
	tenantID uint64,
	model chat.Chat,
	kb *types.KnowledgeBase,
	node *folderNode,
) (string, *types.FolderSummarySnapshot, error) {
	childSummaries, err := s.gatherChildSummaries(ctx, tenantID, kb.ID, node)
	if err != nil {
		return "", nil, err
	}
	if len(node.Files) == 0 && len(childSummaries) == 0 {
		return "", &types.FolderSummarySnapshot{FileCount: 0, ChildCount: len(childSummaries)}, nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "知识库「%s」中的文件夹「%s」", kb.Name, displayPath(node.Path))
	if len(childSummaries) > 0 {
		b.WriteString("（含子文件夹）")
	}
	b.WriteString("。\n\n## 文件清单\n")
	for i, f := range node.Files {
		if i >= 50 {
			fmt.Fprintf(&b, "- ……另有 %d 份文档未列出\n", len(node.Files)-i)
			break
		}
		fmt.Fprintf(&b, "- %s", displayTitle(f))
		if d := strings.TrimSpace(f.Description); d != "" {
			trunc := []rune(d)
			if len(trunc) > 80 {
				trunc = trunc[:80]
			}
			fmt.Fprintf(&b, " —— %s……", string(trunc))
		}
		b.WriteString("\n")
	}
	for _, cs := range childSummaries {
		fmt.Fprintf(&b, "\n## 子文件夹：%s\n%s\n", displayPath(cs.Path), strings.TrimSpace(cs.Content))
	}

	prompt := folderSummaryLeafPrompt(len(node.Files), len(childSummaries) > 0)
	modelCtx := types.WithLLMCallMetadata(ctx, "folder_summary", "")
	resp, err := model.Chat(modelCtx, []chat.Message{
		{Role: "system", Content: prompt},
		{Role: "user", Content: b.String()},
	}, &chat.ChatOptions{Temperature: 0})
	if err != nil {
		return "", nil, fmt.Errorf("llm call: %w", err)
	}
	content := strings.TrimSpace(resp.Content)
	if content == "" {
		return "", nil, fmt.Errorf("llm returned empty content")
	}
	return content, &types.FolderSummarySnapshot{
		FileCount:  len(node.Files),
		ChildCount: len(childSummaries),
	}, nil
}

type childSummary struct {
	Path    string
	Slug    string
	Content string
}

// gatherChildSummaries loads the (already generated) projection pages of the
// node's direct child folders so a parent layer can aggregate them.
func (s *folderSummaryWikiService) gatherChildSummaries(ctx context.Context, tenantID uint64, kbID string, node *folderNode) ([]*childSummary, error) {
	children, err := s.listChildFolders(ctx, tenantID, kbID, node.Path)
	if err != nil {
		return nil, err
	}
	out := make([]*childSummary, 0, len(children))
	for _, child := range children {
		page, err := s.wikiService.GetPageBySlug(ctx, kbID, child.Slug)
		if err != nil || page == nil {
			continue
		}
		out = append(out, &childSummary{Path: child.Path, Slug: child.Slug, Content: page.Content})
	}
	return out, nil
}

// listChildFolders enumerates direct child folder paths of parentPath.
func (s *folderSummaryWikiService) listChildFolders(ctx context.Context, tenantID uint64, kbID string, parentPath string) ([]*folderNode, error) {
	counts, err := s.repo.ListKnowledgeFolderCounts(ctx, tenantID, kbID)
	if err != nil {
		return nil, err
	}
	prefix := parentPath
	if prefix != "" {
		prefix += "/"
	}
	out := []*folderNode{}
	for _, c := range counts {
		if c == nil || c.FolderPath == "" {
			continue
		}
		// Skip the folder itself. TrimPrefix only strips an exact
		// "<parent>/" prefix, so the parent's own row ("测试" under
		// parentPath "测试") survives with rest == the full path, which
		// previously made the folder list ITSELF as its own child — feeding
		// the page's previous content back into its own regeneration prompt
		// (self-aggregation) and emitting a "[[folder_summary/...|self]]"
		// link back to itself.
		if c.FolderPath == parentPath {
			continue
		}
		// Only paths actually under "<parent>/" are children. TrimPrefix is
		// a no-op for paths that merely share a leading substring ("测" under
		// "测试/") or live at a different level ("同级" next to "测试"), and
		// the bare rest != "" check used to admit those as phantom children
		// too — pulling unrelated folders' summaries into this folder's
		// generation prompt and link listing.
		if prefix != "" && !strings.HasPrefix(c.FolderPath, prefix) {
			continue
		}
		rest := strings.TrimPrefix(c.FolderPath, prefix)
		if prefix == "" {
			rest = c.FolderPath
		}
		if rest != "" && !strings.Contains(rest, "/") {
			out = append(out, &folderNode{Path: c.FolderPath, Slug: types.WikiFolderSummarySlug(folderSummaryPathSlug(c.FolderPath))})
		}
	}
	return out, nil
}

// generateRootMap writes the KB-wide map page (folder_summary/index).
func (s *folderSummaryWikiService) generateRootMap(ctx context.Context, model chat.Chat, kb *types.KnowledgeBase, tree []*folderNode) error {
	var tops []*folderNode
	for _, n := range tree {
		if n.Path != "" && n.Parent == "" {
			tops = append(tops, n)
		}
	}
	if len(tops) == 0 {
		return nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "知识库「%s」共有 %d 个顶层文件夹。\n\n## 顶层文件夹摘要\n", kb.Name, len(tops))
	for _, t := range tops {
		page, err := s.wikiService.GetPageBySlug(ctx, kb.ID, t.Slug)
		if err != nil || page == nil {
			fmt.Fprintf(&b, "\n## %s（尚未生成摘要）\n", displayPath(t.Path))
			continue
		}
		fmt.Fprintf(&b, "\n## %s\n%s\n", displayPath(t.Path), strings.TrimSpace(page.Content))
	}
	resp, err := model.Chat(ctx, []chat.Message{
		{Role: "system", Content: rootMapPrompt(len(tops))},
		{Role: "user", Content: b.String()},
	}, &chat.ChatOptions{Temperature: 0})
	if err != nil {
		return err
	}
	content := strings.TrimSpace(resp.Content)
	slug := types.WikiFolderSummarySlug("")
	page, err := s.wikiService.GetPageBySlug(ctx, kb.ID, slug)
	if err == nil && page != nil {
		if types.IsFolderSummaryPageManualEdit(page) {
			return nil
		}
		if page.Content == content {
			return nil
		}
		page.Content = content
		page.Summary = fmt.Sprintf("知识库 %s 的全库导览地图", kb.Name)
		page.LastEditSource = types.WikiEditSourcePipeline
		_, err = s.wikiService.UpdatePage(ctx, page)
		return err
	}
	_, err = s.wikiService.CreatePage(ctx, &types.WikiPage{
		KnowledgeBaseID: kb.ID,
		Slug:            slug,
		Title:           fmt.Sprintf("🗺️ %s · 全库导览", kb.Name),
		PageType:        types.WikiPageTypeFolderSummary,
		Status:          types.WikiPageStatusPublished,
		Content:         content,
		Summary:         fmt.Sprintf("知识库 %s 的全库导览地图", kb.Name),
		LastEditSource:  types.WikiEditSourcePipeline,
	})
	return err
}

// ---------------------------------------------------------------------------
// Projection
// ---------------------------------------------------------------------------

func (s *folderSummaryWikiService) findProjectionPage(ctx context.Context, kbID string, node *folderNode) (*types.WikiPage, bool, error) {
	page, err := s.wikiService.GetPageBySlug(ctx, kbID, node.Slug)
	if err != nil {
		return nil, false, nil // treat as not-found; projectPage creates
	}
	return page, page != nil, nil
}

func (s *folderSummaryWikiService) projectPage(
	ctx context.Context,
	tenantID uint64,
	kb *types.KnowledgeBase,
	node *folderNode,
	content string,
	snapshot *types.FolderSummarySnapshot,
	page *types.WikiPage,
	existed bool,
) error {
	// Template-built link listing (never LLM-generated: listings must be exact).
	docLinks := s.buildDocLinkListing(ctx, kb.ID, node)
	childLinks := s.buildChildLinkListing(ctx, tenantID, kb.ID, node)
	full := renderFolderSummaryPage(node, content, docLinks, childLinks, snapshot)

	if existed && page != nil {
		if page.Content == full {
			return nil // no-op: avoid spurious version bumps
		}
		page.Title = fmt.Sprintf("📁 %s · 文件夹摘要", displayPath(node.Path))
		page.Content = full
		page.Summary = fmt.Sprintf("文件夹 %s 的内容概述", displayPath(node.Path))
		page.LastEditSource = types.WikiEditSourcePipeline
		_, err := s.wikiService.UpdatePage(ctx, page)
		return err
	}
	_, err := s.wikiService.CreatePage(ctx, &types.WikiPage{
		KnowledgeBaseID: kb.ID,
		Slug:            node.Slug,
		Title:           fmt.Sprintf("📁 %s · 文件夹摘要", displayPath(node.Path)),
		PageType:        types.WikiPageTypeFolderSummary,
		Status:          types.WikiPageStatusPublished,
		Content:         full,
		Summary:         fmt.Sprintf("文件夹 %s 的内容概述", displayPath(node.Path)),
		LastEditSource:  types.WikiEditSourcePipeline,
	})
	return err
}

// buildDocLinkListing returns the template section linking each file in the
// folder to its wiki summary page (only for files whose page exists).
func (s *folderSummaryWikiService) buildDocLinkListing(ctx context.Context, kbID string, node *folderNode) []string {
	out := []string{}
	for _, f := range node.Files {
		if f == nil {
			continue
		}
		slug := "summary/" + slugify(f.ID)
		if _, err := s.wikiService.GetPageBySlug(ctx, kbID, slug); err != nil {
			continue
		}
		out = append(out, fmt.Sprintf("- [[%s|%s]]", slug, displayTitle(f)))
	}
	return out
}

// buildChildLinkListing returns links to the direct child folders' projection
// pages (only existing ones).
func (s *folderSummaryWikiService) buildChildLinkListing(ctx context.Context, tenantID uint64, kbID string, node *folderNode) []string {
	children, err := s.listChildFolders(ctx, tenantID, kbID, node.Path)
	if err != nil {
		return nil
	}
	out := []string{}
	for _, c := range children {
		if _, err := s.wikiService.GetPageBySlug(ctx, kbID, c.Slug); err != nil {
			continue
		}
		out = append(out, fmt.Sprintf("- [[%s|%s]]", c.Slug, displayPath(c.Path)))
	}
	return out
}

func renderFolderSummaryPage(node *folderNode, content string, docLinks, childLinks []string, snapshot *types.FolderSummarySnapshot) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# 📁 %s · 文件夹摘要\n\n", displayPath(node.Path))
	b.WriteString(strings.TrimSpace(content))
	b.WriteString("\n")
	if len(docLinks) > 0 {
		b.WriteString("\n## 文档清单\n")
		for _, l := range docLinks {
			b.WriteString(l)
			b.WriteString("\n")
		}
	}
	if len(childLinks) > 0 {
		b.WriteString("\n## 子文件夹\n")
		for _, l := range childLinks {
			b.WriteString(l)
			b.WriteString("\n")
		}
	}
	if snapshot != nil {
		fmt.Fprintf(&b, "\n<!-- folder_summary_snapshot files=%d children=%d -->\n", snapshot.FileCount, snapshot.ChildCount)
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Stale detection + tree listing for the UI panel
// ---------------------------------------------------------------------------

// ListTree returns folder list with per-folder projection state.
func (s *folderSummaryWikiService) ListTree(ctx context.Context, kbID string) (*types.FolderSummaryTree, error) {
	tenantID, _ := ctx.Value(types.TenantIDContextKey).(uint64)
	nodes, err := s.buildFolderTree(ctx, tenantID, kbID)
	if err != nil {
		return nil, err
	}
	out := &types.FolderSummaryTree{Items: []*types.FolderSummaryItem{}}
	for _, n := range nodes {
		item := &types.FolderSummaryItem{
			Path:  n.Path,
			Depth: n.Depth,
			Files: len(n.Files),
			Slug:  n.Slug,
			State: types.FolderSummaryStateMissing,
		}
		page, err := s.wikiService.GetPageBySlug(ctx, kbID, n.Slug)
		if err == nil && page != nil {
			item.State = types.FolderSummaryStateFresh
			item.UpdatedAt = page.UpdatedAt
			if types.IsFolderSummaryPageManualEdit(page) {
				item.State = types.FolderSummaryStateManual
			} else if s.isStale(n, page) {
				item.State = types.FolderSummaryStateStale
			}
		}
		out.Items = append(out.Items, item)
	}
	sort.SliceStable(out.Items, func(i, j int) bool {
		return out.Items[i].Path < out.Items[j].Path
	})
	return out, nil
}

// isStale compares the folder's current file set against the projection
// snapshot embedded in the page (count + max updated_at).
func (s *folderSummaryWikiService) isStale(node *folderNode, page *types.WikiPage) bool {
	snapshot := parseSnapshotComment(page.Content)
	if snapshot == nil {
		return true // unknown snapshot: assume stale
	}
	if snapshot.FileCount != len(node.Files) {
		return true
	}
	for _, f := range node.Files {
		if f != nil && f.UpdatedAt.After(page.UpdatedAt) {
			return true
		}
	}
	return false
}

func parseSnapshotComment(content string) *types.FolderSummarySnapshot {
	idx := strings.LastIndex(content, "<!-- folder_summary_snapshot")
	if idx < 0 {
		return nil
	}
	rest := content[idx:]
	end := strings.Index(rest, "-->")
	if end < 0 {
		return nil
	}
	// Include the full "-->" terminator in the segment: `end` is the offset of
	// the FIRST '-' of "-->", so rest[:end] would cut it off and leave a
	// dangling "<!-- ... files=N children=M " string that never matches the
	// Sscanf format (unexpected EOF), making every page parse as an unknown
	// snapshot and thus permanently stale.
	seg := rest[:end+len("-->")]
	var files, children int
	if _, err := fmt.Sscanf(seg, "<!-- folder_summary_snapshot files=%d children=%d -->", &files, &children); err != nil {
		return nil
	}
	return &types.FolderSummarySnapshot{FileCount: files, ChildCount: children}
}

// ---------------------------------------------------------------------------
// Prompts (temperature etc. follow the kb summary model's own defaults)
// ---------------------------------------------------------------------------

func folderSummaryLeafPrompt(fileCount int, hasChildren bool) string {
	role := "文件夹综述"
	if hasChildren {
		role = "导览式综述（覆盖各子文件夹主题的分工与关系）"
	}
	return fmt.Sprintf(`请用中文为上述材料写一段 150-300 字的%s。要求：
1. 概括这个文件夹（%d 份文档%s）的内容主题、覆盖的业务或技术范围。
2. 不要逐一列举文件名（清单会单独呈现），只做主题级概括。
3. 严禁编造清单之外的任何文档或事实。直接输出正文，不要标题。`,
		role, fileCount, ternaryStr(hasChildren, "，含子文件夹", ""))
}

func rootMapPrompt(topCount int) string {
	return fmt.Sprintf(`请为整个知识库写一段 300 字以内的中文导览：一句话定位这个知识库的用途，
各顶层主题（共 %d 个）的分工，以及推荐的检索入口。直接输出正文。`, topCount)
}

func ternaryStr(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

// displayPath renders "" as the KB root.
func displayPath(path string) string {
	if path == "" {
		return "（根目录）"
	}
	return path
}

func displayTitle(k *types.Knowledge) string {
	if k.Title != "" {
		return k.Title
	}
	if k.FileName != "" {
		return k.FileName
	}
	return k.ID
}
