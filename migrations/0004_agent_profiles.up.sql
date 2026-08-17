ALTER TABLE agent_conversations
    ADD COLUMN profile_code VARCHAR(64) NOT NULL DEFAULT 'general' AFTER name,
    ADD INDEX idx_agent_conversations_user_profile_updated
        (user_id, profile_code, updated_at, id);
