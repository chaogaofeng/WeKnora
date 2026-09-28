package knowledgeops

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

// RecoverInterrupted touches only runs created by this module after opt-in.
// A generous idle limit avoids racing live requests on another application node.
func (r *Repository) RecoverInterrupted(ctx context.Context, now time.Time) error {
	var rows []Run
	cutoff := now.Add(-time.Hour)
	if err := r.db.WithContext(ctx).Where("status = ? AND updated_at < ? AND tenant_id IN (SELECT tenant_id FROM knowledge_ops_settings WHERE enabled = ?)", "running", cutoff, true).Limit(50).Find(&rows).Error; err != nil {
		return err
	}
	for _, run := range rows {
		if err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			gaps := append(run.Gaps, "capture_interrupted")
			body, _ := json.Marshal(gaps)
			q := tx.Model(&Run{}).Where("id = ? AND status = ? AND updated_at < ?", run.ID, "running", cutoff).Updates(map[string]any{"status": "failed", "capture_status": "partial", "gaps": string(body), "finished_at": now, "updated_at": now})
			if q.Error != nil {
				return q.Error
			}
			if q.RowsAffected == 0 {
				return nil
			}
			return NewRepository(tx).Enqueue(ctx, run, 1)
		}); err != nil {
			return err
		}
	}
	return nil
}
