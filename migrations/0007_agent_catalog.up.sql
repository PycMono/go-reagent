CREATE TABLE agents (
    id VARCHAR(32) NOT NULL,
    tenant_id VARCHAR(128) NOT NULL,
    name VARCHAR(128) NOT NULL,
    description TEXT NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'enabled',
    presentation_json JSON NOT NULL,
    template_code VARCHAR(64) NOT NULL,
    bootstrap_key VARCHAR(64) NULL,
    active_version_id VARCHAR(32) NULL,
    active_training_session_id VARCHAR(32) NULL,
    row_version BIGINT UNSIGNED NOT NULL DEFAULT 0,
    created_by VARCHAR(128) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
        ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_agents_tenant_id (tenant_id, id),
    UNIQUE KEY uq_agents_bootstrap (tenant_id, bootstrap_key),
    CONSTRAINT ck_agents_status CHECK (status IN ('enabled','archived'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE agent_versions (
    id VARCHAR(32) NOT NULL,
    tenant_id VARCHAR(128) NOT NULL,
    agent_id VARCHAR(32) NOT NULL,
    version BIGINT UNSIGNED NOT NULL,
    bundle_commit VARCHAR(64) NOT NULL,
    bundle_tag VARCHAR(255) NOT NULL,
    bundle_digest VARCHAR(71) NOT NULL,
    model_config_json JSON NOT NULL,
    tool_policy_json JSON NOT NULL,
    runtime_config_json JSON NOT NULL,
    spec_digest VARCHAR(71) NOT NULL,
    validation_json JSON NOT NULL,
    source_training_session_id VARCHAR(32) NULL,
    published_by VARCHAR(128) NOT NULL,
    published_at DATETIME(6) NOT NULL,
    change_summary TEXT NOT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uq_versions_number (agent_id, version),
    UNIQUE KEY uq_versions_scope (tenant_id, agent_id, id),
    UNIQUE KEY uq_versions_training (source_training_session_id),
    CONSTRAINT fk_versions_agent FOREIGN KEY (tenant_id, agent_id)
        REFERENCES agents (tenant_id, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

ALTER TABLE agents ADD CONSTRAINT fk_agents_active_version
    FOREIGN KEY (tenant_id, id, active_version_id)
    REFERENCES agent_versions (tenant_id, agent_id, id);

ALTER TABLE agent_conversations
    ADD tenant_id VARCHAR(128) COLLATE utf8mb4_bin NULL,
    ADD conversation_type VARCHAR(16) NOT NULL DEFAULT 'chat',
    ADD agent_id VARCHAR(32) COLLATE utf8mb4_bin NULL,
    ADD agent_version_id VARCHAR(32) COLLATE utf8mb4_bin NULL,
    ADD follow_latest BOOLEAN NOT NULL DEFAULT TRUE;
