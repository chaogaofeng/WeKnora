-- Per-message feedback (user ratings + admin annotations) for the ops log.
CREATE TABLE IF NOT EXISTS message_feedbacks (
    id          VARCHAR(36)  PRIMARY KEY,
    tenant_id   BIGINT       NOT NULL,
    session_id  VARCHAR(36)  NOT NULL,
    message_id  VARCHAR(36)  NOT NULL,
    from_source VARCHAR(16)   NOT NULL,
    rating      SMALLINT      NOT NULL DEFAULT 0,
    content     TEXT,
    creator_id  VARCHAR(512),
    created_at  TIMESTAMP,
    updated_at  TIMESTAMP,
    deleted_at  TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_message_feedbacks_session
    ON message_feedbacks(tenant_id, session_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_message_feedbacks_msg_source
    ON message_feedbacks(tenant_id, session_id, message_id, from_source);
