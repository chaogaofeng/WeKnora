-- Migration: 000017_message_pushed_knowledge_ids (sqlite, down)

ALTER TABLE messages DROP COLUMN pushed_knowledge_ids;
