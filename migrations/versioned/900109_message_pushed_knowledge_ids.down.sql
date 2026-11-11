-- Migration: 01001_message_pushed_knowledge_ids (down)
-- Renumbered from dev2's 000080.

ALTER TABLE messages DROP COLUMN IF EXISTS pushed_knowledge_ids;
