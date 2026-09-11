-- Only execute after legacy clients and profile_code readers/writers retire.
ALTER TABLE agent_conversations DROP COLUMN profile_code;
