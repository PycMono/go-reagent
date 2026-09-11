-- Run only after authorized ownership backfill and verification, with writes stopped.
-- NOT NULL and FK changes deliberately fail on unresolved legacy rows.
ALTER TABLE agent_conversations
 MODIFY tenant_id VARCHAR(128) COLLATE utf8mb4_bin NOT NULL,
 MODIFY agent_id VARCHAR(32) COLLATE utf8mb4_bin NOT NULL,
 MODIFY agent_version_id VARCHAR(32) COLLATE utf8mb4_bin NOT NULL,
 MODIFY user_id VARCHAR(128) COLLATE utf8mb4_bin NOT NULL,
 MODIFY conversation_id VARCHAR(128) COLLATE utf8mb4_bin NOT NULL,
 ADD UNIQUE KEY uq_agent_conversations_tenant_owner (tenant_id,user_id,conversation_id),
 ADD UNIQUE KEY uq_agent_conversations_scope (tenant_id,agent_id,id),
 ADD CONSTRAINT fk_conversations_agent FOREIGN KEY (tenant_id,agent_id) REFERENCES agents(tenant_id,id),
 ADD CONSTRAINT fk_conversations_version FOREIGN KEY (tenant_id,agent_id,agent_version_id) REFERENCES agent_versions(tenant_id,agent_id,id),
 ADD CONSTRAINT ck_conversations_type CHECK (conversation_type IN ('chat','training'));
ALTER TABLE agent_conversations DROP INDEX uq_agent_conversations_owner;
