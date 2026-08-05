CREATE TABLE IF NOT EXISTS agent_model_invocations (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    conversation_pk BIGINT UNSIGNED NOT NULL,
    turn_version BIGINT UNSIGNED NOT NULL,
    run_id VARCHAR(128) NULL,
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
    UNIQUE KEY uq_agent_model_invocations_turn_sequence (conversation_pk, turn_version, sequence),
    KEY idx_agent_model_invocations_conversation_time (conversation_pk, created_at),
    CONSTRAINT fk_agent_model_invocations_conversation FOREIGN KEY (conversation_pk)
        REFERENCES agent_conversations (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
