package service

import (
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
)

// Keep ownership checks at the folder-projection boundary. Shared Wiki CRUD
// does not infer ownership, and a task's tenant cannot override the KB owner.
func validateFolderSummaryTenant(kb *types.KnowledgeBase, tenantID uint64) error {
	if kb == nil || kb.ID == "" || kb.TenantID == 0 || tenantID != kb.TenantID {
		return fmt.Errorf("folder summary: knowledge base tenant mismatch")
	}
	return nil
}

// The root overview's lineage must not depend on model-generated links.
func rootMapWithSourceLinks(content string, sources []*folderNode) string {
	var out strings.Builder
	out.WriteString(strings.TrimSpace(content))
	out.WriteString("\n\n## 顶层文件夹\n")
	for _, node := range sources {
		fmt.Fprintf(&out, "- [[%s|%s]]\n", node.Slug, displayPath(node.Path))
	}
	return out.String()
}
