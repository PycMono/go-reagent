CREATE TABLE IF NOT EXISTS agent_model_invocations (
    id VARCHAR(32) NOT NULL,
    conversation_id VARCHAR(32) NOT NULL,
    turn_version BIGINT UNSIGNED NOT NULL,
    run_id VARCHAR(128) NOT NULL DEFAULT '',
    sequence INT UNSIGNED NOT NULL,
    phase VARCHAR(16) NOT NULL,
    platform_id VARCHAR(128) NOT NULL,
    model VARCHAR(255) NOT NULL,
    input_tokens BIGINT UNSIGNED NOT NULL,
    output_tokens BIGINT UNSIGNED NOT NULL,
    input_price_usd_per_million_tokens DECIMAL(20,12) NOT NULL,
    output_price_usd_per_million_tokens DECIMAL(20,12) NOT NULL,
    cost_usd DECIMAL(20,12) NOT NULL,
    latency_ms BIGINT UNSIGNED NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_agent_model_invocations_turn_sequence (conversation_id, turn_version, sequence),
    KEY idx_agent_model_invocations_conversation_time (conversation_id, created_at),
    CONSTRAINT fk_agent_model_invocations_conversation FOREIGN KEY (conversation_id)
        REFERENCES agent_conversations (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
