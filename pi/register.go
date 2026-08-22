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
	tracker, err := observability.NewCostTracker(next, config.ID, config.Model, *config.Pricing)
	if err != nil {
		return nil, err
	}
	// 装饰顺序固定：Loop → TracingProvider → CostTracker → Raw Provider（§5）。
	// TracingProvider 只消费标准化 Usage 和包内 Timing Snapshot；Telemetry
	// 关闭时 Span/Metric 经 SDK 全局 Noop 空转，业务结果不变（OBS-006）。
	return observability.NewTracingProvider(tracker, string(config.Protocol), config.ID, config.Model), nil
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

type loopParams struct {
	fx.In
	Provider  ai.Provider
	Scheduler *Scheduler
	Enabled   ThinkingEnabled
	// Compaction 是可选的压缩配置；未提供时使用零值（主动压缩与 L1 关闭）。
	// 值类型与装配层提供的 harness.CompactionConfig 精确匹配——fx 不做
	// 值/指针隐式转换，类型不一致会让 optional 字段静默落空。
	Compaction harness.CompactionConfig `optional:"true"`
	// Platform 提供 Metrics 的 provider/model Label（与 Ledger 口径一致）。
	Platform providers.Options
}

func newLoop(params loopParams) *Loop {
	return NewLoopWithCompaction(params.Provider, params.Scheduler, bool(params.Enabled), params.Compaction,
		WithLoopProviderIdentity(params.Platform.ID, params.Platform.Model))
}

func newToolRoot(workDir WorkDir) tools.Root {
	return tools.Root(workDir)
}
