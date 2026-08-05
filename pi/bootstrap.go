package pi

import (
	"context"
	"errors"
	"os"

	"github.com/PycMono/go-reagent/pi/agent"
	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/ai/providers"
	"github.com/PycMono/go-reagent/pi/observability"
	"github.com/PycMono/go-reagent/pi/tools"
	"go.uber.org/fx"
)

const defaultMaxParallelTools = 4

func cloneConfig(input *Config) *Config {
	if input == nil {
		return nil
	}
	cloned := *input
	cloned.Platforms = append([]PlatformConfig(nil), input.Platforms...)
	for index := range cloned.Platforms {
		if input.Platforms[index].Pricing != nil {
			pricing := *input.Platforms[index].Pricing
			cloned.Platforms[index].Pricing = &pricing
		}
	}
	return &cloned
}

func buildAgent(config *Config) (*fx.App, agent.Runner, error) {
	workDir, err := os.Getwd()
	if err != nil {
		return nil, nil, err
	}
	platform, err := config.Current()
	if err != nil {
		return nil, nil, err
	}

	var runtime agent.Runner
	app := fx.New(
		fx.NopLogger,
		fx.Supply(platform, WorkDir(workDir)),
		Module,
		fx.Populate(&runtime),
	)
	if err := app.Err(); err != nil {
		return nil, nil, err
	}
	if err := app.Start(context.Background()); err != nil {
		stopErr := app.Stop(context.Background())
		return nil, nil, errors.Join(err, stopErr)
	}
	return app, runtime, nil
}

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

func newToolRoot(workDir WorkDir) tools.Root {
	return tools.Root(workDir)
}

// Module provides the complete reusable default Agent graph.
var Module = fx.Options(
	fx.Provide(
		newPromptComposer,
		newSkillLoader,
		fx.Annotate(NewRunContextFactory, fx.As(new(agent.ContextFactory))),
		newToolRoot,
		tools.NewWorkspace,
		tools.NewProcessSupervisor,
		fx.Annotate(tools.NewReadTool, fx.As(new(agent.Tool)), fx.ResultTags(`group:"agent_tools"`)),
		fx.Annotate(tools.NewEditTool, fx.As(new(agent.Tool)), fx.ResultTags(`group:"agent_tools"`)),
		fx.Annotate(tools.NewWriteTool, fx.As(new(agent.Tool)), fx.ResultTags(`group:"agent_tools"`)),
		fx.Annotate(tools.NewApplyPatchTool, fx.As(new(agent.Tool)), fx.ResultTags(`group:"agent_tools"`)),
		fx.Annotate(tools.NewExecTool, fx.As(new(agent.Tool)), fx.ResultTags(`group:"agent_tools"`)),
		fx.Annotate(tools.NewProcessTool, fx.As(new(agent.Tool)), fx.ResultTags(`group:"agent_tools"`)),
		newClient,
		newRegistry,
		newScheduler,
		newLoop,
		fx.Annotate(agent.New, fx.As(fx.Self()), fx.As(new(agent.Runner))),
	),
)
