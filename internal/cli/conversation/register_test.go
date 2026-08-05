package conversation_test

import (
	"context"
	"testing"

	"github.com/PycMono/go-reagent"
	"github.com/PycMono/go-reagent/pi/agent"
	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/internal/cli/conversation"
	conversationmysql "github.com/PycMono/go-reagent/internal/cli/conversation/mysql"
	drivermysql "github.com/PycMono/go-reagent/internal/cli/driver/mysql"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"
)

func TestRegisteredConversationGraphStartsDisabledWithoutMySQL(t *testing.T) {
	cfg := &reagent.Config{Conversation: reagent.ConversationConfig{HistoryMessageLimit: 100}}
	var (
		connection *drivermysql.Connection
		store      conversation.Store
		runner     conversation.Runner
	)
	app := fxtest.New(t,
		fx.Supply(cfg),
		fx.Provide(func() agent.Runner { return &registeredRuntimeFake{} }),
		drivermysql.Module,
		conversationmysql.Module,
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
	cfg := &reagent.Config{Conversation: reagent.ConversationConfig{HistoryMessageLimit: 100}}
	store := &registeredStoreFake{snapshot: conversation.Snapshot{ConversationPK: 1}}
	runtime := &registeredRuntimeFake{result: agent.RunResult{NewMessages: []ai.Message{{
		Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("answer")},
	}}}}
	var runner conversation.Runner
	app := fxtest.New(t,
		fx.Supply(cfg),
		fx.Provide(
			func() agent.Runner { return runtime },
			func() conversation.Store { return store },
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
