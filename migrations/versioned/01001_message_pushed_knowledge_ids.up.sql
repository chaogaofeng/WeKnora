-- Migration: 01001_message_pushed_knowledge_ids
-- [push-files] Track which knowledge files were pushed by the push_files
-- tool in each assistant message. Column is present but not yet populated in
-- the minimal port (the dev2 writer lived in the full reference-tracking
-- chain); IM link generation and future writers read it.
-- Renumbered from dev2's 000080 to the 01000+ private range to avoid
-- colliding with upstream WeKnora migration numbers.

DO $$ BEGIN RAISE NOTICE '[Migration 01001] Adding pushed_knowledge_ids to messages'; END $$;

ALTER TABLE messages
    ADD COLUMN IF NOT EXISTS pushed_knowledge_ids JSONB NOT NULL DEFAULT '[]'::jsonb;

COMMENT ON COLUMN messages.pushed_knowledge_ids IS
    'Knowledge IDs pushed by push_files tool in this message';

DO $$ BEGIN RAISE NOTICE '[Migration 01001] done'; END $$;
