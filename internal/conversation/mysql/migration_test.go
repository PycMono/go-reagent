package mysql

import (
	"os"
	"strings"
	"testing"
)

func TestConversationMigrationDefinesRequiredSchema(t *testing.T) {
	content, err := os.ReadFile("../../../migrations/0001_conversation_persistence.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(content)
	for _, want := range []string{
		"agent_conversations",
		"agent_messages",
		"JSON NOT NULL",
		"uq_agent_conversations_owner",
		"uq_agent_messages_order",
		"fk_agent_messages_conversation",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration missing %q", want)
		}
	}
	if strings.Contains(sql, "AutoMigrate") {
		t.Fatal("migration must not use AutoMigrate")
	}
}
