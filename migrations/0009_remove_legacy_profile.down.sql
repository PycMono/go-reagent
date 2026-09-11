ALTER TABLE agent_conversations ADD COLUMN profile_code VARCHAR(64) NOT NULL DEFAULT 'general';
-- Historical profile values cannot be reconstructed from a schema rollback.
