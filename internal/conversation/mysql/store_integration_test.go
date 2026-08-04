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

	"github.com/PycMono/go-reagent/internal/config"
	"github.com/PycMono/go-reagent/internal/conversation"
	conversationmysql "github.com/PycMono/go-reagent/internal/conversation/mysql"
	drivermysql "github.com/PycMono/go-reagent/internal/driver/mysql"
	"github.com/PycMono/go-reagent/internal/schema"
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
	connection, err := drivermysql.NewConnection(lifecycle, &config.Config{
		Conversation: config.ConversationConfig{Enabled: true, HistoryMessageLimit: 100},
		MySQL: config.MySQLConfig{
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
	migration, err := os.ReadFile("../../../migrations/0001_conversation_persistence.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range strings.Split(string(migration), ";") {
		statement = strings.TrimSpace(statement)
		if statement == "" {
			continue
		}
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("apply migration: %v", err)
		}
	}

	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	key := conversation.Key{UserID: "go-reagent-test-user-" + suffix, ConversationID: "conversation-" + suffix}
	t.Cleanup(func() {
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
	messages := []schema.Message{
		{Role: schema.RoleUser, Content: []schema.ContentBlock{schema.TextBlock("question")}},
		{Role: schema.RoleAssistant, Content: []schema.ContentBlock{schema.TextBlock("answer")}},
	}
	if err := store.AppendTurn(ctx, conversation.AppendRequest{
		ConversationPK: snapshot.ConversationPK, ExpectedVersion: snapshot.Version,
		RunID: "run-1", Messages: messages,
	}); err != nil {
		t.Fatal(err)
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
