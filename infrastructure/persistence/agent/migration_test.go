package agent

import (
	"os"
	"strings"
	"testing"
)

func TestAgentCatalogMigrationDefinesRecoverableInitialVersionSchema(t *testing.T) {
	upBytes, err := os.ReadFile("../../../migrations/0007_agent_catalog.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	up := string(upBytes)
	for _, want := range []string{
		"CREATE TABLE agents", "CREATE TABLE agent_versions", "uq_agents_bootstrap",
		"uq_versions_number", "uq_versions_scope", "uq_versions_training",
		"fk_versions_agent", "fk_agents_active_version",
		"ADD tenant_id VARCHAR(128) COLLATE utf8mb4_bin NULL",
		"ADD conversation_type VARCHAR(16) NOT NULL DEFAULT 'chat'",
		"ADD agent_id VARCHAR(32) COLLATE utf8mb4_bin NULL",
		"ADD agent_version_id VARCHAR(32) COLLATE utf8mb4_bin NULL",
		"ADD follow_latest BOOLEAN NOT NULL DEFAULT TRUE",
	} {
		if !strings.Contains(up, want) {
			t.Fatalf("up migration missing %q", want)
		}
	}
	for _, forbidden := range []string{"fk_agents_active_training", "fk_versions_source_training", "UNIQUE KEY uq_agents_template"} {
		if strings.Contains(up, forbidden) {
			t.Fatalf("up migration prematurely contains %q", forbidden)
		}
	}
	downBytes, err := os.ReadFile("../../../migrations/0007_agent_catalog.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	down := string(downBytes)
	if strings.Index(down, "DROP FOREIGN KEY fk_agents_active_version") > strings.Index(down, "DROP TABLE IF EXISTS agent_versions") {
		t.Fatal("down migration must remove active-version FK before dropping version table")
	}
	for _, want := range []string{"DROP COLUMN tenant_id", "DROP COLUMN conversation_type", "DROP COLUMN agent_id", "DROP COLUMN agent_version_id", "DROP COLUMN follow_latest"} {
		if !strings.Contains(down, want) {
			t.Fatalf("down migration missing %q", want)
		}
	}
}
