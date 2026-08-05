package pi

import (
	"testing"

	"github.com/PycMono/go-reagent/pi/agent"
	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/observability"
	"github.com/PycMono/go-reagent/pi/tools"
	"go.uber.org/fx"
)

func TestModuleBuildsOneDefaultRuntimeGraph(t *testing.T) {
	var (
		runtime    *agent.Agent
		client     ai.Client
		registry   agent.Registry
		supervisor *tools.ProcessSupervisor
	)
	app := fx.New(
		fx.NopLogger,
		fx.Supply(
			ai.PlatformConfig{ID: "test", Protocol: ai.ProtocolOpenAI, BaseURL: "http://127.0.0.1/v1/", APIKey: "key", Model: "model", Pricing: &ai.PricingConfig{}},
			WorkDir(t.TempDir()),
		),
		Module,
		fx.Populate(&runtime, &client, &registry, &supervisor),
	)
	if err := app.Err(); err != nil {
		t.Fatalf("fx.New() error = %v", err)
	}
	if runtime == nil || registry == nil || supervisor == nil {
		t.Fatalf("runtime graph = runtime:%#v registry:%#v supervisor:%#v", runtime, registry, supervisor)
	}
	if _, ok := client.(*observability.CostTracker); !ok {
		t.Fatalf("client type = %T, want *observability.CostTracker", client)
	}
	if definitions := registry.GetAvailableTools(); len(definitions) != 6 {
		t.Fatalf("tool definitions = %#v, want 6", definitions)
	}
}

func TestModuleRejectsMissingPricingWithoutPanicking(t *testing.T) {
	var client ai.Client
	app := fx.New(
		fx.NopLogger,
		fx.Supply(
			ai.PlatformConfig{ID: "test", Protocol: ai.ProtocolOpenAI, BaseURL: "http://127.0.0.1/v1/", APIKey: "key", Model: "model"},
			WorkDir(t.TempDir()),
		),
		Module,
		fx.Populate(&client),
	)
	if err := app.Err(); err == nil {
		t.Fatal("fx.New() error = nil")
	}
}
