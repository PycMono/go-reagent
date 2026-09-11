-- Stop writes and reconcile live processes first. Git history is never removed.
ALTER TABLE agent_versions DROP FOREIGN KEY fk_versions_training;
ALTER TABLE agents DROP FOREIGN KEY fk_agents_active_training;
DROP TABLE agent_training_sessions;
