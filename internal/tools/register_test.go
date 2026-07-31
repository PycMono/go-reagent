package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/PycMono/go-reagent/internal/config"
	"github.com/PycMono/go-reagent/internal/schema"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"
)

func TestRegisterProvidesRuntimeRegistry(t *testing.T) {
	var registry Registry
	app := fxtest.New(t,
		fx.Supply(config.WorkDir(t.TempDir())),
		Register,
		fx.Populate(&registry),
	)
	app.RequireStart()
	defer app.RequireStop()

	if registry == nil {
		t.Fatal("Register did not provide Registry")
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
