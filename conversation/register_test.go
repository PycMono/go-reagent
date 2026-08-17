package conversation_test

import (
	"context"
	"testing"

	sqlsdk "github.com/PycMono/go-mysql-sdk"
	"github.com/PycMono/go-reagent/config"
	"github.com/PycMono/go-reagent/conversation"
	conversationentity "github.com/PycMono/go-reagent/domain/entity/conversation"
	conversationrepo "github.com/PycMono/go-reagent/domain/repository/conversation"
	"github.com/PycMono/go-reagent/infrastructure"
	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
	goredis "github.com/redis/go-redis/v9"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"
)

func TestRegisteredConversationGraphStartsDisabledWithoutMySQL(t *testing.T) {
	cfg := &config.Config{Conversation: config.ConversationConfig{HistoryMessageLimit: 100}}
	redisClient := goredis.NewUniversalClient(&goredis.UniversalOptions{Addrs: []string{"127.0.0.1:1"}})
	t.Cleanup(func() { _ = redisClient.Close() })
	app := fxtest.New(t,
		fx.Supply(cfg),
		fx.Replace(fx.Annotate(redisClient, fx.As(new(goredis.UniversalClient)))),
		fx.Provide(func() pi.Runner { return &registeredRuntimeFake{} }),
		infrastructure.Register,
		conversation.Register,
		fx.Invoke(func(sqlsdk.Provider, conversationrepo.IConversationRepository, conversation.Runner) {}),
	)
	app.RequireStart()
	defer app.RequireStop()
}

func TestRegisteredRunnerUsesConfiguredHistoryLimit(t *testing.T) {
	cfg := &config.Config{Conversation: config.ConversationConfig{HistoryMessageLimit: 100}}
	repository := &registeredRepositoryFake{conversation: conversationentity.Conversation{UserID: "user", ConversationID: "conversation"}}
	runtime := &registeredRuntimeFake{result: pi.RunResult{NewMessages: []ai.Message{{
		Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("answer")},
	}}}}
	var runner conversation.Runner
	app := fxtest.New(t,
		fx.Supply(cfg),
		fx.Provide(
			func() pi.Runner { return runtime },
			func() conversationrepo.IConversationRepository { return repository },
		),
		conversation.Register,
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
	if repository.limit != 100 {
		t.Fatalf("FindByUserIDAndConversationID() limit = %d, want 100", repository.limit)
	}
}

type registeredRuntimeFake struct {
	result pi.RunResult
}

func (f *registeredRuntimeFake) Run(context.Context, pi.RunRequest, pi.Reporter) (pi.RunResult, error) {
	return f.result, nil
}

type registeredRepositoryFake struct {
	conversation conversationentity.Conversation
	limit        int
}

func (f *registeredRepositoryFake) FindByUserIDAndConversationID(_ context.Context, _, _ string) (*conversationentity.Conversation, bool, error) {
	conversation := f.conversation
	return &conversation, true, nil
}

func (f *registeredRepositoryFake) Create(context.Context, *conversationentity.Conversation) error {
	return nil
}

func (f *registeredRepositoryFake) ListMessagesByConversationID(_ context.Context, _ string, limit int) ([]*conversationentity.Message, error) {
	f.limit = limit
	return nil, nil
}

func (f *registeredRepositoryFake) AppendTurn(context.Context, string, string, uint64, []*conversationentity.Message, []*conversationentity.ModelInvocation) error {
	return nil
}
