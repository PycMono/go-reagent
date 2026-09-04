package pi

import (
	"context"
	"errors"
	"fmt"
	"time"

	contexttracing "github.com/PycMono/go-context-sdk/tracing"
	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/ai/providers"
	pierrors "github.com/PycMono/go-reagent/pi/errors"
	"github.com/PycMono/go-reagent/pi/extension"
	"github.com/PycMono/go-reagent/pi/governor"
	"github.com/PycMono/go-reagent/pi/harness"
	"github.com/PycMono/go-reagent/pi/harness/observability"
	"github.com/PycMono/go-reagent/pi/harness/sandbox"
	"github.com/PycMono/go-reagent/pi/harness/tools"
	"github.com/PycMono/go-reagent/pi/loopdetect"
	pimcp "github.com/PycMono/go-reagent/pi/mcp"
	"github.com/PycMono/go-reagent/pi/middleware"
	"github.com/PycMono/go-reagent/pi/toolexec"
)

const defaultMaxParallelTools = 4

// Runner 定义无状态 Agent 的单次运行行为。
type Runner interface {
	Run(context.Context, RunRequest, EventListener) (RunResult, error)
}

// Agent 是可复用的无状态运行入口。
type Agent struct {
	builder     *harness.ContextBuilder
	loop        *Loop
	toolRuntime *toolexec.Runtime
	notifiers   []Notifier
	// hooks 与 subagents 仅由 pi.New 装配；直建 Agent 时为零值。
	hooks     []lifecycleHook
	subagents *SubagentBinder
}

// newProvider 构造带装饰链的 ai.Provider：Loop → TracingProvider →
// CostTracker → Raw Provider（OBS-006）。装饰顺序固定，仅 pi.New 装配使用。
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
	// TracingProvider 只消费标准化 Usage 和包内 Timing Snapshot；Telemetry
	// 关闭时 Span/Metric 经 SDK 全局 Noop 空转，业务结果不变（OBS-006）。
	return observability.NewTracingProvider(tracker, string(config.Protocol), config.ID, config.Model), nil
}

// New 内部完成 pi 的全部初始化，调用顺序即启动时序约束：
// Runner → Workspace/Supervisor → 工具 → Registry → 扩展 → Tool Runtime
// → Provider 装饰链 → Loop → Agent → subagent 绑定。
func New(opts Options) (*Agent, error) {
	if opts.WorkDir == "" {
		return nil, errors.New("pi: workdir is required")
	}
	// Runner 由 pi 内部按平台选择沙箱后端，不允许外部注入。
	runner, err := sandbox.NewRunner(opts.WorkDir)
	if err != nil {
		return nil, fmt.Errorf("pi: select sandbox runner: %w", err)
	}

	root := tools.Root(opts.WorkDir)
	workspace, err := tools.NewWorkspace(root)
	if err != nil {
		return nil, err
	}
	// 工具集：read 恒装，write/exec 按档位；应用层追加与 subagent 占位
	// 亦在 Registry 构造时在册（binder 启动期校验）。
	allTools := []ai.Tool{tools.NewReadTool(workspace)}
	var supervisor *tools.ProcessSupervisor
	if opts.AllowExec {
		supervisor = tools.NewProcessSupervisor(workspace, runner)
		allTools = append(allTools, tools.NewExecTool(supervisor), tools.NewProcessTool(supervisor))
	}
	if opts.AllowWrite {
		allTools = append(allTools, tools.NewEditTool(workspace), tools.NewWriteTool(workspace), tools.NewApplyPatchTool(workspace))
	}
	allTools = append(allTools, opts.Tools...)
	if opts.BuiltinSubagent {
		allTools = append(allTools, NewResearchSubagentTool())
	}

	registry, err := toolexec.NewRegistry(allTools)
	if err != nil {
		return nil, err
	}
	extensions, err := pimcp.New(opts.MCPServers, root, runner)
	if err != nil {
		return nil, fmt.Errorf("pi: assemble MCP extensions: %w", err)
	}
	extRuntime, err := extension.NewRuntime(registry, extensions)
	if err != nil {
		return nil, err
	}

	toolRuntime := toolexec.NewRuntime(registry,
		append(middleware.Defaults(), opts.ExtraHandlers...), defaultMaxParallelTools)

	provider, err := newProvider(opts.Platform)
	if err != nil {
		return nil, err
	}
	loop := NewLoop(provider, toolRuntime, opts.Compaction,
		WithLoopProviderIdentity(opts.Platform.ID, opts.Platform.Model),
		WithLoopDetection(opts.LoopDetection))

	composer := harness.NewPromptComposer(opts.WorkDir)
	builder := harness.NewContextBuilder(composer, opts.WorkDir)
	agent := &Agent{builder: builder, loop: loop, toolRuntime: toolRuntime, notifiers: opts.Notifiers}

	// 钩子顺序即启动顺序：Workspace/Supervisor 的清理钩子（OnStop-only）
	// 先注册，扩展注册+freeze 其次，subagent 绑定显式排最后。
	// 钩子顺序即启动顺序（Stop 逆序）：探针 → Workspace → Supervisor → 扩展。
	agent.hooks = []lifecycleHook{
		{onStart: func(ctx context.Context) error {
			switch r := runner.(type) {
			case *sandbox.SeatbeltRunner:
				return sandbox.ProbeSeatbelt(ctx, r)
			case *sandbox.BubblewrapRunner:
				return sandbox.ProbeBubblewrap(ctx, r)
			}
			return nil
		}},
		{onStop: func(context.Context) error { return workspace.Close() }},
	}
	if supervisor != nil {
		agent.hooks = append(agent.hooks, lifecycleHook{onStop: func(context.Context) error { return supervisor.Close() }})
	}
	agent.hooks = append(agent.hooks, lifecycleHook{onStart: extRuntime.Start, onStop: extRuntime.Stop})

	if opts.BuiltinSubagent {
		// binder.Start 依赖 Registry 已冻结（扩展 Start 完成后）。
		agent.subagents = NewSubagentBinder(SubagentBinderParams{
			Registry:      registry,
			Runtime:       extRuntime,
			Tools:         allTools,
			ToolRuntime:   toolRuntime,
			Provider:      provider,
			Compaction:    opts.Compaction,
			LoopDetection: opts.LoopDetection,
			Platform:      opts.Platform,
		})
	}
	return agent, nil
}

// ToolDefinitions 返回 Agent 当前注册的工具定义（诊断用）。
func (a *Agent) ToolDefinitions() []ai.ToolDefinition { return a.toolRuntime.Definitions() }

// Start 按注册顺序执行启动钩子：扩展注册+freeze → subagent 绑定
// （全有或全无）。
func (a *Agent) Start(ctx context.Context) error {
	for _, hook := range a.hooks {
		if hook.onStart != nil {
			if err := hook.onStart(ctx); err != nil {
				return err
			}
		}
	}
	if a.subagents != nil {
		if err := a.subagents.Start(ctx); err != nil {
			return err
		}
	}
	return nil
}

// Stop 逆序执行停机钩子（subagent 绑定无停机钩子）。
func (a *Agent) Stop(ctx context.Context) error {
	var joined error
	for index := len(a.hooks) - 1; index >= 0; index-- {
		if hook := a.hooks[index]; hook.onStop != nil {
			joined = errors.Join(joined, hook.onStop(ctx))
		}
	}
	return joined
}

// Run 校验并执行一次相互隔离的请求。
//
// invoke_agent Span在本函数创建：经过 Chat 服务时是
// conversation.run 的子 Span；直接 SDK 调用时自然成为根 Span。
// Span 状态与生命周期由 WithSpan 管理。
func (a *Agent) Run(ctx context.Context, request RunRequest, listener EventListener) (result RunResult, err error) {
	startedAt := time.Now()
	err = contexttracing.WithSpan(ctx, observability.AgentSpanName(observability.AgentName), func(ctx context.Context) (runErr error) {
		defer func() {
			// 终止原因与 governor.Totals 无论成败都写入。
			reason := string(result.Termination.Reason)
			if reason == "" {
				reason = string(governor.TerminationError)
			}
			fields := []contexttracing.Field{
				contexttracing.OperationName("invoke_agent"),
				contexttracing.KV(observability.AttrGenAIAgentName, observability.AgentName),
				contexttracing.KV(observability.AttrTerminationReason, reason),
				contexttracing.KV(observability.AttrRunTurns, result.Termination.Totals.Turns),
				contexttracing.KV(observability.AttrRunInvocations, int(result.Termination.Totals.Invocations)),
				contexttracing.KV(observability.AttrRunTotalTokens, result.Termination.Totals.TotalTokens),
				contexttracing.KV(observability.AttrRunCostUSD, result.Termination.Totals.CostUSD),
			}
			fields = append(fields, observability.ErrorFields(runErr)...)
			contexttracing.WithKV(ctx, fields...)
			observability.RecordAgentRun(ctx, reason, time.Since(startedAt))
			observability.RecordAgentRunShape(ctx, result.Termination.Totals.Turns, int(result.Termination.Totals.Invocations))
		}()

		fail := func(failErr error) error {
			result.Termination = governor.TerminationFromError(failErr, governor.Totals{})
			return failErr
		}
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		if err := request.Validate(); err != nil {
			return fail(err)
		}

		var runContext harness.Context
		prepErr := contexttracing.WithSpan(ctx, observability.SpanNamePrepareContext, func(prepCtx context.Context) error {
			var err error
			runContext, err = a.prepareRunContext(prepCtx, request)
			if err != nil {
				contexttracing.WithKV(prepCtx, observability.ErrorFields(err)...)
			}
			return err
		}, contexttracing.WithErrorClassifier(observability.ClassifyError))
		if prepErr != nil {
			return fail(prepErr)
		}
		if err := ctx.Err(); err != nil {
			return fail(err)
		}

		gov := governor.New(request.Limits)
		if listener == nil {
			listener = nopListener{}
		}
		if len(a.notifiers) > 0 {
			// 告警监听排在调用方 EventListener 之后：SSE 等实时订阅优先，
			// 通道 panic 由 MultiEventListener/notifySafely 双层兜底。
			listener = NewMultiEventListener([]ListenerRegistration{
				{Name: "caller", Order: 0, Listener: listener},
				{Name: "alerts", Order: 100, Listener: &alertListener{notifiers: a.notifiers}},
			})
		}
		newMessages, invocations, runErr := a.loop.run(ctx, runContext, listener, gov)
		result.NewMessages = newMessages
		result.Invocations = invocations
		result.Termination = gov.Termination(runErr)
		return runErr
	}, contexttracing.WithErrorClassifier(observability.ClassifyError))
	// 在 WithSpan 之外告警：覆盖 prepare 失败等 loop 之前的提前返回路径
	//（fail() 已正确设置 result.Termination）。
	a.notifyTermination(ctx, result.Termination)
	return result, err
}

// notifyTermination 把异常终止翻译为运行告警：错误/超时/预算终止告警，
// 正常完成与用户主动取消不告警。Summary 只含终止原因与累计用量，不含
// 消息正文或错误细节，告警通道可直接转发。
func (a *Agent) notifyTermination(ctx context.Context, termination governor.Termination) {
	if len(a.notifiers) == 0 {
		return
	}
	var notification Notification
	switch termination.Reason {
	case governor.TerminationError, governor.TerminationDeadline, governor.TerminationLoopDetected:
		notification = Notification{
			Kind:    NotificationRunError,
			Summary: fmt.Sprintf("Agent 运行异常终止（%s）", termination.Reason),
		}
	case governor.TerminationMaxTurns, governor.TerminationMaxCost, governor.TerminationMaxTotalTokens:
		notification = Notification{
			Kind: NotificationRunLimit,
			Summary: fmt.Sprintf("Agent 运行触发预算上限（%s）：%d 轮 / %d 次调用 / %d tokens / $%.6f",
				termination.Reason, termination.Totals.Turns, termination.Totals.Invocations,
				termination.Totals.TotalTokens, termination.Totals.CostUSD),
		}
	default:
		return
	}
	for _, notifier := range a.notifiers {
		notifySafely(ctx, notifier, notification)
	}
}

func (a *Agent) prepareRunContext(ctx context.Context, request RunRequest) (harness.Context, error) {
	history := make([]ai.Message, len(request.History))
	for index, message := range request.History {
		converted, err := message.Message2AI()
		if err != nil {
			return harness.Context{}, fmt.Errorf("history message %d: %w", index, err)
		}
		history[index] = converted
	}
	input, err := request.Input.Message2AI()
	if err != nil {
		return harness.Context{}, fmt.Errorf("input: %w", err)
	}

	if input.Role != ai.RoleUser {
		return harness.Context{}, fmt.Errorf("%w: input sender type must be customer", pierrors.ErrRequestInvalid)
	}
	blocks := make([]harness.ContextBlock, len(request.Context))
	for index, block := range request.Context {
		blocks[index] = harness.ContextBlock{Name: block.Name, Content: block.Content, Priority: block.Priority}
	}

	prepared, err := a.builder.Build(ctx, harness.ContextRequest{
		History: history,
		Input:   input,
		Context: blocks,
	}, a.toolRuntime.Definitions())
	if err != nil {
		return harness.Context{}, err
	}

	return prepared, nil
}

// Options 是 pi.New 的全部输入：调用方把需要的东西提前传齐，
// pi 内部完成其余装配。pi 不感知 *config.Config（config → pi 依赖方向
// 已锁定，反向 import 会成环），config 翻译留在调用侧。
type Options struct {
	WorkDir         string
	Platform        providers.Options
	Tools           []ai.Tool             // 应用层追加工具（chat 等）
	BuiltinSubagent bool                  // 挂载内置 research 查证子代理
	MCPServers      []pimcp.ServerOptions // MCP server 声明（纯数据，环境解析/沙箱拉起在 pi/mcp 内完成）
	Notifiers       []Notifier
	ExtraHandlers   []middleware.Handler
	Compaction      harness.CompactionConfig
	LoopDetection   loopdetect.Config
	// 内置 Coding 工具档位：read 恒装；write/exec 按能力开关。
	AllowWrite bool
	AllowExec  bool
}

// WorkDir 是 Agent 的工作区路径(由组合根供数)。
type WorkDir string

// ExtraToolHandlers 是装配层（组合根）追加在默认中间件链之后的扩展
// Handler，如按配置挂载的权限拦截、重试与超时。fx 未提供时为零值，
// 链保持纯默认。
type ExtraToolHandlers []middleware.Handler

// lifecycleHook 是 pi 自有的生命周期钩子：Start 顺序执行，Stop 逆序。
type lifecycleHook struct {
	onStart func(context.Context) error
	onStop  func(context.Context) error
}
