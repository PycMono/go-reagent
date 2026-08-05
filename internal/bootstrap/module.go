package bootstrap

import (
	"errors"

	"github.com/PycMono/go-reagent/agent"
	"github.com/PycMono/go-reagent/ai"
	"github.com/PycMono/go-reagent/ai/providers"
	"github.com/PycMono/go-reagent/internal/observability"
	"github.com/PycMono/go-reagent/internal/tools"
	"github.com/PycMono/go-reagent/internal/workspace"
	"go.uber.org/fx"
)

const defaultMaxParallelTools = 4

type registryParams struct {
	fx.In

	Tools []agent.Tool `group:"agent_tools"`
}

func newClient(config ai.PlatformConfig) (ai.Client, error) {
	if config.Pricing == nil {
		return nil, errors.New("model pricing is required")
	}
	next, err := providers.New(config)
	if err != nil {
		return nil, err
	}
	return observability.NewCostTracker(next, config.ID, config.Model, observability.Pricing{
		InputUSDPerMillionTokens:  config.Pricing.InputUSDPerMillionTokens,
		OutputUSDPerMillionTokens: config.Pricing.OutputUSDPerMillionTokens,
	})
}

func newRegistry(params registryParams) (agent.Registry, error) {
	return agent.NewRegistry(agent.RegistryOptions{
		Tools:       params.Tools,
		Middlewares: agent.DefaultMiddlewareRegistrations(),
	})
}

func newScheduler(registry agent.Registry) *agent.Scheduler {
	return agent.NewScheduler(registry, defaultMaxParallelTools)
}

func newLoop(client ai.Client, scheduler *agent.Scheduler) *agent.Loop {
	return agent.NewLoop(client, scheduler, true)
}

// Module is the shared private default Agent graph used by the SDK and CLI.
var Module = fx.Options(
	workspace.Module,
	tools.Module,
	fx.Provide(
		newClient,
		newRegistry,
		newScheduler,
		newLoop,
		fx.Annotate(agent.New, fx.As(fx.Self()), fx.As(new(agent.Runner))),
	),
)
