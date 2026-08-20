package web

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	ginsdk "github.com/PycMono/go-gin-sdk"
	chatservice "github.com/PycMono/go-reagent/application/service/chat"
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
	want := []string{"calculate", "course_query", "get_current_time", "read"}
	if !slices.Equal(names, want) {
		t.Fatalf("Web Agent tools = %v, want %v", names, want)
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

func TestAgentRegisterUsesDirectGeneralChatRuntime(t *testing.T) {
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
	want := []string{"calculate", "get_current_time", "read"}
	if !slices.Equal(names, want) {
		t.Fatalf("Web Agent tools = %v, want %v", names, want)
	}
	for _, forbidden := range []string{"apply_patch", "edit", "exec", "process", "write"} {
		if slices.Contains(names, forbidden) {
			t.Fatalf("Web Agent exposed %q", forbidden)
		}
	}
	if bool(thinking) {
		t.Fatal("Web Agent unexpectedly enables the separate Thinking phase")
	}
}
