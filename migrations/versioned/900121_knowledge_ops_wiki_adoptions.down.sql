-- Refuse to discard Wiki publication provenance during rollback.
CREATE TEMP TABLE knowledge_ops_wiki_rollback_guard (value INTEGER CHECK(value = 0));
INSERT INTO knowledge_ops_wiki_rollback_guard SELECT COUNT(*) FROM knowledge_ops_adoptions WHERE target_type = 'wiki';
DROP TABLE knowledge_ops_wiki_rollback_guard;
ALTER TABLE knowledge_ops_adoptions DROP CONSTRAINT knowledge_ops_adoptions_target_unique;
ALTER TABLE knowledge_ops_adoptions DROP COLUMN wiki_page_id;
ALTER TABLE knowledge_ops_adoptions DROP COLUMN wiki_slug;
ALTER TABLE knowledge_ops_adoptions DROP COLUMN wiki_version;
ALTER TABLE knowledge_ops_adoptions DROP COLUMN target_type;
ALTER TABLE knowledge_ops_adoptions ADD CONSTRAINT knowledge_ops_adoptions_tenant_id_run_id_analysis_id_key UNIQUE(tenant_id,run_id,analysis_id);
