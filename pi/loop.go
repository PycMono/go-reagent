package pi

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	contexttracing "github.com/PycMono/go-context-sdk/tracing"
	logsdk "github.com/PycMono/go-logger-sdk"
	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/harness"
	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
	"github.com/PycMono/go-reagent/pi/harness/observability"
	"github.com/PycMono/go-reagent/pi/toolexec"
)

// Loop owns provider phases, message history, validation, and tool scheduling.
// Loop 实例可并发复用：消息、计数、预算等全部 Run 状态只保存在方法局部和
// 每次运行传入的 request-local Governor 中。
//
// 可观测性走 SDK 全局默认（go-context-sdk StartSpan / go-observability-sdk
// 包级 Metrics）：Runtime 未安装时全部 Noop，Loop 不持有门面或开关。
type Loop struct {
	provider   ai.Provider
	scheduler  *toolexec.Scheduler
	compaction harness.CompactionConfig
	// providerID 与 model 只用于 Metrics Label（与 Ledger Usage.PlatformID
	// 口径一致），不参与任何业务决策；未装配时记录为 unknown。
	providerID string
	model      string
}

// LoopOption 定制 Loop 的可选能力。
type LoopOption func(*Loop)

// WithLoopProviderIdentity 设置 Metrics 的 provider/model Label。
func WithLoopProviderIdentity(providerID, model string) LoopOption {
	return func(l *Loop) {
		l.providerID = providerID
		l.model = model
	}
}

type loopResult struct {
	newMessages []ai.Message
	invocations []ModelInvocation
}

// NewLoop creates the state-machine boundary for Agent execution.
func NewLoop(provider ai.Provider, scheduler *toolexec.Scheduler, options ...LoopOption) *Loop {
	return NewLoopWithCompaction(provider, scheduler, harness.CompactionConfig{}, options...)
}

// NewLoopWithCompaction 与 NewLoop 相同，但显式注入压缩配置；
// 零值配置关闭主动压缩与 L1，reactive 兜底始终启用。
func NewLoopWithCompaction(
	provider ai.Provider,
	scheduler *toolexec.Scheduler,
	compaction harness.CompactionConfig,
	options ...LoopOption,
) *Loop {
	loop := &Loop{
		provider:   provider,
		scheduler:  scheduler,
		compaction: compaction,
	}
	for _, option := range options {
		if option != nil {
			option(loop)
		}
	}
	return loop
}

// invocationObserver 在摘要 Usage 校验后调用：入账并累加预算，
// 返回的 finalizer 在契约判定后固定 Outcome 并记录指标。
type invocationObserver func(usage ai.Usage, requestIndex uint32, finishReason string) (finalize func(error), err error)

// runState 是一次 Run 的全部可变状态。
type runState struct {
	newMessages    []ai.Message
	invocations    []ModelInvocation
	contextHistory []ai.Message
	availableTools ai.ToolDefinitions
	callSequence   uint32
}

func (l *Loop) run(
	ctx context.Context,
	runContext harness.Context,
	listener EventListener,
	governor *runGovernor,
) (loopResult, error) {
	if err := ctx.Err(); err != nil {
		return loopResult{}, fmt.Errorf("agent 运行已取消: %w", err)
	}

	state := &runState{
		newMessages:    make([]ai.Message, 0),
		invocations:    make([]ModelInvocation, 0),
		contextHistory: append([]ai.Message(nil), runContext.Messages...),
	}
	finish := func(err error) (loopResult, error) {
		return loopResult{
			newMessages: append([]ai.Message(nil), state.newMessages...),
			invocations: append([]ModelInvocation(nil), state.invocations...),
		}, err
	}

	state.availableTools = append(ai.ToolDefinitions(nil), runContext.Tools...)
	slices.SortFunc(state.availableTools, func(a, b ai.ToolDefinition) int {
		return cmp.Compare(a.Name, b.Name)
	})

	// 记账顺序：校验 Usage → 立即入账并累加预算 → 契约校验 → 固定
	// Outcome。observeCompaction 返回 finalizer，由调用方在契约判定后调用。
	observeCompaction := invocationObserver(func(usage ai.Usage, requestIndex uint32, finishReason string) (func(error), error) {
		index := l.recordInvocation(ctx, state, ModelInvocationPhaseCompaction, usage, requestIndex, finishReason)
		finalize := func(contractErr error) { l.finalizeInvocation(ctx, state, index, contractErr) }
		return finalize, governor.observe(state.invocations[index])
	})
	// 根运行创建请求序号器，子代理运行复用父 Run 的。
	sequencer, ctx := sequencerFromCtx(ctx)
	compactionRt := newCompactionRuntime(l.compaction, runContext.CurrentInputIndex, sequencer)

	for {
		if err := ctx.Err(); err != nil {
			return finish(fmt.Errorf("agent 运行已取消: %w", err))
		}

		// 防止死循环，退出机制
		if err := governor.checkTurnLimit(); err != nil {
			return finish(err)
		}

		done, err := l.executeTurn(ctx, state, governor, listener, compactionRt, observeCompaction)
		if done || err != nil {
			return finish(err)
		}
	}
}

// recordInvocation 追加一条可信 Invocation并
// 返回其在 state.invocations 中的下标。Outcome 初始为 accepted，由
// finalizeInvocation 在契约校验后固定。
func (l *Loop) recordInvocation(
	ctx context.Context,
	state *runState,
	phase ModelInvocationPhase,
	usage ai.Usage,
	requestIndex uint32,
	finishReason string,
) int {
	if usage.CostQuality == "" {
		usage.CostQuality = ai.CostQualityEstimated
	}
	state.callSequence++
	state.invocations = append(state.invocations, ModelInvocation{
		Sequence:             state.callSequence,
		Phase:                phase,
		Usage:                usage,
		Outcome:              ModelInvocationAccepted,
		ProviderRequestIndex: requestIndex,
		FinishReason:         finishReason,
	})
	return len(state.invocations) - 1
}

// finalizeInvocation 在契约校验后固定 Outcome 并记录 P0 指标：
// invocations/cost/tokens 只在此处各累加一次，acceptance 取最终判定。
func (l *Loop) finalizeInvocation(ctx context.Context, state *runState, index int, contractErr error) {
	invocation := &state.invocations[index]
	acceptance := observability.AcceptanceAccepted
	if contractErr != nil {
		invocation.Outcome = ModelInvocationContractInvalid
		acceptance = observability.AcceptanceContractInvalid
	}
	observability.RecordModelInvocation(ctx,
		labelOrUnknown(l.providerID), labelOrUnknown(l.model),
		observability.GenerationPhase(invocation.Phase), acceptance,
		invocation.Usage.CostUSD, invocation.Usage.CostQuality,
		invocation.Usage.InputTokens, invocation.Usage.OutputTokens,
		invocation.Usage.CacheReadTokens, invocation.Usage.CacheWriteTokens, invocation.Usage.ReasoningTokens)
}

func labelOrUnknown(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}

// maxSubagentCallsPerBatch 是单个工具批次允许执行的子代理调用上限
// （toolexec.Scheduler 对 wave 内每个调用都创建 goroutine，maxParallel 只限并发
// 不限总数）。超出的调用不调度，确定性生成 IsError 结果。
const maxSubagentCallsPerBatch = 8

// planToolBatch 在 Schedule 前做确定性的批次预处理（与 goroutine 调度无关）：
//
//  1. 可见性边界：不在本轮 availableTools 中的调用一律合成 IsError 拒绝——
//     "模型只能调用当轮可见的工具"是 Loop 级通用不变量（主运行为全集无差别，
//     子代理运行即白名单执行边界）。此类拒绝与未注册工具的旧语义一致：
//     静默错误结果，不补发事件（模型幻觉调用不应触发 tool_error 告警）；
//  2. 子代理配额：按原始调用顺序保留前 maxSubagentCallsPerBatch 个子代理调用
//     进入 runnable（普通工具不受限、全部保留原相对顺序），超出的子代理调用
//     在原始下标处预生成 IsError 结果并补发事件（对合法工具的主动限流，
//     SSE 需要可见）。
//
// origin 是 runnable 下标 → 原始下标的映射；silent 是被拒下标中不补发事件的集合。
func (l *Loop) planToolBatch(
	calls []ai.ToolCall,
	availableTools ai.ToolDefinitions,
) (runnable []ai.ToolCall, origin []int, rejected map[int]toolexec.Result, silent map[int]bool) {
	runnable = make([]ai.ToolCall, 0, len(calls))
	origin = make([]int, 0, len(calls))
	subagentSeen := 0
	for index, call := range calls {
		if !availableTools.Has(call.Name) {
			if rejected == nil {
				rejected = make(map[int]toolexec.Result)
				silent = make(map[int]bool)
			}
			rejected[index] = toolexec.Result{
				ToolCallID: call.ID,
				ToolName:   call.Name,
				Content: []ai.ContentBlock{ai.TextBlock(
					fmt.Sprintf("tool %q is not available in this run", call.Name))},
				IsError:   true,
				ErrorCode: pierrors.ErrorCodeToolPermissionDenied,
			}
			silent[index] = true
			continue
		}
		if l.scheduler.IsSubagentTool(call.Name) {
			subagentSeen++
			if subagentSeen > maxSubagentCallsPerBatch {
				if rejected == nil {
					rejected = make(map[int]toolexec.Result)
					silent = make(map[int]bool)
				}
				rejected[index] = toolexec.Result{
					ToolCallID: call.ID,
					ToolName:   call.Name,
					Content: []ai.ContentBlock{ai.TextBlock(
						fmt.Sprintf("单批子代理调用超过上限 %d，请分批委派", maxSubagentCallsPerBatch))},
					IsError:   true,
					ErrorCode: pierrors.ErrorCodeRunLimitExceeded,
				}
				continue
			}
		}
		runnable = append(runnable, call)
		origin = append(origin, index)
	}
	return runnable, origin, rejected, silent
}

// executeTurn 执行一个完整 Turn：可选 Thinking、Action 与该轮 Tool 批次。
// Turn Span 恰好覆盖本轮业务主体（经 contexttracing.WithSpan 管理状态与
// 生命周期）。返回 done=true 表示 Run 结束（成功或终态错误）。
func (l *Loop) executeTurn(
	ctx context.Context,
	state *runState,
	governor *runGovernor,
	listener EventListener,
	rt *compactionRuntime,
	observeCompaction invocationObserver,
) (done bool, err error) {
	governor.startTurn()
	turnCount := governor.getTurns()
	logsdk.Info(ctx, fmt.Sprintf("========== [Turn %d] 开始 ==========", turnCount),
		logsdk.Any("component", "engine"), logsdk.Any("turn", turnCount))

	err = contexttracing.WithSpan(ctx, observability.SpanNameTurn, func(ctx context.Context) error {
		contexttracing.WithKV(ctx,
			contexttracing.KV(observability.AttrTurnIndex, turnCount),
			contexttracing.KV(observability.AttrContextMessageCount, len(state.contextHistory)),
			contexttracing.KV(observability.AttrContextEstimatedToken,
				rt.meter.Estimate(harness.RequestFootprint{Messages: state.contextHistory, Tools: state.availableTools})),
			contexttracing.KV(observability.AttrToolsAvailableCount, len(state.availableTools)),
		)

		if err := ctx.Err(); err != nil {
			done = true
			return fmt.Errorf("agent 运行已取消: %w", err)
		}
		listener.OnEvent(ctx, NewMessageStartEvent())

		compactedHistory, compactErr := l.maybeCompact(ctx, state.contextHistory, state.availableTools, rt, observeCompaction)
		if compactErr != nil {
			done = true
			return fmt.Errorf("action 阶段生成失败: %w", pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "action", compactErr))
		}
		state.contextHistory = compactedHistory
		generated, genErr := l.generateWithSpan(ctx, observability.GenerationPhaseAction, state.contextHistory, state.availableTools, func(block ai.ContentBlock) {
			listener.OnEvent(ctx, NewMessageUpdateEvent(block))
		}, observeCompaction, rt)
		if genErr != nil {
			done = true
			return fmt.Errorf("action 阶段生成失败: %w", pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "action", genErr))
		}
		state.contextHistory = generated.context
		actionResp := generated.message
		if actionResp == nil || actionResp.Usage == nil {
			done = true
			return fmt.Errorf("action 阶段生成失败: %w", pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "action", actionResp.ValidateAction()))
		}

		// 可信 Usage 先于契约校验入账并累加预算。
		actionIndex := l.recordInvocation(ctx, state, ModelInvocationPhaseAction,
			*actionResp.Usage, generated.requestIndex, string(actionResp.FinishReason))
		actionBudgetErr := governor.observe(state.invocations[actionIndex])
		actionContractErr := actionResp.ValidateAction()
		l.finalizeInvocation(ctx, state, actionIndex, actionContractErr)
		if actionContractErr != nil {
			done = true
			return fmt.Errorf("action 阶段生成失败: %w", pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "action", actionContractErr))
		}
		if actionBudgetErr != nil {
			// 预算已达到：无工具的完整 Action 仍是可持久化的业务消息；
			// 带工具的 Action 不能写入 NewMessages，也得不到 message_end。
			if len(actionResp.ToolCalls) == 0 {
				state.contextHistory = append(state.contextHistory, *actionResp)
				state.newMessages = append(state.newMessages, *actionResp)
				listener.OnEvent(ctx, NewMessageEndEvent(*actionResp))
			}
			done = true
			return actionBudgetErr
		}

		state.contextHistory = append(state.contextHistory, *actionResp)
		state.newMessages = append(state.newMessages, *actionResp)
		listener.OnEvent(ctx, NewMessageEndEvent(*actionResp))

		if len(actionResp.ToolCalls) == 0 {
			done = true
			return nil
		}
		if err := actionResp.ToolCalls.Validate(); err != nil {
			done = true
			return fmt.Errorf("action 阶段返回了无效的工具调用: %w", err)
		}

		runnable, origin, rejected, silentRejected := l.planToolBatch(actionResp.ToolCalls, state.availableTools)
		if len(rejected) > 0 {
			contexttracing.WithKV(ctx, contexttracing.KV(observability.AttrToolsRejectedCount, len(rejected)))
		}
		mode := l.scheduler.Mode(runnable, state.availableTools)
		contexttracing.WithKV(ctx,
			contexttracing.KV(observability.AttrToolsRequestedCount, len(actionResp.ToolCalls)),
			contexttracing.KV(observability.AttrToolsExecutionMode, mode),
		)
		logsdk.Info(ctx, "[Engine] 模型请求调用工具",
			logsdk.Any("component", "engine"),
			logsdk.Any("turn", turnCount),
			logsdk.Any("tool_count", len(actionResp.ToolCalls)),
			logsdk.Any("rejected_count", len(rejected)),
			logsdk.Any("execution_mode", mode),
		)
		observer := func(ctx context.Context, event toolexec.Event) {
			listener.OnEvent(ctx, NewAgentToolEvent(event))
		}

		// 每个工具批次创建独立的预算账户与取消源。
		batchCtx, cancel := context.WithCancelCause(ctx)
		defer cancel(nil)
		account := &batchBudget{governor: governor, cancel: cancel}
		recorder := &invocationRecorder{}
		scheduleCtx := withInvocationRecorder(withBatchBudget(batchCtx, account), recorder)

		// 被拒绝的调用不进入调度。批次上限拒绝按原始下标顺序补发完整事件
		// （SSE 事件流完整且顺序确定）；可见性拒绝保持静默（对齐未注册工具
		// 的旧语义，不触发 tool_error 告警）。
		for _, index := range slices.Sorted(maps.Keys(rejected)) {
			if silentRejected[index] {
				continue
			}
			call := actionResp.ToolCalls[index]
			observer(batchCtx, toolexec.NewStartEvent(call))
			observer(batchCtx, toolexec.NewEndEvent(call, rejected[index]))
		}

		scheduled, scheduleErr := l.scheduler.Schedule(scheduleCtx, runnable, state.availableTools, observer)

		// 结算：所有返回路径强制执行。
		// 只追加账本，不再 observe——预算已经 batchBudget 实时扣减。
		for _, report := range recorder.drain() {
			for _, childInv := range report.Invocations {
				index := l.recordInvocation(ctx, state, ModelInvocationPhaseSubagent,
					childInv.Usage, childInv.ProviderRequestIndex, childInv.FinishReason)
				state.invocations[index].Outcome = childInv.Outcome
			}
		}

		// 错误优先级（严格按序判定，不做 errors.Join）：
		// 父 ctx 取消/deadline → 父预算触顶 → Schedule 基础设施错误。
		if err := ctx.Err(); err != nil {
			done = true
			return fmt.Errorf("agent 运行已取消: %w", err)
		}
		if errors.Is(context.Cause(batchCtx), errParentBudgetExhausted) {
			if budgetErr := governor.firstBudgetError(); budgetErr != nil {
				done = true
				return budgetErr
			}
		}
		if scheduleErr != nil {
			if errors.Is(scheduleErr, context.Canceled) || errors.Is(scheduleErr, context.DeadlineExceeded) {
				done = true
				return fmt.Errorf("agent 运行已取消: %w", scheduleErr)
			}
			done = true
			return fmt.Errorf("%w: schedule tools: %w", pierrors.ErrToolRuntime, scheduleErr)
		}

		// 按原始下标合并调度结果与合成拒绝结果。
		results := make([]toolexec.Result, len(actionResp.ToolCalls))
		for index, result := range scheduled {
			results[origin[index]] = result
		}
		for index, result := range rejected {
			results[index] = result
		}

		for _, result := range results {
			rawMessage := ai.Message{
				Role:       ai.RoleTool,
				Content:    append([]ai.ContentBlock(nil), result.Content...),
				ToolCallID: result.ToolCallID,
				ToolName:   result.ToolName,
				IsError:    result.IsError,
			}
			state.contextHistory = append(state.contextHistory, rawMessage)
			state.newMessages = append(state.newMessages, rawMessage)
		}
		return nil

	}, contexttracing.WithErrorClassifier(observability.ClassifyError))
	return done, err
}
