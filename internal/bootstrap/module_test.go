package bootstrap

import (
	"testing"

	"github.com/PycMono/go-reagent/agent"
	"github.com/PycMono/go-reagent/ai"
	"github.com/PycMono/go-reagent/internal/tools"
	"github.com/PycMono/go-reagent/internal/workspace"
	"go.uber.org/fx"
)

func TestModuleBuildsOneDefaultRuntimeGraph(t *testing.T) {
	var (
		runtime    *agent.Agent
		registry   agent.Registry
		supervisor *tools.ProcessSupervisor
	)
	app := fx.New(
		fx.NopLogger,
		fx.Supply(
			ai.PlatformConfig{ID: "test", Protocol: ai.ProtocolOpenAI, BaseURL: "http://127.0.0.1/v1/", APIKey: "key", Model: "model", Pricing: &ai.PricingConfig{}},
			workspace.WorkDir(t.TempDir()),
		),
		Module,
		fx.Populate(&runtime, &registry, &supervisor),
	)
	if err := app.Err(); err != nil {
		t.Fatalf("fx.New() error = %v", err)
	}
	if runtime == nil || registry == nil || supervisor == nil {
		t.Fatalf("runtime graph = runtime:%#v registry:%#v supervisor:%#v", runtime, registry, supervisor)
	}
	if definitions := registry.GetAvailableTools(); len(definitions) != 6 {
		t.Fatalf("tool definitions = %#v, want 6", definitions)
	}
}
