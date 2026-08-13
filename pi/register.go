package pi

import (
	"errors"

	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/ai/providers"
	"github.com/PycMono/go-reagent/pi/harness"
	"github.com/PycMono/go-reagent/pi/harness/observability"
	"github.com/PycMono/go-reagent/pi/harness/tools"
	"go.uber.org/fx"
)

const defaultMaxParallelTools = 4

// WorkDir is the Agent workspace path supplied to the Fx graph.
type WorkDir string

// Register provides the complete reusable default Agent graph.
var Register = fx.Options(
	fx.Provide(
		newPromptComposer,
		newContextBuilder,
		newToolRoot,
		tools.NewWorkspace,
		tools.NewProcessSupervisor,
		fx.Annotate(tools.NewReadTool, fx.As(new(ai.Tool)), fx.ResultTags(`group:"agent_tools"`)),
		fx.Annotate(tools.NewEditTool, fx.As(new(ai.Tool)), fx.ResultTags(`group:"agent_tools"`)),
		fx.Annotate(tools.NewWriteTool, fx.As(new(ai.Tool)), fx.ResultTags(`group:"agent_tools"`)),
		fx.Annotate(tools.NewApplyPatchTool, fx.As(new(ai.Tool)), fx.ResultTags(`group:"agent_tools"`)),
		fx.Annotate(tools.NewExecTool, fx.As(new(ai.Tool)), fx.ResultTags(`group:"agent_tools"`)),
		fx.Annotate(tools.NewProcessTool, fx.As(new(ai.Tool)), fx.ResultTags(`group:"agent_tools"`)),
		newProvider,
		newToolRuntime,
		newScheduler,
		newLoop,
		fx.Annotate(New, fx.As(fx.Self()), fx.As(new(Runner))),
	),
)

type toolRuntimeParams struct {
	fx.In
	Tools []ai.Tool `group:"agent_tools"`
}

func newProvider(config providers.Options) (ai.Provider, error) {
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

func newPromptComposer(workDir WorkDir) *harness.PromptComposer {
	return harness.NewPromptComposer(string(workDir))
}

func newContextBuilder(composer *harness.PromptComposer, workDir WorkDir) *harness.ContextBuilder {
	return harness.NewContextBuilder(composer, string(workDir))
}

func newToolRuntime(params toolRuntimeParams) (ToolRuntime, error) {
	return NewToolRuntime(ToolRuntimeOptions{
		Tools:       params.Tools,
		Middlewares: DefaultMiddlewareRegistrations(),
	})
}

func newScheduler(toolRuntime ToolRuntime) *Scheduler {
	return NewScheduler(toolRuntime, defaultMaxParallelTools)
}

func newLoop(provider ai.Provider, scheduler *Scheduler) *Loop {
	return NewLoop(provider, scheduler, true)
}

func newToolRoot(workDir WorkDir) tools.Root {
	return tools.Root(workDir)
}
