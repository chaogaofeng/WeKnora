-- Migration: 01000_knowledge_push_allowed (down)
-- Renumbered from dev2's 000084 to the 01000+ private range.

DO $$ BEGIN RAISE NOTICE '[Migration 01000] Dropping knowledges.push_allowed...'; END $$;

ALTER TABLE knowledges
    DROP COLUMN IF EXISTS push_allowed;

DO $$ BEGIN RAISE NOTICE '[Migration 01000] knowledges.push_allowed dropped'; END $$;
