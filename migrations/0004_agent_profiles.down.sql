ALTER TABLE agent_conversations
    DROP INDEX idx_agent_conversations_user_profile_updated,
    DROP COLUMN profile_code;
