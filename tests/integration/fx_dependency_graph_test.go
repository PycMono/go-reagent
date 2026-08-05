package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/PycMono/go-reagent/agent"
	"github.com/PycMono/go-reagent/ai"
	reagentinternal "github.com/PycMono/go-reagent/internal"
	"github.com/PycMono/go-reagent/internal/config"
	"github.com/PycMono/go-reagent/internal/tools"
	workspacepkg "github.com/PycMono/go-reagent/internal/workspace"
	"go.uber.org/fx"
)

type dependencyGraphProvider struct{}

func (*dependencyGraphProvider) Generate(context.Context, []ai.Message, []ai.ToolDefinition) (*ai.Message, error) {
	return &ai.Message{Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("done")}}, nil
}

func TestRootRegisterPopulatesStructuredRuntimeGraph(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{
		"currentPlatform":"test",
		"platforms":[{"id":"test","protocol":"openai","baseURL":"http://127.0.0.1","apiKey":"test","model":"test"}]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONFIG_PATH", configPath)
	workDir := t.TempDir()
	var (
		runtime    agent.Runner
		registry   agent.Registry
		workspace  *tools.Workspace
		supervisor *tools.ProcessSupervisor
	)
	app := fx.New(
		fx.NopLogger,
		reagentinternal.Register,
		fx.Replace(workspacepkg.WorkDir(workDir), config.Prompt("test")),
		fx.Replace(fx.Annotate(&dependencyGraphProvider{}, fx.As(new(ai.Client)))),
		fx.Populate(&runtime, &registry, &workspace, &supervisor),
	)
	if err := app.Err(); err != nil {
		t.Fatalf("fx.New() error = %v", err)
	}
	if err := app.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() {
		if err := app.Stop(context.Background()); err != nil {
			t.Errorf("Stop() error = %v", err)
		}
	})
	if runtime == nil || registry == nil || workspace == nil || supervisor == nil {
		t.Fatalf("graph = runtime:%T registry:%T workspace:%#v supervisor:%#v", runtime, registry, workspace, supervisor)
	}
	want := []string{"apply_patch", "edit", "exec", "process", "read", "write"}
	definitions := registry.GetAvailableTools()
	if len(definitions) != len(want) {
		t.Fatalf("definitions = %#v", definitions)
	}
	for index, name := range want {
		if definitions[index].Name != name {
			t.Fatalf("definitions[%d].Name = %q, want %q", index, definitions[index].Name, name)
		}
	}
}
