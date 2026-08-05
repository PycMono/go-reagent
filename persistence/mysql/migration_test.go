package mysql

import (
	"os"
	"strings"
	"testing"
)

func TestConversationMigrationDefinesRequiredSchema(t *testing.T) {
	content, err := os.ReadFile("../../migrations/0001_conversation_persistence.up.sql")
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

func TestModelInvocationMigrationDefinesRequiredSchema(t *testing.T) {
	content, err := os.ReadFile("../../migrations/0002_model_invocation_observability.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(content)
	for _, want := range []string{
		"agent_model_invocations",
		"input_tokens",
		"output_tokens",
		"input_price_usd_per_million_tokens DECIMAL(20,12)",
		"output_price_usd_per_million_tokens DECIMAL(20,12)",
		"cost_usd DECIMAL(20,12)",
		"latency_ms",
		"phase",
		"platform_id",
		"model",
		"fk_agent_model_invocations_conversation",
		"idx_agent_model_invocations_conversation_time",
		"uq_agent_model_invocations_turn_sequence",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration missing %q", want)
		}
	}

	down, err := os.ReadFile("../../migrations/0002_model_invocation_observability.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(string(down)), "DROP TABLE IF EXISTS agent_model_invocations;"; got != want {
		t.Fatalf("down migration = %q, want %q", got, want)
	}
}
