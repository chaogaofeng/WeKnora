-- Migration: 000108_knowledge_push_allowed (down)
-- Renumbered from dev2's 000084; see the up migration for the numbering history.

DO $$ BEGIN RAISE NOTICE '[Migration 000108] Dropping knowledges.push_allowed...'; END $$;

ALTER TABLE knowledges
    DROP COLUMN IF EXISTS push_allowed;

DO $$ BEGIN RAISE NOTICE '[Migration 000108] knowledges.push_allowed dropped'; END $$;
