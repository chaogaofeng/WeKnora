-- PostgreSQL one-off repair, not an application-startup migration.
-- Back up the affected wiki_pages and wiki_page_revisions rows first.
-- Repairs only zero-owner folder summaries in active, nonzero-owner KBs.
-- Nonzero mismatches, other page types and mismatched revision KBs are untouched.
-- This single statement atomically updates ownership without changing content,
-- versions, timestamps, source links or historical text. Safe to run again.
WITH owners AS (
    SELECT p.id, p.knowledge_base_id, kb.tenant_id
    FROM wiki_pages p
    JOIN knowledge_bases kb ON kb.id = p.knowledge_base_id
    WHERE p.page_type = 'folder_summary'
      AND p.tenant_id = 0
      AND p.deleted_at IS NULL
      AND kb.deleted_at IS NULL
      AND kb.tenant_id > 0
), repaired_pages AS (
    UPDATE wiki_pages p
    SET tenant_id = owners.tenant_id
    FROM owners
    WHERE p.id = owners.id AND p.tenant_id = 0
    RETURNING p.id, p.knowledge_base_id, p.tenant_id
), repaired_revisions AS (
    UPDATE wiki_page_revisions r
    SET tenant_id = p.tenant_id
    FROM repaired_pages p
    WHERE r.page_id = p.id
      AND r.knowledge_base_id = p.knowledge_base_id
      AND r.tenant_id = 0
    RETURNING r.id
)
SELECT (SELECT count(*) FROM repaired_pages) AS repaired_pages,
       (SELECT count(*) FROM repaired_revisions) AS repaired_revisions;
