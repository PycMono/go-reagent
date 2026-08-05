package engine

import (
	"context"
	"slices"
	"testing"

	"github.com/PycMono/go-reagent/agent"
	"github.com/PycMono/go-reagent/ai"
	"github.com/PycMono/go-reagent/internal/config"
	ctxpkg "github.com/PycMono/go-reagent/internal/context"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"
)

type registerProvider struct{}

func (*registerProvider) Generate(context.Context, []ai.Message, []ai.ToolDefinition) (*ai.Message, error) {
	return &ai.Message{Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("done")}}, nil
}

type registerRegistry struct{}

func (*registerRegistry) GetAvailableTools() []ai.ToolDefinition { return nil }

func (*registerRegistry) Execute(context.Context, ai.ToolCall, agent.ToolEventObserver) (agent.ToolResult, error) {
	return agent.ToolResult{}, nil
}

func TestRegisterProvidesAgentRuntimeStack(t *testing.T) {
	var (
		runtime   AgentRuntime
		scheduler *ToolScheduler
		loop      *AgentLoop
	)
	app := fxtest.New(t,
		fx.Provide(func() ai.Client { return &registerProvider{} }),
		fx.Provide(func() agent.Registry { return &registerRegistry{} }),
		fx.Supply(config.WorkDir(t.TempDir())),
		ctxpkg.Register,
		Register,
		fx.Populate(&runtime, &scheduler, &loop),
	)
	app.RequireStart()
	defer app.RequireStop()

	if runtime == nil || scheduler == nil || loop == nil {
		t.Fatalf("runtime stack = runtime:%T scheduler:%#v loop:%#v", runtime, scheduler, loop)
	}
	if scheduler.maxParallel != defaultMaxParallelTools || !loop.enableThinking {
		t.Fatalf("runtime defaults = parallel:%d thinking:%v", scheduler.maxParallel, loop.enableThinking)
	}
}

type namedRegisterReporter struct {
	name string
	got  *[]string
}

func (r *namedRegisterReporter) Report(context.Context, agent.AgentEvent) {
	*r.got = append(*r.got, r.name)
}

func TestRegisterSortsReversedReporterGroupByOrderThenName(t *testing.T) {
	var got []string
	registration := func(name string) agent.ReporterRegistration {
		return agent.ReporterRegistration{Name: name, Order: 50, Reporter: &namedRegisterReporter{name: name, got: &got}}
	}
	var reporter agent.Reporter
	app := fxtest.New(t,
		fx.Provide(func() ai.Client { return &registerProvider{} }),
		fx.Provide(func() agent.Registry { return &registerRegistry{} }),
		fx.Supply(config.WorkDir(t.TempDir())),
		ctxpkg.Register,
		Register,
		fx.Provide(
			fx.Annotate(func() agent.ReporterRegistration { return registration("zeta") }, fx.ResultTags(`group:"reporters"`)),
			fx.Annotate(func() agent.ReporterRegistration { return registration("alpha") }, fx.ResultTags(`group:"reporters"`)),
		),
		fx.Populate(&reporter),
	)
	app.RequireStart()
	defer app.RequireStop()

	reporter.Report(context.Background(), agent.NewThinkingEvent())
	if !slices.Equal(got, []string{"alpha", "zeta"}) {
		t.Fatalf("reporter sequence = %v, want alpha,zeta", got)
	}
}
