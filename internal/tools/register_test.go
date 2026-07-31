package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/internal/config"
	"github.com/PycMono/go-reagent/internal/schema"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"
)

func TestRegisterProvidesRuntimeRegistry(t *testing.T) {
	var (
		registry   Registry
		workspace  *Workspace
		supervisor *ProcessSupervisor
	)
	app := fxtest.New(t,
		fx.Supply(config.WorkDir(t.TempDir())),
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
	duplicateRead := testTool("read", func(context.Context, json.RawMessage, UpdateEmitter) (schema.ToolOutput, error) {
		return schema.ToolOutput{Content: []schema.ContentBlock{schema.TextBlock("duplicate")}}, nil
	})
	app := fx.New(
		fx.NopLogger,
		fx.Supply(config.WorkDir(t.TempDir())),
		Register,
		fx.Provide(fx.Annotate(
			func() Tool { return duplicateRead },
			fx.ResultTags(`group:"agent_tools"`),
		)),
		fx.Invoke(func(Registry) {}),
	)
	if err := app.Err(); err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("app.Err() = %v, want duplicate registration error", err)
	}
}

func TestRegisterSortsReversedMiddlewareGroupByOrderThenName(t *testing.T) {
	var sequence []string
	registration := func(name string) MiddlewareRegistration {
		return MiddlewareRegistration{
			Name:  name,
			Order: 1000,
			Middleware: func(next Handler) Handler {
				return func(ctx context.Context, execution Execution, emit UpdateEmitter) (schema.ToolOutput, error) {
					sequence = append(sequence, name)
					return next(ctx, execution, emit)
				}
			},
		}
	}
	probe := testTool("order_probe", func(context.Context, json.RawMessage, UpdateEmitter) (schema.ToolOutput, error) {
		return schema.ToolOutput{Content: []schema.ContentBlock{schema.TextBlock("ok")}}, nil
	})
	var registry Registry
	app := fxtest.New(t,
		fx.Supply(config.WorkDir(t.TempDir())),
		Register,
		fx.Provide(
			fx.Annotate(func() Tool { return probe }, fx.ResultTags(`group:"agent_tools"`)),
			fx.Annotate(func() MiddlewareRegistration { return registration("zeta") }, fx.ResultTags(`group:"tool_middlewares"`)),
			fx.Annotate(func() MiddlewareRegistration { return registration("alpha") }, fx.ResultTags(`group:"tool_middlewares"`)),
		),
		fx.Populate(&registry),
	)
	app.RequireStart()
	defer app.RequireStop()

	result, err := registry.Execute(context.Background(), schema.ToolCall{ID: "probe", Name: "order_probe", Arguments: json.RawMessage(`{"text":"x"}`)}, nil)
	if err != nil || result.IsError {
		t.Fatalf("Execute() = (%#v, %v)", result, err)
	}
	if got := strings.Join(sequence, ","); got != "alpha,zeta" {
		t.Fatalf("middleware sequence = %q, want alpha,zeta", got)
	}
}

func TestRuntimeRegistryRejectsLegacyExecAndProcessFields(t *testing.T) {
	var registry Registry
	app := fxtest.New(t,
		fx.Supply(config.WorkDir(t.TempDir())),
		Register,
		fx.Populate(&registry),
	)
	app.RequireStart()
	defer app.RequireStop()

	for _, call := range []schema.ToolCall{
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
