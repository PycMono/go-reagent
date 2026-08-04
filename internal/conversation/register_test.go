package conversation_test

import (
	"context"
	"testing"

	"github.com/PycMono/go-reagent/internal/config"
	"github.com/PycMono/go-reagent/internal/conversation"
	conversationmysql "github.com/PycMono/go-reagent/internal/conversation/mysql"
	drivermysql "github.com/PycMono/go-reagent/internal/driver/mysql"
	"github.com/PycMono/go-reagent/internal/engine"
	"github.com/PycMono/go-reagent/internal/schema"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"
)

func TestRegisteredConversationGraphStartsDisabledWithoutMySQL(t *testing.T) {
	cfg := &config.Config{Conversation: config.ConversationConfig{HistoryMessageLimit: 100}}
	var (
		connection *drivermysql.Connection
		store      conversation.Store
		runner     conversation.Runner
	)
	app := fxtest.New(t,
		fx.Supply(cfg),
		fx.Provide(func() engine.AgentRuntime { return &registeredRuntimeFake{} }),
		drivermysql.Register,
		conversationmysql.Register,
		conversation.Register,
		fx.Populate(&connection, &store, &runner),
	)
	app.RequireStart()
	defer app.RequireStop()
	if connection == nil || store == nil || runner == nil {
		t.Fatalf("registered values = %#v, %#v, %#v", connection, store, runner)
	}
}

func TestRegisteredRunnerUsesConfiguredHistoryLimit(t *testing.T) {
	cfg := &config.Config{Conversation: config.ConversationConfig{HistoryMessageLimit: 100}}
	store := &registeredStoreFake{snapshot: conversation.Snapshot{ConversationPK: 1}}
	runtime := &registeredRuntimeFake{result: schema.RunResult{NewMessages: []schema.Message{{
		Role: schema.RoleAssistant, Content: []schema.ContentBlock{schema.TextBlock("answer")},
	}}}}
	var runner conversation.Runner
	app := fxtest.New(t,
		fx.Supply(cfg),
		fx.Provide(
			func() engine.AgentRuntime { return runtime },
			func() conversation.Store { return store },
		),
		conversation.Register,
		fx.Populate(&runner),
	)
	app.RequireStart()
	defer app.RequireStop()

	_, err := runner.Run(context.Background(), conversation.RunRequest{
		UserID: "user", ConversationID: "conversation",
		Input: schema.Message{Role: schema.RoleUser, Content: []schema.ContentBlock{schema.TextBlock("question")}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if store.limit != 100 {
		t.Fatalf("LoadOrCreate() limit = %d, want 100", store.limit)
	}
}

type registeredRuntimeFake struct {
	result schema.RunResult
}

func (f *registeredRuntimeFake) Run(context.Context, schema.RunRequest, engine.Reporter) (schema.RunResult, error) {
	return f.result, nil
}

type registeredStoreFake struct {
	snapshot conversation.Snapshot
	limit    int
}

func (f *registeredStoreFake) LoadOrCreate(_ context.Context, _ conversation.Key, limit int) (conversation.Snapshot, error) {
	f.limit = limit
	return f.snapshot, nil
}

func (f *registeredStoreFake) AppendTurn(context.Context, conversation.AppendRequest) error {
	return nil
}
