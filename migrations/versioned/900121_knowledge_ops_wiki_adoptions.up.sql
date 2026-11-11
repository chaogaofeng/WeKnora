ALTER TABLE knowledge_ops_adoptions ADD COLUMN target_type TEXT NOT NULL DEFAULT 'faq' CHECK(target_type IN ('faq','wiki'));
ALTER TABLE knowledge_ops_adoptions ADD COLUMN wiki_page_id TEXT NOT NULL DEFAULT '';
ALTER TABLE knowledge_ops_adoptions ADD COLUMN wiki_slug TEXT NOT NULL DEFAULT '';
ALTER TABLE knowledge_ops_adoptions ADD COLUMN wiki_version INTEGER NOT NULL DEFAULT 0;
ALTER TABLE knowledge_ops_adoptions DROP CONSTRAINT knowledge_ops_adoptions_tenant_id_run_id_analysis_id_key;
ALTER TABLE knowledge_ops_adoptions ADD CONSTRAINT knowledge_ops_adoptions_target_unique UNIQUE(tenant_id,run_id,analysis_id,target_type);
