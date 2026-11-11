-- Migration: 000108_knowledge_push_allowed
-- Description: 为 knowledges 表添加 push_allowed 列，控制单个文档是否允许被推送/生成下载链接。
-- 默认 true 保持向后兼容（现有文档维持原有"可推送"行为）。
-- Renumbered from dev2's 000084, then from a private 01000 range back into the
-- mainline sequence: golang-migrate parses versions as plain integers, so the
-- former 01000 numbering made the DB jump past upstream's 000096..000107
-- migrations, silently skipping them. 000108/000109 now sit after upstream
-- (000107 as of this writing) and stay in the tracked monotonic order.

DO $$ BEGIN RAISE NOTICE '[Migration 000108] Adding knowledges.push_allowed...'; END $$;

ALTER TABLE knowledges
    ADD COLUMN IF NOT EXISTS push_allowed BOOLEAN NOT NULL DEFAULT TRUE;

DO $$ BEGIN RAISE NOTICE '[Migration 000108] knowledges.push_allowed ready'; END $$;
