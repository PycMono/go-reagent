package pi

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/PycMono/go-reagent/pi/ai/providers"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"
)

func TestReadOnlyToolsRegisterExposesOnlyRead(t *testing.T) {
	got := resolveRegisteredToolNames(t, ReadOnlyToolsRegister)
	if want := []string{"read"}; !slices.Equal(got, want) {
		t.Fatalf("tool names = %v, want %v", got, want)
	}
}

func TestCodingToolsRegisterPreservesCompleteDefaultSet(t *testing.T) {
	got := resolveRegisteredToolNames(t, CodingToolsRegister)
	want := []string{"apply_patch", "edit", "exec", "process", "read", "write"}
	if !slices.Equal(got, want) {
		t.Fatalf("tool names = %v, want %v", got, want)
	}
}

func TestCoreRegisterAllowsEmptyToolGroup(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("You are a test Agent."), 0o600); err != nil {
		t.Fatal(err)
	}
	var runtime ToolRuntime
	app := fxtest.New(
		t,
		CoreRegister,
		fx.Supply(
			WorkDir(root),
			ThinkingEnabled(false),
			providers.Options{
				ID: "test", Protocol: providers.ProtocolOpenAI, BaseURL: "https://example.test/v1/",
				APIKey: "test-key", Model: "test-model", Pricing: &providers.Pricing{},
			},
		),
		fx.Populate(&runtime),
	)
	app.RequireStart()
	t.Cleanup(app.RequireStop)
	if got := runtime.Definitions(); len(got) != 0 {
		t.Fatalf("CoreRegister tools = %#v, want empty", got)
	}
}

func resolveRegisteredToolNames(t *testing.T, register fx.Option) []string {
	t.Helper()
	var runtime ToolRuntime
	app := fxtest.New(
		t,
		register,
		fx.Provide(newToolRuntime),
		fx.Supply(WorkDir(t.TempDir())),
		fx.Populate(&runtime),
	)
	app.RequireStart()
	t.Cleanup(app.RequireStop)
	definitions := runtime.Definitions()
	names := make([]string, len(definitions))
	for index, definition := range definitions {
		names[index] = definition.Name
	}
	return names
}
