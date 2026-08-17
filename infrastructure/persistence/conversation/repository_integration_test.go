//go:build integration

package conversation_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	commonerrors "github.com/PycMono/go-reagent/common/errors"
	"github.com/PycMono/go-reagent/config"
	conversationentity "github.com/PycMono/go-reagent/domain/entity/conversation"
	"github.com/PycMono/go-reagent/infrastructure/driver/mysql"
	conversationpersistence "github.com/PycMono/go-reagent/infrastructure/persistence/conversation"
	"github.com/PycMono/go-reagent/infrastructure/serviceimpl"
)

func TestMySQLConversationRepositoryRoundTrip(t *testing.T) {
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

	provider, err := mysql.NewProvider(&config.Config{
		Conversation: config.ConversationConfig{Enabled: true, HistoryMessageLimit: 100},
		MySQL: config.MySQLConfig{
			Host: host, Port: port, Database: database, User: user, Password: password,
			MaxOpen: 10, MaxIdle: 2, ConnLifetime: 60, ConnTimeout: 3, LogLevel: 3, SlowThreshold: 500,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	transactions, err := mysql.NewTransactionManager(provider)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	db := provider.UseDB(ctx)
	if db == nil {
		t.Fatal("MySQL connection returned nil database")
	}
	for _, migrationPath := range []string{
		"../../../migrations/0001_conversation_persistence.up.sql",
		"../../../migrations/0002_model_invocation_observability.up.sql",
		"../../../migrations/0003_web_chat.up.sql",
		"../../../migrations/0004_agent_profiles.up.sql",
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
	userID := "go-reagent-test-user-" + suffix
	conversationID := "conversation-" + suffix
	var cleanupConversationID string
	t.Cleanup(func() {
		if cleanupConversationID != "" {
			if err := db.Exec("DELETE FROM agent_model_invocations WHERE conversation_id = ?", cleanupConversationID).Error; err != nil {
				t.Errorf("cleanup invocations: %v", err)
			}
		}
		if err := db.Exec("DELETE FROM agent_conversations WHERE user_id = ? AND conversation_id = ?", userID, conversationID).Error; err != nil {
			t.Errorf("cleanup conversation: %v", err)
		}
	})

	idService, err := serviceimpl.NewIDService(0)
	if err != nil {
		t.Fatal(err)
	}
	repository := conversationpersistence.NewConversationRepo(provider, transactions, idService)
	created := &conversationentity.Conversation{UserID: userID, ConversationID: conversationID, Name: "Integration Chat", ProfileCode: "writing"}
	if err := repository.Create(ctx, created); err != nil {
		t.Fatal(err)
	}
	conversation, found, err := repository.FindByUserIDAndConversationID(ctx, userID, conversationID)
	if err != nil || !found {
		t.Fatalf("initial conversation = %#v, %v, %v", conversation, found, err)
	}
	if conversation.ID == "" || conversation.ProfileCode != "writing" || conversation.Version != 0 {
		t.Fatalf("initial conversation = %#v", conversation)
	}
	cleanupConversationID = conversation.ID
	messages := []*conversationentity.Message{
		{RunID: "run-1", Role: conversationentity.RoleUser, Payload: conversationentity.MessagePayload{Content: []conversationentity.ContentBlock{{Type: conversationentity.ContentTypeText, Text: "question"}}}},
		{RunID: "run-1", Role: conversationentity.RoleAssistant, Payload: conversationentity.MessagePayload{Content: []conversationentity.ContentBlock{{Type: conversationentity.ContentTypeText, Text: "answer"}}}},
	}
	invocations := []*conversationentity.ModelInvocation{{
		RunID:       "run-1",
		Sequence:    1,
		Phase:       conversationentity.InvocationPhaseAction,
		InputTokens: 120, OutputTokens: 30,
		InputPriceUSDPerMillionTokens: 0.15, OutputPriceUSDPerMillionTokens: 0.60,
		CostUSD: 0.000036, LatencyMS: 245, PlatformID: "test", Model: "test-model",
	},
	}
	if err := repository.AppendTurn(ctx, userID, conversationID, conversation.Version, messages, invocations); err != nil {
		t.Fatal(err)
	}
	var invocationCount int64
	if err := db.Table("agent_model_invocations").
		Where("conversation_id = ?", conversation.ID).
		Count(&invocationCount).Error; err != nil {
		t.Fatal(err)
	}
	if invocationCount != 1 {
		t.Fatalf("invocation count = %d, want 1", invocationCount)
	}
	loaded, found, err := repository.FindByUserIDAndConversationID(ctx, userID, conversationID)
	if err != nil || !found {
		t.Fatal(err)
	}
	loadedMessages, err := repository.ListMessagesByConversationID(ctx, conversation.ID, 100)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Version != 1 || len(loadedMessages) != 2 {
		t.Fatalf("loaded conversation = %#v", loaded)
	}
	err = repository.AppendTurn(ctx, userID, conversationID, conversation.Version, messages, invocations)
	if !errors.Is(err, commonerrors.ErrConflict) {
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
