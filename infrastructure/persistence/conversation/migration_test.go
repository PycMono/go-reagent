package conversation

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

func TestModelInvocationMigrationDefinesRequiredSchema(t *testing.T) {
	content, err := os.ReadFile("../../../migrations/0002_model_invocation_observability.up.sql")
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

	down, err := os.ReadFile("../../../migrations/0002_model_invocation_observability.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(string(down)), "DROP TABLE IF EXISTS agent_model_invocations;"; got != want {
		t.Fatalf("down migration = %q, want %q", got, want)
	}
}

func TestWebChatMigrationDefinesConversationName(t *testing.T) {
	up, err := os.ReadFile("../../../migrations/0003_web_chat.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"ALTER TABLE agent_conversations",
		"ADD COLUMN name VARCHAR(255) NOT NULL DEFAULT 'Untitled Chat'",
	} {
		if !strings.Contains(string(up), want) {
			t.Fatalf("up migration missing %q", want)
		}
	}

	down, err := os.ReadFile("../../../migrations/0003_web_chat.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(string(down)), "ALTER TABLE agent_conversations DROP COLUMN name;"; got != want {
		t.Fatalf("down migration = %q, want %q", got, want)
	}
}

func TestAgentProfileMigrationDefinesConversationProfile(t *testing.T) {
	up, err := os.ReadFile("../../../migrations/0004_agent_profiles.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"ALTER TABLE agent_conversations",
		"ADD COLUMN profile_code VARCHAR(64) NOT NULL DEFAULT 'general' AFTER name",
		"idx_agent_conversations_user_profile_updated",
		"user_id, profile_code, updated_at, id",
	} {
		if !strings.Contains(string(up), want) {
			t.Fatalf("up migration missing %q", want)
		}
	}
	down, err := os.ReadFile("../../../migrations/0004_agent_profiles.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"DROP INDEX idx_agent_conversations_user_profile_updated", "DROP COLUMN profile_code"} {
		if !strings.Contains(string(down), want) {
			t.Fatalf("down migration missing %q", want)
		}
	}
}

// TestInvocationLedgerTracingMigration 锁定阶段 3 迁移（设计 §10.1）：
// 扩展现有表，不平行新建账本；旧行由列默认值覆盖。
func TestInvocationLedgerTracingMigration(t *testing.T) {
	content, err := os.ReadFile("../../../migrations/0005_invocation_ledger_tracing.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(content)
	for _, want := range []string{
		"ALTER TABLE agent_model_invocations",
		"trace_id VARCHAR(32) NULL",
		"provider_request_index INT UNSIGNED NULL",
		"outcome VARCHAR(32) NOT NULL DEFAULT 'accepted'",
		"cost_quality VARCHAR(16) NOT NULL DEFAULT 'estimated'",
		"ttft_ms BIGINT UNSIGNED NULL",
		"finish_reason VARCHAR(32) NULL",
		"error_code VARCHAR(64) NULL",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration 0005 missing %q", want)
		}
	}
	if strings.Contains(sql, "CREATE TABLE") {
		t.Fatal("阶段 3 必须扩展现有表，不得平行新建账本")
	}
	// 当前没有按 TraceID 反查 MySQL 的路径，不建 trace_id 索引。
	if strings.Contains(sql, "KEY") || strings.Contains(sql, "INDEX") {
		t.Fatal("不得为 trace_id 建索引")
	}

	down, err := os.ReadFile("../../../migrations/0005_invocation_ledger_tracing.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"DROP COLUMN trace_id", "DROP COLUMN outcome", "DROP COLUMN ttft_ms"} {
		if !strings.Contains(string(down), want) {
			t.Fatalf("down migration 0005 missing %q", want)
		}
	}
}

// TestInvocationUsageEnhancementMigration 锁定阶段 4 迁移（设计 §10.1）。
func TestInvocationUsageEnhancementMigration(t *testing.T) {
	content, err := os.ReadFile("../../../migrations/0006_invocation_usage_enhancement.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(content)
	for _, want := range []string{
		"ALTER TABLE agent_model_invocations",
		"cache_read_tokens BIGINT UNSIGNED NOT NULL DEFAULT 0",
		"cache_write_tokens BIGINT UNSIGNED NOT NULL DEFAULT 0",
		"reasoning_tokens BIGINT UNSIGNED NOT NULL DEFAULT 0",
		"cache_read_price_usd_per_million_tokens DECIMAL(20,12) NOT NULL DEFAULT 0",
		"cache_write_price_usd_per_million_tokens DECIMAL(20,12) NOT NULL DEFAULT 0",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration 0006 missing %q", want)
		}
	}
	down, err := os.ReadFile("../../../migrations/0006_invocation_usage_enhancement.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(down), "DROP COLUMN cache_read_tokens") {
		t.Fatal("down migration 0006 missing cache_read_tokens")
	}
}
