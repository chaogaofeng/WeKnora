-- Refuse to discard Wiki publication provenance during rollback.
CREATE TEMP TABLE knowledge_ops_wiki_rollback_guard (value INTEGER CHECK(value = 0));
INSERT INTO knowledge_ops_wiki_rollback_guard SELECT COUNT(*) FROM knowledge_ops_adoptions WHERE target_type = 'wiki';
DROP TABLE knowledge_ops_wiki_rollback_guard;
CREATE TABLE knowledge_ops_adoptions_previous (
    id TEXT PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    run_id TEXT NOT NULL REFERENCES knowledge_ops_runs(id) ON DELETE CASCADE,
    analysis_id TEXT NOT NULL REFERENCES knowledge_ops_analyses(id) ON DELETE CASCADE,
    review_id TEXT NOT NULL REFERENCES knowledge_ops_reviews(id) ON DELETE CASCADE,
    session_id TEXT NOT NULL,
    message_id TEXT NOT NULL,
    source_actor_id TEXT NOT NULL DEFAULT '',
    actor_id TEXT NOT NULL,
    kb_id TEXT NOT NULL,
    target_tenant_id BIGINT NOT NULL,
    question TEXT NOT NULL,
    answer TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('publishing','succeeded','failed')),
    entry_id BIGINT NOT NULL DEFAULT 0,
    knowledge_id TEXT NOT NULL DEFAULT '',
    reused BOOLEAN NOT NULL DEFAULT FALSE,
    error TEXT NOT NULL DEFAULT '',
    lease_token TEXT NOT NULL DEFAULT '',
    lease_until TIMESTAMP NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    UNIQUE(tenant_id,run_id,analysis_id)
);
INSERT INTO knowledge_ops_adoptions_previous (id,tenant_id,run_id,analysis_id,review_id,session_id,message_id,source_actor_id,actor_id,kb_id,target_tenant_id,question,answer,status,entry_id,knowledge_id,reused,error,lease_token,lease_until,created_at,updated_at) SELECT id,tenant_id,run_id,analysis_id,review_id,session_id,message_id,source_actor_id,actor_id,kb_id,target_tenant_id,question,answer,status,entry_id,knowledge_id,reused,error,lease_token,lease_until,created_at,updated_at FROM knowledge_ops_adoptions;
DROP TABLE knowledge_ops_adoptions;
ALTER TABLE knowledge_ops_adoptions_previous RENAME TO knowledge_ops_adoptions;
CREATE INDEX idx_knowledge_ops_adoptions_run ON knowledge_ops_adoptions(tenant_id,run_id,created_at);
