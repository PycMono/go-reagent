ALTER TABLE agents DROP FOREIGN KEY fk_agents_active_version;

DROP TABLE IF EXISTS agent_versions;
DROP TABLE IF EXISTS agents;

ALTER TABLE agent_conversations
    DROP COLUMN tenant_id,
    DROP COLUMN conversation_type,
    DROP COLUMN agent_id,
    DROP COLUMN agent_version_id,
    DROP COLUMN follow_latest;
