package types

import "time"

// [folder-summary] Folder-summary projection state for the UI panel.

const (
	// FolderSummaryStateMissing means no projection page exists yet.
	FolderSummaryStateMissing = "missing"
	// FolderSummaryStateFresh means the page exists and matches the folder snapshot.
	FolderSummaryStateFresh = "fresh"
	// FolderSummaryStateStale means files changed since the page was generated.
	FolderSummaryStateStale = "stale"
	// FolderSummaryStateManual means a user hand-edited the page; machine
	// sync is skipped until an explicit forced refresh.
	FolderSummaryStateManual = "manual_edit"
)

// FolderSummarySnapshot is the input fingerprint embedded in each projection
// page (as an HTML comment) for cheap stale detection.
type FolderSummarySnapshot struct {
	FileCount  int `json:"file_count"`
	ChildCount int `json:"child_count"`
}

// FolderSummaryItem is one folder row in the panel's tree listing.
type FolderSummaryItem struct {
	Path      string    `json:"path"` // "" = KB root map page
	Slug      string    `json:"slug"` // folder_summary/<slug>
	Depth     int       `json:"depth"`
	Files     int       `json:"files"`
	State     string    `json:"state"` // missing / fresh / stale / manual_edit
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

// FolderSummaryTree is the panel's full listing for one KB.
type FolderSummaryTree struct {
	Items []*FolderSummaryItem `json:"items"`
}
