package conversation_test

import (
	"context"
	"testing"

	"github.com/PycMono/go-reagent/config"
	"github.com/PycMono/go-reagent/conversation"
	conversationentity "github.com/PycMono/go-reagent/domain/entity/conversation"
	conversationrepo "github.com/PycMono/go-reagent/domain/repository/conversation"
	"github.com/PycMono/go-reagent/infrastructure/driver/mysql"
	"github.com/PycMono/go-reagent/pi/agent"
	"github.com/PycMono/go-reagent/pi/ai"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"
)

func TestRegisteredConversationGraphStartsDisabledWithoutMySQL(t *testing.T) {
	cfg := &config.Config{Conversation: config.ConversationConfig{HistoryMessageLimit: 100}}
	var (
		connection *mysql.Connection
		store      conversationrepo.Store
		runner     conversation.Runner
	)
	app := fxtest.New(t,
		fx.Supply(cfg),
		fx.Provide(
			func() agent.Runner { return &registeredRuntimeFake{} },
			func() conversationrepo.Store { return &registeredStoreFake{} },
		),
		mysql.Module,
		conversation.Module,
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
	store := &registeredStoreFake{snapshot: conversationentity.Snapshot{ConversationPK: 1}}
	runtime := &registeredRuntimeFake{result: agent.RunResult{NewMessages: []ai.Message{{
		Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("answer")},
	}}}}
	var runner conversation.Runner
	app := fxtest.New(t,
		fx.Supply(cfg),
		fx.Provide(
			func() agent.Runner { return runtime },
			func() conversationrepo.Store { return store },
		),
		conversation.Module,
		fx.Populate(&runner),
	)
	app.RequireStart()
	defer app.RequireStop()

	_, err := runner.Run(context.Background(), conversation.RunRequest{
		UserID: "user", ConversationID: "conversation",
		Input: ai.Message{Role: ai.RoleUser, Content: []ai.ContentBlock{ai.TextBlock("question")}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if store.limit != 100 {
		t.Fatalf("LoadOrCreate() limit = %d, want 100", store.limit)
	}
}

type registeredRuntimeFake struct {
	result agent.RunResult
}

func (f *registeredRuntimeFake) Run(context.Context, agent.RunRequest, agent.Reporter) (agent.RunResult, error) {
	return f.result, nil
}

type registeredStoreFake struct {
	snapshot conversationentity.Snapshot
	limit    int
}

func (f *registeredStoreFake) LoadOrCreate(_ context.Context, _ conversationentity.Key, limit int) (conversationentity.Snapshot, error) {
	f.limit = limit
	return f.snapshot, nil
}

func (f *registeredStoreFake) AppendTurn(context.Context, conversationentity.AppendRequest) error {
	return nil
}
