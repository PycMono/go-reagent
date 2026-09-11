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
	pierrors "github.com/PycMono/go-reagent/pi/errors"
	"github.com/PycMono/go-reagent/pi/governor"
	"github.com/PycMono/go-reagent/pi/harness"
	"github.com/PycMono/go-reagent/pi/harness/observability"
	"github.com/PycMono/go-reagent/pi/loopdetect"
	"github.com/PycMono/go-reagent/pi/toolexec"
)

// Loop owns provider phases, message history, validation, and tool scheduling.
// Loop 实例可并发复用：消息、计数、预算等全部 Run 状态只保存在方法局部和
// 每次运行传入的 request-local Governor 中。
//
// Trace 使用 SDK 全局默认 Provider；未安装时 Noop，Loop 不持有观测开关。
type Loop struct {
	provider    ai.Provider
	toolRuntime *toolexec.Runtime
	compaction  harness.CompactionConfig
	// loopDetection 是不可变的循环检测配置；每次 Run 用它创建独立的
	// request-local Detector。
	loopDetection loopdetect.Config
}

// LoopOption 定制 Loop 的可选能力。
type LoopOption func(*Loop)

// NewLoop creates the state-machine boundary for Agent execution.
// 零值配置关闭主动压缩与 L1，reactive 兜底始终启用。
func NewLoop(
	provider ai.Provider,
	toolRuntime *toolexec.Runtime,
	compaction harness.CompactionConfig,
	options ...LoopOption,
) *Loop {
	loop := &Loop{
		provider:    provider,
		toolRuntime: toolRuntime,
		compaction:  compaction,
	}
	for _, option := range options {
		if option != nil {
			option(loop)
		}
	}
	return loop
}

// WithLoopDetection 设置工具循环检测配置。不传该 Option 或传入零值 Config
// 都表示默认启用；只有显式 Disabled: true 才恢复无检测的旧行为。
func WithLoopDetection(config loopdetect.Config) LoopOption {
	return func(l *Loop) {
		l.loopDetection = config
	}
}

// CompactionConfig 返回 Loop 生效的压缩配置（诊断用）。
func (l *Loop) CompactionConfig() harness.CompactionConfig { return l.compaction }

// maxSubagentCallsPerBatch 是单个工具批次允许执行的子代理调用上限
// （toolexec.Runtime 对 wave 内每个调用都创建 goroutine，maxParallel 只限并发
// 不限总数）。超出的调用不调度，确定性生成 IsError 结果。
const maxSubagentCallsPerBatch = 8

// invocationObserver 在摘要 Usage 校验后调用：入账并累加预算，
// 返回的 finalizer 在契约判定后固定 Outcome 。
type invocationObserver func(usage ai.Usage, requestIndex uint32, finishReason string) (finalize func(error), err error)

func (l *Loop) run(
	ctx context.Context,
	runContext harness.Context,
	listener EventListener,
	gov *governor.Governor,
) (newMessages []ai.Message, invocations []governor.Invocation, err error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, fmt.Errorf("agent 运行已取消: %w", err)
	}

	state := &runState{
		contextHistory: append([]ai.Message(nil), runContext.Messages...),
	}
	finish := func(err error) ([]ai.Message, []governor.Invocation, error) {
		return append([]ai.Message(nil), state.newMessages...),
			append([]governor.Invocation(nil), state.invocations...),
			err
	}

	state.availableTools = append(ai.ToolDefinitions(nil), runContext.Tools...)
	slices.SortFunc(state.availableTools, func(a, b ai.ToolDefinition) int {
		return cmp.Compare(a.Name, b.Name)
	})

	// 记账顺序：校验 Usage → 立即入账并累加预算 → 契约校验 → 固定
	// Outcome。observeCompaction 返回 finalizer，由调用方在契约判定后调用。
	observeCompaction := invocationObserver(func(usage ai.Usage, requestIndex uint32, finishReason string) (func(error), error) {
		index := state.addInvocation(governor.PhaseCompaction, usage, requestIndex, finishReason)
		finalize := func(contractErr error) {
			if contractErr != nil {
				state.invocations[index].Outcome = governor.OutcomeContractInvalid
			}
		}
		return finalize, gov.Observe(state.invocations[index])
	})
	// 根运行创建请求序号器，子代理运行复用父 Run 的。
	sequencer, ctx := governor.SequencerFromCtx(ctx)
	compactionRt := newCompactionRuntime(l.compaction, runContext.CurrentInputIndex, sequencer)
	// 行为循环检测器：request-local，主代理与每个子代理各自独立。
	detector := loopdetect.New(l.loopDetection)

	for {
		if err = ctx.Err(); err != nil {
			return finish(fmt.Errorf("agent 运行已取消: %w", err))
		}

		if shouldExit, err := gov.CheckTurnLimit(); shouldExit { // 防止死循环，退出机制
			return finish(err)
		}
		gov.StartTurn() // 设置循环次数

		done, err := l.execute(ctx, state, gov, detector, listener, compactionRt, observeCompaction)
		if done || err != nil {
			return finish(err)
		}
	}
}

// executeTurn 按顺序执行一轮：压缩、生成、记账校验、护栏准入与工具批次。
// Turn Span 恰好覆盖本轮业务主体（经 contexttracing.WithSpan 管理状态与
// 生命周期）。返回 done=true 表示 Run 结束（成功或终态错误）。
func (l *Loop) execute(
	ctx context.Context,
	state *runState,
	gov *governor.Governor,
	detector *loopdetect.Detector,
	listener EventListener,
	rt *compactionRuntime,
	observeCompaction invocationObserver,
) (done bool, err error) {

	turnCount := gov.Turns()
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

		if err = ctx.Err(); err != nil {
			done = true
			return fmt.Errorf("agent 运行已取消: %w", err)
		}

		EmitMessageStart(ctx, listener)

		compactedHistory, compactErr := l.maybeCompact(ctx, state.contextHistory, state.availableTools, rt, observeCompaction)
		if compactErr != nil {
			done = true
			return fmt.Errorf("action 阶段生成失败: %w", pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "action", compactErr))
		}
		state.contextHistory = compactedHistory

		// 消费 pending reminder：经 ephemeral 通道投递，本次逻辑 Action 的
		// 每次物理请求（含 retry 与 overflow 恢复）都能看到；完成后即清空，
		// 不进入历史、事件流或压缩摘要。
		ephemeral := state.pendingReminder
		state.pendingReminder = nil
		generated, genErr := l.generate(ctx, observability.GenerationPhaseAction, state.contextHistory, ephemeral, state.availableTools, func(block ai.ContentBlock) {
			EmitMessageUpdate(ctx, listener, block)
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
		actionIndex := state.addInvocation(governor.PhaseAction,
			*actionResp.Usage, generated.requestIndex, string(actionResp.FinishReason))
		actionBudgetErr := gov.Observe(state.invocations[actionIndex])
		// Action 契约与 Tool Calls 固有校验（ID 非空不重复、参数为合法 JSON）
		// 一并计入该 Invocation 的 contract_invalid Outcome。
		actionContractErr := actionResp.ValidateAction()
		if actionContractErr == nil {
			actionContractErr = actionResp.ToolCalls.Validate()
		}
		if actionContractErr != nil {
			state.invocations[actionIndex].Outcome = governor.OutcomeContractInvalid
			done = true
			return fmt.Errorf("action 阶段生成失败: %w", pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "action", actionContractErr))
		}
		if actionBudgetErr != nil {
			// 预算已达到：无工具的完整 Action 仍是可持久化的业务消息；
			// 带工具的 Action 不能写入 NewMessages，也得不到 message_end。
			if len(actionResp.ToolCalls) == 0 {
				state.appendAssistantMessage(ctx, listener, actionResp)
			}
			done = true
			return actionBudgetErr
		}

		if len(actionResp.ToolCalls) == 0 {
			state.appendAssistantMessage(ctx, listener, actionResp)
			done = true
			return nil
		}

		// 行为循环准入必须在带工具 Assistant 提交之前：recover/terminate
		// 路径不允许留下没有对应 Tool Results 的残缺消息。
		admission := detector.AdmitToolBatch(actionResp.ToolCalls)
		if admission.Intervention != nil {
			recordLoopIntervention(ctx, admission.Intervention)
		}
		switch admission.Decision {
		case loopdetect.DecisionTerminate:
			// 不提交 Assistant、不发送 message_end、不生成 Tool Results、
			// 不执行工具调度；provisional delta 由 run.failed 路径丢弃。
			done = true
			return fmt.Errorf("agent 运行因工具循环护栏终止: %w",
				pierrors.Wrap(pierrors.ErrorCodeRunLoopDetected, "tool loop detection",
					loopdetect.NewError(admission.Intervention)))
		case loopdetect.DecisionRecover:
			// 提交完整协议组（原始 Assistant + 每个调用的合成结果），不调用
			// 不执行工具调度、不创建 BatchBudget、不调用 RecordToolBatchOutcome；
			// 模型获得一个恢复 turn，仍受全部预算与取消约束。
			state.appendAssistantMessage(ctx, listener, actionResp)
			commitLoopRecoveryResults(ctx, state, actionResp.ToolCalls, listener)
			return nil
		}
		if admission.Decision == loopdetect.DecisionWarn {
			state.pendingReminder = []ai.Message{newLoopReminderMessage(admission.Intervention)}
		}

		// allow/warn：提交 Assistant 后执行整批工具。
		state.appendAssistantMessage(ctx, listener, actionResp)
		if err := l.executeToolBatch(ctx, state, gov, actionResp, detector, listener, turnCount); err != nil {
			done = true
			return err
		}
		return nil

	}, contexttracing.WithErrorClassifier(observability.ClassifyError))
	return done, err
}

// executeToolBatch 计划、调度并结算一个工具批次，把结果按原始顺序追加到
// state 并记录批次 Outcome。非 nil 返回值为 Run 终态错误。
func (l *Loop) executeToolBatch(
	ctx context.Context,
	state *runState,
	gov *governor.Governor,
	actionResp *ai.Message,
	detector *loopdetect.Detector,
	listener EventListener,
	turnCount int,
) error {
	runnable, origin, rejected, silentRejected := l.planToolBatch(actionResp.ToolCalls, state.availableTools)
	if len(rejected) > 0 {
		contexttracing.WithKV(ctx, contexttracing.KV(observability.AttrToolsRejectedCount, len(rejected)))
	}
	mode := l.toolRuntime.Mode(runnable, state.availableTools)
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
		EmitToolEvent(ctx, listener, event)
	}

	// 每个工具批次创建独立的预算账户与取消源。
	batchCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	account := governor.NewBatchBudget(gov, cancel)
	recorder := governor.NewInvocationRecorder()
	scheduleCtx := governor.WithInvocationRecorder(governor.WithBatchBudget(batchCtx, account), recorder)

	// 被拒绝的调用不进入调度。批次上限拒绝按原始下标顺序补发完整事件
	// （SSE 事件流完整且顺序确定）；可见性拒绝保持静默（对齐未注册工具
	// 的旧语义，不触发 tool_error 告警）。
	for _, index := range slices.Sorted(maps.Keys(rejected)) {
		if silentRejected[index] {
			continue
		}
		call := actionResp.ToolCalls[index]
		observer(batchCtx, toolexec.NewStartEvent(call))
		observer(batchCtx, rejected[index])
	}

	scheduled, scheduleErr := l.toolRuntime.Schedule(scheduleCtx, runnable, state.availableTools, observer)

	// 结算：所有返回路径强制执行。
	// 只追加账本，不再 observe——预算已经 governor.BatchBudget 实时扣减。
	for _, report := range recorder.Drain() {
		for _, childInv := range report.Invocations {
			index := state.addInvocation(governor.PhaseSubagent,
				childInv.Usage, childInv.ProviderRequestIndex, childInv.FinishReason)
			state.invocations[index].Outcome = childInv.Outcome
		}
	}

	// 错误优先级（严格按序判定，不做 errors.Join）：
	// 父 ctx 取消/deadline → 父预算触顶 → Schedule 基础设施错误。
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("agent 运行已取消: %w", err)
	}
	if errors.Is(context.Cause(batchCtx), governor.ErrParentBudgetExhausted) {
		if budgetErr := gov.FirstBudgetError(); budgetErr != nil {
			return budgetErr
		}
	}
	if scheduleErr != nil {
		if errors.Is(scheduleErr, context.Canceled) || errors.Is(scheduleErr, context.DeadlineExceeded) {
			return fmt.Errorf("agent 运行已取消: %w", scheduleErr)
		}
		return fmt.Errorf("%w: schedule tools: %w", pierrors.ErrToolRuntime, scheduleErr)
	}

	// 按原始下标合并调度结果与合成拒绝结果。
	results := make([]toolexec.Event, len(actionResp.ToolCalls))
	for index, result := range scheduled {
		results[origin[index]] = result
	}
	for index, result := range rejected {
		results[index] = result
	}

	// 结果对齐校验：长度、ToolCallID 或工具名不一致表示 Runtime/合并
	// 逻辑违反内部不变量——不属于模型行为，也不能降级为“跳过循环记账后
	// 继续”。保留全部已入账 Invocation 与 Totals，合成“执行状态未知”
	// 结果闭合已提交的 Assistant，以 internal error 终止（Termination
	// 为 error 而非 loop_detected）。
	if !toolexec.ResultsMatchCalls(actionResp.ToolCalls, results) {
		for _, call := range actionResp.ToolCalls {
			state.appendToolResultMessage(toolexec.NewRejectedEvent(call, pierrors.ErrorCodeInternal,
				"工具批次结果对齐失败，执行状态未知，请勿自动重试"))
		}
		return fmt.Errorf("agent 运行因内部错误终止: %w",
			pierrors.Wrap(pierrors.ErrorCodeInternal, "tool batch outcome reconciliation",
				errors.New("tool results misaligned with requested calls")))
	}

	for _, result := range results {
		state.appendToolResultMessage(result)
	}
	detector.RecordToolBatchOutcome(actionResp.ToolCalls, results)
	return nil
}

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
) (runnable []ai.ToolCall, origin []int, rejected map[int]toolexec.Event, silent map[int]bool) {
	runnable = make([]ai.ToolCall, 0, len(calls))
	origin = make([]int, 0, len(calls))
	subagentSeen := 0
	for index, call := range calls {
		if !availableTools.Has(call.Name) {
			if rejected == nil {
				rejected = make(map[int]toolexec.Event)
				silent = make(map[int]bool)
			}
			rejected[index] = toolexec.NewRejectedEvent(call, pierrors.ErrorCodeToolPermissionDenied,
				fmt.Sprintf("tool %q is not available in this run", call.Name))
			silent[index] = true
			continue
		}
		if l.toolRuntime.IsSubagentTool(call.Name) {
			subagentSeen++
			if subagentSeen > maxSubagentCallsPerBatch {
				if rejected == nil {
					rejected = make(map[int]toolexec.Event)
				}
				rejected[index] = toolexec.NewRejectedEvent(call, pierrors.ErrorCodeRunLimitExceeded,
					fmt.Sprintf("单批子代理调用超过上限 %d，请分批委派", maxSubagentCallsPerBatch))
				continue
			}
		}
		runnable = append(runnable, call)
		origin = append(origin, index)
	}
	return runnable, origin, rejected, silent
}
