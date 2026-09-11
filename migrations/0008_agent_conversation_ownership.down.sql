-- Restore the old unique index FIRST. Cross-tenant duplicate owners cause this
-- statement to fail, preserving all customer rows and the tenant-aware index.
ALTER TABLE agent_conversations ADD UNIQUE KEY uq_agent_conversations_owner(user_id,conversation_id);
ALTER TABLE agent_conversations
 DROP FOREIGN KEY fk_conversations_agent,
 DROP FOREIGN KEY fk_conversations_version,
 DROP CHECK ck_conversations_type,
 DROP INDEX uq_agent_conversations_tenant_owner,
 DROP INDEX uq_agent_conversations_scope,
 MODIFY tenant_id VARCHAR(128) COLLATE utf8mb4_bin NULL,
 MODIFY agent_id VARCHAR(32) COLLATE utf8mb4_bin NULL,
 MODIFY agent_version_id VARCHAR(32) COLLATE utf8mb4_bin NULL;
