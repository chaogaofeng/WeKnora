CREATE TABLE knowledge_ops_followups (
    id TEXT PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    run_id TEXT NOT NULL REFERENCES knowledge_ops_runs(id) ON DELETE CASCADE,
    analysis_id TEXT NOT NULL REFERENCES knowledge_ops_analyses(id) ON DELETE CASCADE,
    route TEXT NOT NULL CHECK(route IN ('source_verification','technical_followup')),
    action TEXT NOT NULL CHECK(action IN ('resolved','accepted_limitation','not_required','reopened')),
    reason TEXT NOT NULL,
    actor_id TEXT NOT NULL,
    previous_id TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL,
    UNIQUE(tenant_id,run_id,analysis_id,route,previous_id)
);
CREATE INDEX idx_knowledge_ops_followups_run ON knowledge_ops_followups(tenant_id,run_id,created_at);
