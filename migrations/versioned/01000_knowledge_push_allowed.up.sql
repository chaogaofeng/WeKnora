-- Migration: 01000_knowledge_push_allowed
-- Description: 为 knowledges 表添加 push_allowed 列，控制单个文档是否允许被推送/生成下载链接。
-- 默认 true 保持向后兼容（现有文档维持原有"可推送"行为）。
-- Renumbered from dev2's 000084 to the 01000+ private range to avoid
-- colliding with upstream WeKnora migration numbers (upstream is at 000095).
-- 000095 as of this writing).

DO $$ BEGIN RAISE NOTICE '[Migration 01000] Adding knowledges.push_allowed...'; END $$;

ALTER TABLE knowledges
    ADD COLUMN IF NOT EXISTS push_allowed BOOLEAN NOT NULL DEFAULT TRUE;

DO $$ BEGIN RAISE NOTICE '[Migration 01000] knowledges.push_allowed ready'; END $$;
