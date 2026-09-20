package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// renderFolderSummaryPage embeds the snapshot as a trailing HTML comment with
// a newline after "-->" (see renderFolderSummaryPage); parseSnapshotComment
// must round-trip it. This guards the off-by-end slicing bug where
// rest[:end] dropped the "-->" terminator and Sscanf failed with
// "unexpected EOF" for EVERY page — making isStale return true unconditionally
// and the panel show stale forever even after a successful refresh.
func TestParseSnapshotCommentRoundTripsRenderedPage(t *testing.T) {
	node := &folderNode{Path: "测试", Slug: "folder_summary/ce-shi"}
	full := renderFolderSummaryPage(node, "综述内容", []string{"- [[summary/x|doc]]"}, nil,
		&types.FolderSummarySnapshot{FileCount: 2, ChildCount: 1})

	snapshot := parseSnapshotComment(full)
	if snapshot == nil {
		t.Fatalf("parseSnapshotComment() = nil for rendered page; want snapshot. content tail: %q", tail(full, 120))
	}
	if snapshot.FileCount != 2 || snapshot.ChildCount != 1 {
		t.Fatalf("parseSnapshotComment() = files=%d children=%d; want 2/1", snapshot.FileCount, snapshot.ChildCount)
	}
}

func TestParseSnapshotCommentWithoutSnapshot(t *testing.T) {
	if got := parseSnapshotComment("# 标题\n\n没有快照注释"); got != nil {
		t.Fatalf("parseSnapshotComment() = %+v; want nil when comment absent", got)
	}
	if got := parseSnapshotComment("<!-- folder_summary_snapshot files=2"); got != nil {
		t.Fatalf("parseSnapshotComment() = %+v; want nil when terminator missing", got)
	}
}

// isStale must report FRESH for a page whose snapshot matches the node's
// current file set and whose files were not updated after the page. With the
// slicing bug every page parsed as unknown-snapshot and was permanently stale.
func TestIsStaleFreshWhenSnapshotMatches(t *testing.T) {
	svc := &folderSummaryWikiService{}
	node := &folderNode{Path: "测试"}
	page := &types.WikiPage{
		Content:   renderFolderSummaryPage(node, "综述", nil, nil, &types.FolderSummarySnapshot{FileCount: 2, ChildCount: 0}),
		UpdatedAt: nowPlus(0),
	}
	node.Files = []*types.Knowledge{
		{ID: "f1", UpdatedAt: nowPlus(-10)},
		{ID: "f2", UpdatedAt: nowPlus(-5)},
	}
	if svc.isStale(node, page) {
		t.Fatal("isStale() = true for matching snapshot; want false (fresh)")
	}

	// A newer file flips it back to stale.
	node.Files[1].UpdatedAt = nowPlus(10)
	if !svc.isStale(node, page) {
		t.Fatal("isStale() = false with a file updated after the page; want true")
	}

	// A changed file count flips it back to stale.
	node.Files = node.Files[:1]
	if !svc.isStale(node, page) {
		t.Fatal("isStale() = false with a differing file count; want true")
	}
}

// listChildFolders must never return the folder itself as its own child.
// The original bug: TrimPrefix("测试", "测试/") = "测试" (non-empty, no "/"),
// so the parent's own counts row passed the child filter and the folder
// aggregated its own previous page into its regeneration prompt
// (self-reference, content compounding on every refresh).
func TestListChildFoldersExcludesSelfAndNonChildren(t *testing.T) {
	svc := &folderSummaryWikiService{}
	repo := &fakeFolderCountsRepo{counts: []*types.KnowledgeFolderCount{
		{FolderPath: "测试", Count: 2},           // the folder itself
		{FolderPath: "测试/子文件夹", Count: 3},      // direct child
		{FolderPath: "测试/子文件夹/孙文件夹", Count: 1}, // grandchild (nested — not direct)
		{FolderPath: "同级", Count: 5},           // sibling — unrelated
		{FolderPath: "", Count: 7},             // root-level rows (excluded)
		{FolderPath: "测", Count: 1},            // prefix-share, not a child ("测" ⊄ "测试/")
		{FolderPath: "测试A/子文件夹", Count: 4},     // shares a prefix substring, not under "测试/"
	}}
	svc.repo = repo

	children, err := svc.listChildFolders(nil, 1, "kb", "测试")
	if err != nil {
		t.Fatalf("listChildFolders() err = %v", err)
	}
	paths := make([]string, 0, len(children))
	for _, c := range children {
		paths = append(paths, c.Path)
	}
	want := []string{"测试/子文件夹"}
	if strings.Join(paths, ",") != strings.Join(want, ",") {
		t.Fatalf("listChildFolders(测试) = %v; want %v", paths, want)
	}
}

// Root listing (parentPath "") must enumerate every top-level folder — the
// root map page aggregates these — while still excluding the root row itself.
func TestListChildFoldersRootLevel(t *testing.T) {
	svc := &folderSummaryWikiService{}
	svc.repo = &fakeFolderCountsRepo{counts: []*types.KnowledgeFolderCount{
		{FolderPath: "测试", Count: 2},
		{FolderPath: "同级", Count: 5},
		{FolderPath: "测试/子文件夹", Count: 3},
	}}

	children, err := svc.listChildFolders(nil, 1, "kb", "")
	if err != nil {
		t.Fatalf("listChildFolders() err = %v", err)
	}
	paths := make([]string, 0, len(children))
	for _, c := range children {
		paths = append(paths, c.Path)
	}
	if strings.Join(paths, ",") != "测试,同级" {
		t.Fatalf("listChildFolders(\"\") = %v; want [测试 同级]", paths)
	}
}

// --- helpers ---

func tail(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}

// nowPlus returns a time offset (seconds) from a fixed base so UpdatedAt
// comparisons in tests are deterministic.
func nowPlus(seconds int) time.Time {
	base := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	return base.Add(time.Duration(seconds) * time.Second)
}

// fakeFolderCountsRepo stubs only ListKnowledgeFolderCounts — enough to drive
// listChildFolders without a database.
type fakeFolderCountsRepo struct {
	interfaces.KnowledgeRepository
	counts []*types.KnowledgeFolderCount
}

func (r *fakeFolderCountsRepo) ListKnowledgeFolderCounts(
	_ context.Context, _ uint64, _ string,
) ([]*types.KnowledgeFolderCount, error) {
	return r.counts, nil
}
