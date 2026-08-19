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

// ThinkingEnabled controls whether Loop runs the separate planning phase.
type ThinkingEnabled bool

// CoreRegister provides Agent Core without choosing any concrete tools.
var CoreRegister = fx.Options(
	fx.Provide(
		newPromptComposer,
		newContextBuilder,
		newProvider,
		newFXToolRegistry,
		newExtensionRuntime,
		newFXToolRuntime,
		newScheduler,
		newLoop,
		fx.Annotate(New, fx.As(fx.Self()), fx.As(new(Runner))),
	),
)

// ReadOnlyToolsRegister provides the Workspace-scoped read tool.
var ReadOnlyToolsRegister = fx.Options(
	fx.Provide(
		newToolRoot,
		tools.NewWorkspace,
		fx.Annotate(tools.NewReadTool, fx.As(new(ai.Tool)), fx.ResultTags(`group:"agent_tools"`)),
	),
)

// CodingToolsRegister provides the complete local Coding tool set.
var CodingToolsRegister = fx.Options(
	ReadOnlyToolsRegister,
	fx.Provide(
		tools.NewProcessSupervisor,
		fx.Annotate(tools.NewEditTool, fx.As(new(ai.Tool)), fx.ResultTags(`group:"agent_tools"`)),
		fx.Annotate(tools.NewWriteTool, fx.As(new(ai.Tool)), fx.ResultTags(`group:"agent_tools"`)),
		fx.Annotate(tools.NewApplyPatchTool, fx.As(new(ai.Tool)), fx.ResultTags(`group:"agent_tools"`)),
		fx.Annotate(tools.NewExecTool, fx.As(new(ai.Tool)), fx.ResultTags(`group:"agent_tools"`)),
		fx.Annotate(tools.NewProcessTool, fx.As(new(ai.Tool)), fx.ResultTags(`group:"agent_tools"`)),
	),
)

// Register preserves the complete reusable default Agent graph.
var Register = fx.Options(
	CoreRegister,
	CodingToolsRegister,
	fx.Supply(ThinkingEnabled(true)),
)

type toolRegistryParams struct {
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

func newFXToolRegistry(params toolRegistryParams) (*toolRegistry, error) {
	return newToolRegistry(params.Tools)
}

func newFXToolRuntime(registry *toolRegistry, _ *extensionRuntime) ToolRuntime {
	return newToolRuntimeFromRegistry(registry, DefaultMiddlewareRegistrations())
}

func newScheduler(toolRuntime ToolRuntime) *Scheduler {
	return NewScheduler(toolRuntime, defaultMaxParallelTools)
}

func newLoop(provider ai.Provider, scheduler *Scheduler, enabled ThinkingEnabled) *Loop {
	return NewLoop(provider, scheduler, bool(enabled))
}

func newToolRoot(workDir WorkDir) tools.Root {
	return tools.Root(workDir)
}
