package web

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	ginsdk "github.com/PycMono/go-gin-sdk"
	chatservice "github.com/PycMono/go-reagent/application/service/chat"
	"github.com/PycMono/go-reagent/config"
	conversationrepo "github.com/PycMono/go-reagent/domain/repository/conversation"
	pagectl "github.com/PycMono/go-reagent/infrastructure/controller/http/page"
	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/ai/providers"
	"github.com/gin-gonic/gin"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"
)

func TestRegisterResolvesWebGraphWithoutCLIInputs(t *testing.T) {
	var (
		engine     *gin.Engine
		server     *ginsdk.HTTPServer
		service    *chatservice.Service
		store      conversationrepo.IConversationRepository
		management conversationrepo.IConversationManagementRepository
		page       *pagectl.Controller
	)
	err := fx.ValidateApp(
		Register,
		fx.Populate(&engine, &server, &service, &store, &management, &page),
	)
	if err != nil {
		t.Fatalf("Web Register graph is invalid: %v", err)
	}
}

func TestAgentRegisterIncludesExplicitBusinessTool(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("You are a test chat Agent."), 0o600); err != nil {
		t.Fatal(err)
	}
	var runtime pi.ToolRuntime
	app := fxtest.New(
		t,
		agentRegister,
		fx.Supply(
			pi.WorkDir(root),
			providers.Options{
				ID: "test", Protocol: providers.ProtocolOpenAI, BaseURL: "https://example.test/v1/",
				APIKey: "test-key", Model: "test-model", Pricing: &providers.Pricing{},
			},
		),
		fx.Provide(fx.Annotate(
			newRegisterTestBusinessTool,
			fx.As(new(ai.Tool)),
			fx.ResultTags(`group:"agent_tools"`),
		)),
		fx.Populate(&runtime),
	)
	app.RequireStart()
	t.Cleanup(app.RequireStop)

	definitions := runtime.Definitions()
	names := make([]string, len(definitions))
	for index, definition := range definitions {
		names[index] = definition.Name
	}
	if !slices.Equal(names, []string{"course_query", "read"}) {
		t.Fatalf("Web Agent tools = %v, want [course_query read]", names)
	}
}

type registerTestBusinessTool struct{}

func newRegisterTestBusinessTool() *registerTestBusinessTool {
	return &registerTestBusinessTool{}
}

func (*registerTestBusinessTool) Definition() ai.ToolDefinition {
	return ai.ToolDefinition{
		Name: "course_query",
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
		},
	}
}

func (*registerTestBusinessTool) Execute(context.Context, json.RawMessage, ai.UpdateEmitter) (ai.ToolOutput, error) {
	return ai.ToolOutput{Content: []ai.ContentBlock{ai.TextBlock("course")}}, nil
}

func TestValidateConfigRequiresPersistenceAndLoopback(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.Config
		want string
	}{
		{name: "nil", want: "config"},
		{name: "persistence disabled", cfg: &config.Config{HTTP: config.HTTPConfig{Host: "127.0.0.1"}}, want: "conversation.enabled"},
		{name: "public bind", cfg: &config.Config{Conversation: config.ConversationConfig{Enabled: true}, HTTP: config.HTTPConfig{Host: "0.0.0.0"}}, want: "loopback"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateConfig(test.cfg); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validateConfig() error = %v, want %q", err, test.want)
			}
		})
	}
	if err := validateConfig(&config.Config{
		Conversation: config.ConversationConfig{Enabled: true}, HTTP: config.HTTPConfig{Host: "::1"},
	}); err != nil {
		t.Fatalf("loopback config rejected: %v", err)
	}
}

func TestAgentRegisterUsesDirectReadOnlyRuntime(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("You are a test chat Agent."), 0o600); err != nil {
		t.Fatal(err)
	}
	var (
		runtime  pi.ToolRuntime
		thinking pi.ThinkingEnabled
	)
	app := fxtest.New(
		t,
		agentRegister,
		fx.Supply(
			pi.WorkDir(root),
			providers.Options{
				ID: "test", Protocol: providers.ProtocolOpenAI, BaseURL: "https://example.test/v1/",
				APIKey: "test-key", Model: "test-model", Pricing: &providers.Pricing{},
			},
		),
		fx.Populate(&runtime, &thinking),
	)
	app.RequireStart()
	t.Cleanup(app.RequireStop)

	definitions := runtime.Definitions()
	names := make([]string, len(definitions))
	for index, definition := range definitions {
		names[index] = definition.Name
	}
	if !slices.Equal(names, []string{"read"}) {
		t.Fatalf("Web Agent tools = %v, want [read]", names)
	}
	if bool(thinking) {
		t.Fatal("Web Agent unexpectedly enables the separate Thinking phase")
	}
}
