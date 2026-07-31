package engine

import (
	"context"
	"testing"

	"github.com/PycMono/go-reagent/internal/config"
	ctxpkg "github.com/PycMono/go-reagent/internal/context"
	"github.com/PycMono/go-reagent/internal/provider"
	"github.com/PycMono/go-reagent/internal/schema"
	"github.com/PycMono/go-reagent/internal/tools"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"
)

type registerProvider struct{}

func (*registerProvider) Generate(context.Context, []schema.Message, []schema.ToolDefinition) (*schema.Message, error) {
	return &schema.Message{
		Role:    schema.RoleAssistant,
		Content: []schema.ContentBlock{schema.TextBlock("done")},
	}, nil
}

type registerRegistry struct{}

func (*registerRegistry) GetAvailableTools() []schema.ToolDefinition { return nil }

func (*registerRegistry) Execute(context.Context, schema.ToolCall) schema.ToolResult {
	return schema.ToolResult{}
}

func TestNewAgentEngineUsesInjectedContextComponents(t *testing.T) {
	workDir := t.TempDir()
	composer := ctxpkg.NewPromptComposer(workDir)
	skillLoader := ctxpkg.NewSkillLoader(workDir)

	agentEngine := NewAgentEngine(
		&registerProvider{},
		&registerRegistry{},
		composer,
		skillLoader,
		workDir,
		false,
	)

	if agentEngine.composer != composer || agentEngine.skillLoader != skillLoader {
		t.Fatalf("AgentEngine context components were not injected")
	}
}

func TestRegisterProvidesAgent(t *testing.T) {
	var agent Agent
	app := fxtest.New(t,
		fx.Provide(func() provider.LLMProvider { return &registerProvider{} }),
		fx.Provide(func() tools.Registry { return &registerRegistry{} }),
		fx.Supply(config.WorkDir(t.TempDir())),
		ctxpkg.Register,
		Register,
		fx.Populate(&agent),
	)
	app.RequireStart()
	defer app.RequireStop()

	if agent == nil {
		t.Fatal("Register did not provide Agent")
	}
	agentEngine, ok := agent.(*AgentEngine)
	if !ok {
		t.Fatalf("Agent type = %T, want *AgentEngine", agent)
	}
	if agentEngine.WorkDir == "" || agentEngine.composer == nil || agentEngine.skillLoader == nil {
		t.Fatalf("AgentEngine workspace context = %#v", agentEngine)
	}
	if !agentEngine.EnableThinking || agentEngine.MaxParallelTools != defaultMaxParallelTools {
		t.Fatalf("AgentEngine defaults = thinking:%v parallel:%d", agentEngine.EnableThinking, agentEngine.MaxParallelTools)
	}
}
