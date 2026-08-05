package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/agent"
	"github.com/PycMono/go-reagent/ai"
	workspacepkg "github.com/PycMono/go-reagent/internal/workspace"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"
)

func TestRegisterProvidesRuntimeRegistry(t *testing.T) {
	var (
		registry   agent.Registry
		workspace  *Workspace
		supervisor *ProcessSupervisor
	)
	app := fxtest.New(t,
		fx.Supply(workspacepkg.WorkDir(t.TempDir())),
		Register,
		fx.Populate(&registry, &workspace, &supervisor),
	)
	app.RequireStart()
	defer app.RequireStop()

	if registry == nil {
		t.Fatal("Register did not provide Registry")
	}
	if workspace == nil || supervisor == nil {
		t.Fatalf("shared resources = workspace:%#v supervisor:%#v", workspace, supervisor)
	}
	definitions := registry.GetAvailableTools()
	if len(definitions) != 6 {
		t.Fatalf("tool definitions = %#v, want 6 tools", definitions)
	}
	wantNames := []string{"apply_patch", "edit", "exec", "process", "read", "write"}
	for index, want := range wantNames {
		if definitions[index].Name != want {
			t.Fatalf("definitions[%d].Name = %q, want %q", index, definitions[index].Name, want)
		}
	}
}

func TestRegisterRejectsDuplicateToolGroupNames(t *testing.T) {
	duplicateRead := registerTestTool{name: "read", execute: func(context.Context, json.RawMessage, agent.UpdateEmitter) (agent.ToolOutput, error) {
		return agent.ToolOutput{Content: []ai.ContentBlock{ai.TextBlock("duplicate")}}, nil
	}}
	app := fx.New(
		fx.NopLogger,
		fx.Supply(workspacepkg.WorkDir(t.TempDir())),
		Register,
		fx.Provide(fx.Annotate(
			func() agent.Tool { return duplicateRead },
			fx.ResultTags(`group:"agent_tools"`),
		)),
		fx.Invoke(func(agent.Registry) {}),
	)
	if err := app.Err(); err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("app.Err() = %v, want duplicate registration error", err)
	}
}

func TestRuntimeRegistryRejectsLegacyExecAndProcessFields(t *testing.T) {
	var registry agent.Registry
	app := fxtest.New(t,
		fx.Supply(workspacepkg.WorkDir(t.TempDir())),
		Register,
		fx.Populate(&registry),
	)
	app.RequireStart()
	defer app.RequireStop()

	for _, call := range []ai.ToolCall{
		{ID: "legacy-exec-timeout", Name: "exec", Arguments: json.RawMessage(`{"command":"true","timeout_ms":1000}`)},
		{ID: "legacy-exec-yield", Name: "exec", Arguments: json.RawMessage(`{"command":"true","yield_ms":1}`)},
		{ID: "legacy-process-session", Name: "process", Arguments: json.RawMessage(`{"action":"poll","session_id":"x"}`)},
		{ID: "legacy-process-wait", Name: "process", Arguments: json.RawMessage(`{"action":"poll","sessionId":"x","wait_ms":1}`)},
	} {
		result, err := registry.Execute(context.Background(), call, nil)
		if err != nil || !result.IsError {
			t.Fatalf("Execute(%s) = (%#v, %v)", call.ID, result, err)
		}
	}
}

type registerTestTool struct {
	name    string
	execute func(context.Context, json.RawMessage, agent.UpdateEmitter) (agent.ToolOutput, error)
}

func (t registerTestTool) Definition() ai.ToolDefinition {
	return ai.ToolDefinition{
		Name:        t.name,
		Description: "test tool",
		InputSchema: map[string]any{"type": "object"},
	}
}

func (t registerTestTool) Execute(ctx context.Context, raw json.RawMessage, emit agent.UpdateEmitter) (agent.ToolOutput, error) {
	return t.execute(ctx, raw, emit)
}
