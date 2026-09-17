-- Migration: 000017_message_pushed_knowledge_ids (sqlite)
-- [push-files] See versioned/01001 for the postgres variant. Renumbered from
-- dev2's sqlite 000003 to follow the main sqlite sequence.

ALTER TABLE messages ADD COLUMN pushed_knowledge_ids TEXT NOT NULL DEFAULT '[]';
