//go:build integration

package mysql_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/PycMono/go-reagent"
	"github.com/PycMono/go-reagent/agent"
	"github.com/PycMono/go-reagent/ai"
	"github.com/PycMono/go-reagent/internal/cli/conversation"
	conversationmysql "github.com/PycMono/go-reagent/internal/cli/conversation/mysql"
	drivermysql "github.com/PycMono/go-reagent/internal/cli/driver/mysql"
	"go.uber.org/fx/fxtest"
)

func TestMySQLStoreRoundTrip(t *testing.T) {
	host := requiredMySQLTestEnv(t, "MYSQL_TEST_HOST")
	database := requiredMySQLTestEnv(t, "MYSQL_TEST_DATABASE")
	user := requiredMySQLTestEnv(t, "MYSQL_TEST_USER")
	password := requiredMySQLTestEnv(t, "MYSQL_TEST_PASSWORD")
	port := 3306
	if value := strings.TrimSpace(os.Getenv("MYSQL_TEST_PORT")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			t.Fatalf("MYSQL_TEST_PORT: %v", err)
		}
		port = parsed
	}

	lifecycle := fxtest.NewLifecycle(t)
	connection, err := drivermysql.NewConnection(lifecycle, &reagent.Config{
		Conversation: reagent.ConversationConfig{Enabled: true, HistoryMessageLimit: 100},
		MySQL: reagent.MySQLConfig{
			Host: host, Port: port, Database: database, User: user, Password: password,
			MaxOpen: 10, MaxIdle: 2, ConnLifetime: 60, ConnTimeout: 3, LogLevel: 3, SlowThreshold: 500,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	lifecycle.RequireStart()
	t.Cleanup(lifecycle.RequireStop)

	ctx := context.Background()
	db := connection.UseDB(ctx)
	if db == nil {
		t.Fatal("MySQL connection returned nil database")
	}
	for _, migrationPath := range []string{
		"../../../../migrations/0001_conversation_persistence.up.sql",
		"../../../../migrations/0002_model_invocation_observability.up.sql",
	} {
		migration, err := os.ReadFile(migrationPath)
		if err != nil {
			t.Fatal(err)
		}
		for _, statement := range strings.Split(string(migration), ";") {
			statement = strings.TrimSpace(statement)
			if statement == "" {
				continue
			}
			if err := db.Exec(statement).Error; err != nil {
				t.Fatalf("apply migration %s: %v", migrationPath, err)
			}
		}
	}

	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	key := conversation.Key{UserID: "go-reagent-test-user-" + suffix, ConversationID: "conversation-" + suffix}
	var cleanupConversationPK uint64
	t.Cleanup(func() {
		if cleanupConversationPK != 0 {
			if err := db.Exec("DELETE FROM agent_model_invocations WHERE conversation_pk = ?", cleanupConversationPK).Error; err != nil {
				t.Errorf("cleanup invocations: %v", err)
			}
		}
		if err := db.Exec("DELETE FROM agent_conversations WHERE user_id = ? AND conversation_id = ?", key.UserID, key.ConversationID).Error; err != nil {
			t.Errorf("cleanup conversation: %v", err)
		}
	})

	store := conversationmysql.NewStore(connection, connection)
	snapshot, err := store.LoadOrCreate(ctx, key, 100)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ConversationPK == 0 || snapshot.Version != 0 || len(snapshot.Messages) != 0 {
		t.Fatalf("initial snapshot = %#v", snapshot)
	}
	cleanupConversationPK = snapshot.ConversationPK
	messages := []ai.Message{
		{Role: ai.RoleUser, Content: []ai.ContentBlock{ai.TextBlock("question")}},
		{Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("answer")}},
	}
	if err := store.AppendTurn(ctx, conversation.AppendRequest{
		ConversationPK: snapshot.ConversationPK, ExpectedVersion: snapshot.Version,
		RunID: "run-1", Messages: messages,
		Invocations: []agent.ModelInvocation{{
			Sequence: 1,
			Phase:    agent.ModelInvocationPhaseAction,
			Usage: ai.Usage{
				InputTokens:                    120,
				OutputTokens:                   30,
				InputPriceUSDPerMillionTokens:  0.15,
				OutputPriceUSDPerMillionTokens: 0.60,
				CostUSD:                        0.000036,
				LatencyMS:                      245,
				PlatformID:                     "test",
				Model:                          "test-model",
			},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	var invocationCount int64
	if err := db.Table("agent_model_invocations").
		Where("conversation_pk = ?", snapshot.ConversationPK).
		Count(&invocationCount).Error; err != nil {
		t.Fatal(err)
	}
	if invocationCount != 1 {
		t.Fatalf("invocation count = %d, want 1", invocationCount)
	}
	loaded, err := store.LoadOrCreate(ctx, key, 100)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Version != 1 || len(loaded.Messages) != 2 {
		t.Fatalf("loaded snapshot = %#v", loaded)
	}
	err = store.AppendTurn(ctx, conversation.AppendRequest{
		ConversationPK: snapshot.ConversationPK, ExpectedVersion: snapshot.Version,
		Messages: messages,
	})
	if !errors.Is(err, conversation.ErrConflict) {
		t.Fatalf("stale AppendTurn() error = %v, want ErrConflict", err)
	}
}

func requiredMySQLTestEnv(t *testing.T, name string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		t.Skip(fmt.Sprintf("%s is not set", name))
	}
	return value
}
