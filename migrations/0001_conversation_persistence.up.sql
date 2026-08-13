CREATE TABLE IF NOT EXISTS agent_conversations (
    id VARCHAR(32) NOT NULL,
    user_id VARCHAR(128) NOT NULL,
    conversation_id VARCHAR(128) NOT NULL,
    version BIGINT UNSIGNED NOT NULL DEFAULT 0,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_agent_conversations_owner (user_id, conversation_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS agent_messages (
    id VARCHAR(32) NOT NULL,
    conversation_id VARCHAR(32) NOT NULL,
    turn_version BIGINT UNSIGNED NOT NULL,
    ordinal INT UNSIGNED NOT NULL,
    run_id VARCHAR(128) NOT NULL DEFAULT '',
    role VARCHAR(32) NOT NULL,
    payload JSON NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_agent_messages_order (conversation_id, turn_version, ordinal),
    CONSTRAINT fk_agent_messages_conversation FOREIGN KEY (conversation_id)
        REFERENCES agent_conversations (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
