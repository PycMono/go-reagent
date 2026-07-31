package integration_test

import (
	"context"
	"testing"

	"github.com/PycMono/go-reagent/internal/config"
	"github.com/PycMono/go-reagent/internal/schema"
	"github.com/PycMono/go-reagent/internal/tools"
	"go.uber.org/fx/fxtest"
)

func TestNewRegistryRegistersToolsAndClosesThemOnStop(t *testing.T) {
	lifecycle := fxtest.NewLifecycle(t)
	registry, err := tools.NewRuntimeRegistry(lifecycle, config.WorkDir(t.TempDir()))
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	lifecycle.RequireStart()

	definitions := registry.GetAvailableTools()
	wantNames := []string{"apply_patch", "edit", "exec", "process", "read", "write"}
	if len(definitions) != len(wantNames) {
		t.Fatalf("definitions = %#v", definitions)
	}
	for index, want := range wantNames {
		if definitions[index].Name != want {
			t.Fatalf("definitions[%d].Name = %q, want %q", index, definitions[index].Name, want)
		}
	}
	lifecycle.RequireStop()

	for _, call := range []schema.ToolCall{
		{ID: "read-after-stop", Name: "read", Arguments: []byte(`{"path":"a.txt"}`)},
		{ID: "exec-after-stop", Name: "exec", Arguments: []byte(`{"command":"true"}`)},
		{ID: "process-after-stop", Name: "process", Arguments: []byte(`{"action":"list"}`)},
	} {
		if result, executeErr := registry.Execute(context.Background(), call, nil); executeErr != nil || !result.IsError {
			t.Fatalf("%s result after Stop = %#v, want closed-resource error", call.Name, result)
		}
	}
}
