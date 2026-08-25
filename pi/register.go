package pi

import (
	"context"
	"errors"
	"fmt"

	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/ai/providers"
	"github.com/PycMono/go-reagent/pi/harness"
	"github.com/PycMono/go-reagent/pi/harness/observability"
	"github.com/PycMono/go-reagent/pi/harness/tools"
	"github.com/PycMono/go-reagent/pi/middleware"
	"github.com/PycMono/go-reagent/pi/toolexec"
	"go.uber.org/fx"
)

const defaultMaxParallelTools = 4

// WorkDir is the Agent workspace path supplied to the Fx graph.
type WorkDir string

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
		fx.Annotate(newAgent, fx.As(fx.Self()), fx.As(new(Runner))),
	),
)

type agentParams struct {
	fx.In
	Builder     *harness.ContextBuilder
	Loop        *Loop
	ToolRuntime toolexec.Executor
	Notifiers   []Notifier `group:"agent_notifiers"`
}

// newAgent 聚合 agent_notifiers 组的外部通知通道（企业微信/飞书等）；
// 组为空时 Agent 不包装通知桥接，零开销。
func newAgent(params agentParams) *Agent {
	return New(params.Builder, params.Loop, params.ToolRuntime, params.Notifiers...)
}

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
)

// SubagentRegister 提供内置 research 子代理工具与启动期绑定器：
// 引入即启用，不引入即关闭（零工具、零开销）。
// 工具的 Exa 白名单存在性由 binder 在 freeze 后校验，缺失即启动失败
// （当前部署 Exa 为必需 MCP 服务）。
//
// fx.Invoke 是硬要求：fx.Provide 惰性实例化，binder 若无消费者永远不会
// 执行（不 append OnStart、工具永远未绑定）。
var SubagentRegister = fx.Options(
	fx.Provide(newSubagentTools, newSubagentBinder),
	fx.Invoke(func(*subagentBinder) {}),
)

type subagentToolsOut struct {
	fx.Out
	Tools []ai.Tool `group:"agent_tools,flatten"`
}

// newSubagentTools 产出未绑定的 *SubagentTool 占位（不依赖 Registry，
// 无 fx 环），经 fx.Out 展平进入初始 Registry（freeze 前在册，合法）。
func newSubagentTools() (subagentToolsOut, error) {
	return subagentToolsOut{Tools: []ai.Tool{newResearchSubagentTool()}}, nil
}

type subagentBinderParams struct {
	fx.In
	Lifecycle fx.Lifecycle
	Registry  *toolexec.Registry
	// Runtime 仅表达构造顺序：binder 的 OnStart 必须在 extensionRuntime
	// 注册 MCP 工具并 freeze 之后执行。
	Runtime     *extensionRuntime
	Tools       []ai.Tool `group:"agent_tools"`
	ToolRuntime toolexec.Executor
	Provider    ai.Provider
	Compaction  harness.CompactionConfig `optional:"true"`
	Platform    providers.Options
}

// subagentBinder 在启动期 freeze 后校验定义并原子绑定子管线（全有或全无）。
// 子 toolexec.Scheduler 复用共享 toolexec.Executor：执行边界由 Loop 的可见性校验保证
// （availableTools = 白名单 defs 快照）。
type subagentBinder struct {
	registry    *toolexec.Registry
	toolRuntime toolexec.Executor
	tools       []*SubagentTool
	provider    ai.Provider
	compaction  harness.CompactionConfig
	platform    providers.Options
}

func newSubagentBinder(params subagentBinderParams) *subagentBinder {
	binder := &subagentBinder{
		registry:    params.Registry,
		toolRuntime: params.ToolRuntime,
		provider:    params.Provider,
		compaction:  params.Compaction,
		platform:    params.Platform,
	}
	for _, tool := range params.Tools {
		if subagent, ok := tool.(*SubagentTool); ok {
			binder.tools = append(binder.tools, subagent)
		}
	}
	params.Lifecycle.Append(fx.Hook{OnStart: binder.start})
	return binder
}

// start 在 Registry 冻结后执行：先完成全部定义校验与管线构造，任一失败
// 即启动失败；全部成功才统一 bound.Store（全有或全无）。
func (b *subagentBinder) start(_ context.Context) error {
	if len(b.tools) == 0 {
		return nil
	}
	available := make(map[string]bool)
	for _, definition := range b.registry.Definitions() {
		available[definition.Name] = true
	}

	pipelines := make([]*subagentPipeline, 0, len(b.tools))
	for _, tool := range b.tools {

		// 白名单存在性（含 MCP 工具；此时 Registry 已冻结）。
		defs := make(ai.ToolDefinitions, 0, len(tool.tools))
		for _, name := range tool.tools {
			definition, _, ok := b.registry.Lookup(name)
			if !ok {
				return fmt.Errorf("subagent %q: tool %q is not registered (available: %v)",
					tool.name, name, available)
			}
			defs = append(defs, definition)
		}
		// 占位工具必须已在冻结 Registry 中且正是当前实例。
		_, registered, ok := b.registry.Lookup(subagentToolName(tool.name))
		if !ok || registered != ai.Tool(tool) {
			return fmt.Errorf("subagent %q: placeholder tool %q is missing from the registry",
				tool.name, subagentToolName(tool.name))
		}
		scheduler := toolexec.NewScheduler(b.toolRuntime, defaultMaxParallelTools)
		childLoop := NewLoopWithCompaction(b.provider, scheduler, b.compaction,
			WithLoopProviderIdentity(b.platform.ID, b.platform.Model))
		pipelines = append(pipelines, &subagentPipeline{childLoop: childLoop, childTools: defs})
	}

	for index, tool := range b.tools {
		tool.bound.Store(pipelines[index])
	}
	return nil
}

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
	// 装饰顺序固定：Loop → TracingProvider → CostTracker → Raw Provider。
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

func newFXToolRegistry(params toolRegistryParams) (*toolexec.Registry, error) {
	return toolexec.NewRegistry(params.Tools)
}

// ExtraToolHandlers 是装配层（组合根）追加在默认中间件链之后的扩展
// Handler，如按配置挂载的权限拦截、重试与超时。fx 未提供时为零值，
// 链保持纯默认。
type ExtraToolHandlers []middleware.Handler

type toolRuntimeParams struct {
	fx.In
	Registry *toolexec.Registry
	// Ext 仅用于 fx 构造顺序约束：MCP 工具注册并 freeze 之后才建 Runtime。
	Ext   *extensionRuntime
	Extra ExtraToolHandlers `optional:"true"`
}

func newFXToolRuntime(params toolRuntimeParams) toolexec.Executor {
	handlers := append(middleware.Defaults(), params.Extra...)
	return toolexec.NewExecutorFromRegistry(params.Registry, handlers)
}

func newScheduler(toolRuntime toolexec.Executor) *toolexec.Scheduler {
	return toolexec.NewScheduler(toolRuntime, defaultMaxParallelTools)
}

type loopParams struct {
	fx.In
	Provider  ai.Provider
	Scheduler *toolexec.Scheduler
	// Compaction 是可选的压缩配置；未提供时使用零值（主动压缩与 L1 关闭）。
	// 值类型与装配层提供的 harness.CompactionConfig 精确匹配——fx 不做
	// 值/指针隐式转换，类型不一致会让 optional 字段静默落空。
	Compaction harness.CompactionConfig `optional:"true"`
	// Platform 提供 Metrics 的 provider/model Label（与 Ledger 口径一致）。
	Platform providers.Options
}

func newLoop(params loopParams) *Loop {
	return NewLoopWithCompaction(params.Provider, params.Scheduler, params.Compaction,
		WithLoopProviderIdentity(params.Platform.ID, params.Platform.Model))
}

func newToolRoot(workDir WorkDir) tools.Root {
	return tools.Root(workDir)
}
