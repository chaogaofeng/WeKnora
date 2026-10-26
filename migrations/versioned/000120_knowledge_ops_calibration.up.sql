ALTER TABLE knowledge_ops_settings ADD COLUMN calibration_binding TEXT NOT NULL DEFAULT '';
ALTER TABLE knowledge_ops_settings ADD COLUMN calibration_policy_version TEXT NOT NULL DEFAULT '';
ALTER TABLE knowledge_ops_settings ADD COLUMN calibration_model_id TEXT NOT NULL DEFAULT '';
ALTER TABLE knowledge_ops_settings ADD COLUMN calibration_confirmed_at TIMESTAMP NULL;
