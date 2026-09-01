package pi

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

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
// 可观测性走 SDK 全局默认（go-context-sdk StartSpan / go-observability-sdk
// 包级 Metrics）：Runtime 未安装时全部 Noop，Loop 不持有门面或开关。
type Loop struct {
	provider   ai.Provider
	scheduler  *toolexec.Scheduler
	compaction harness.CompactionConfig
	// loopDetection 是不可变的循环检测配置；每次 Run 用它创建独立的
	// request-local Detector。
	loopDetection loopdetect.Config
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

// WithLoopDetection 设置工具循环检测配置。不传该 Option 或传入零值 Config
// 都表示默认启用；只有显式 Disabled: true 才恢复无检测的旧行为。
func WithLoopDetection(config loopdetect.Config) LoopOption {
	return func(l *Loop) {
		l.loopDetection = config
	}
}

type loopResult struct {
	newMessages []ai.Message
	invocations []governor.Invocation
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
	invocations    []governor.Invocation
	contextHistory []ai.Message
	availableTools ai.ToolDefinitions
	callSequence   uint32
	// pendingReminder 是循环护栏排入下一次 Action 的 ephemeral 提醒：只进入
	// 下一次逻辑生成的 Provider 请求（含 retry 与 overflow 恢复），不进入
	// contextHistory、newMessages、事件流或压缩摘要；消费一次即清空。
	pendingReminder []ai.Message
}

func (l *Loop) run(
	ctx context.Context,
	runContext harness.Context,
	listener EventListener,
	gov *governor.Governor,
) (loopResult, error) {
	if err := ctx.Err(); err != nil {
		return loopResult{}, fmt.Errorf("agent 运行已取消: %w", err)
	}

	state := &runState{
		newMessages:    make([]ai.Message, 0),
		invocations:    make([]governor.Invocation, 0),
		contextHistory: append([]ai.Message(nil), runContext.Messages...),
	}
	finish := func(err error) (loopResult, error) {
		return loopResult{
			newMessages: append([]ai.Message(nil), state.newMessages...),
			invocations: append([]governor.Invocation(nil), state.invocations...),
		}, err
	}

	state.availableTools = append(ai.ToolDefinitions(nil), runContext.Tools...)
	slices.SortFunc(state.availableTools, func(a, b ai.ToolDefinition) int {
		return cmp.Compare(a.Name, b.Name)
	})

	// 记账顺序：校验 Usage → 立即入账并累加预算 → 契约校验 → 固定
	// Outcome。observeCompaction 返回 finalizer，由调用方在契约判定后调用。
	observeCompaction := invocationObserver(func(usage ai.Usage, requestIndex uint32, finishReason string) (func(error), error) {
		index := l.recordInvocation(state, governor.PhaseCompaction, usage, requestIndex, finishReason)
		finalize := func(contractErr error) { l.finalizeInvocation(ctx, state, index, contractErr) }
		return finalize, gov.Observe(state.invocations[index])
	})
	// 根运行创建请求序号器，子代理运行复用父 Run 的。
	sequencer, ctx := governor.SequencerFromCtx(ctx)
	compactionRt := newCompactionRuntime(l.compaction, runContext.CurrentInputIndex, sequencer)
	// 行为循环检测器：request-local，主代理与每个子代理各自独立。
	detector := loopdetect.New(l.loopDetection)

	for {
		if err := ctx.Err(); err != nil {
			return finish(fmt.Errorf("agent 运行已取消: %w", err))
		}

		if err := gov.CheckTurnLimit(); err != nil { // 防止死循环，退出机制
			return finish(err)
		}
		gov.StartTurn() // 设置循环次数

		done, err := l.executeTurn(ctx, state, gov, detector, listener, compactionRt, observeCompaction)
		if done || err != nil {
			return finish(err)
		}
	}
}

// recordInvocation 追加一条可信 Invocation并
// 返回其在 state.invocations 中的下标。Outcome 初始为 accepted，由
// finalizeInvocation 在契约校验后固定。
func (l *Loop) recordInvocation(
	state *runState,
	phase governor.InvocationPhase,
	usage ai.Usage,
	requestIndex uint32,
	finishReason string,
) int {
	if usage.CostQuality == "" {
		usage.CostQuality = ai.CostQualityEstimated
	}
	state.callSequence++
	state.invocations = append(state.invocations, governor.Invocation{
		Sequence:             state.callSequence,
		Phase:                phase,
		Usage:                usage,
		Outcome:              governor.OutcomeAccepted,
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
		invocation.Outcome = governor.OutcomeContractInvalid
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
		// 消费 pending reminder：经 ephemeral 通道投递，本次逻辑 Action 的
		// 每次物理请求（含 retry 与 overflow 恢复）都能看到；完成后即清空，
		// 不进入历史、事件流或压缩摘要。
		ephemeral := state.pendingReminder
		state.pendingReminder = nil
		generated, genErr := l.generateWithSpan(ctx, observability.GenerationPhaseAction, state.contextHistory, ephemeral, state.availableTools, func(block ai.ContentBlock) {
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
		actionIndex := l.recordInvocation(state, governor.PhaseAction,
			*actionResp.Usage, generated.requestIndex, string(actionResp.FinishReason))
		actionBudgetErr := gov.Observe(state.invocations[actionIndex])
		// Action 契约与 Tool Calls 固有校验（ID 非空不重复、参数为合法 JSON）
		// 一并计入该 Invocation 的 contract_invalid Outcome。
		actionContractErr := actionResp.ValidateAction()
		if actionContractErr == nil {
			actionContractErr = actionResp.ToolCalls.Validate()
		}
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

		if len(actionResp.ToolCalls) == 0 {
			state.contextHistory = append(state.contextHistory, *actionResp)
			state.newMessages = append(state.newMessages, *actionResp)
			listener.OnEvent(ctx, NewMessageEndEvent(*actionResp))
			done = true
			return nil
		}

		// 行为循环准入必须在带工具 Assistant 提交之前：recover/terminate
		// 路径不允许留下没有对应 Tool Results 的残缺消息。
		admission := detector.AdmitToolBatch(actionResp.ToolCalls)
		if admission.Intervention != nil {
			observeLoopDetection(ctx, admission.Intervention)
		}
		switch admission.Decision {
		case loopdetect.DecisionTerminate:
			// 不提交 Assistant、不发送 message_end、不生成 Tool Results、
			// 不执行 Scheduler；provisional delta 由 run.failed 路径丢弃。
			done = true
			return fmt.Errorf("agent 运行因工具循环护栏终止: %w",
				pierrors.Wrap(pierrors.ErrorCodeRunLoopDetected, "tool loop detection",
					loopdetect.NewError(admission.Intervention)))
		case loopdetect.DecisionRecover:
			// 提交完整协议组（原始 Assistant + 每个调用的合成结果），不调用
			// Scheduler、不创建 BatchBudget、不调用 RecordToolBatchOutcome；
			// 模型获得一个恢复 turn，仍受全部预算与取消约束。
			state.contextHistory = append(state.contextHistory, *actionResp)
			state.newMessages = append(state.newMessages, *actionResp)
			listener.OnEvent(ctx, NewMessageEndEvent(*actionResp))
			commitLoopVetoResults(ctx, state, actionResp.ToolCalls, listener)
			return nil
		}
		if admission.Decision == loopdetect.DecisionWarn {
			state.pendingReminder = []ai.Message{loopReminderMessage(admission.Intervention)}
		}

		// allow/warn：提交 Assistant 后执行整批工具。
		state.contextHistory = append(state.contextHistory, *actionResp)
		state.newMessages = append(state.newMessages, *actionResp)
		listener.OnEvent(ctx, NewMessageEndEvent(*actionResp))

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
			observer(batchCtx, toolexec.NewEndEvent(call, rejected[index]))
		}

		scheduled, scheduleErr := l.scheduler.Schedule(scheduleCtx, runnable, state.availableTools, observer)

		// 结算：所有返回路径强制执行。
		// 只追加账本，不再 observe——预算已经 governor.BatchBudget 实时扣减。
		for _, report := range recorder.Drain() {
			for _, childInv := range report.Invocations {
				index := l.recordInvocation(state, governor.PhaseSubagent,
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
		if errors.Is(context.Cause(batchCtx), governor.ErrParentBudgetExhausted) {
			if budgetErr := gov.FirstBudgetError(); budgetErr != nil {
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

		// 结果对齐校验：长度、ToolCallID 或工具名不一致表示 Scheduler/合并
		// 逻辑违反内部不变量——不属于模型行为，也不能降级为“跳过循环记账后
		// 继续”。保留全部已入账 Invocation 与 Totals，合成“执行状态未知”
		// 结果闭合已提交的 Assistant，以 internal error 终止（Termination
		// 为 error 而非 loop_detected）。
		if !toolResultsAligned(actionResp.ToolCalls, results) {
			for _, call := range actionResp.ToolCalls {
				synthetic := toolexec.Result{
					ToolCallID: call.ID,
					ToolName:   call.Name,
					Content: []ai.ContentBlock{ai.TextBlock(
						"工具批次结果对齐失败，执行状态未知，请勿自动重试")},
					IsError:   true,
					ErrorCode: pierrors.ErrorCodeInternal,
				}
				appendToolResultMessage(state, synthetic)
			}
			done = true
			return fmt.Errorf("agent 运行因内部错误终止: %w",
				pierrors.Wrap(pierrors.ErrorCodeInternal, "tool batch outcome reconciliation",
					errors.New("tool results misaligned with requested calls")))
		}

		for _, result := range results {
			appendToolResultMessage(state, result)
		}
		detector.RecordToolBatchOutcome(actionResp.ToolCalls, results)
		return nil

	}, contexttracing.WithErrorClassifier(observability.ClassifyError))
	return done, err
}

// appendToolResultMessage 把一条工具结果按 Tool Calling 协议追加到
// contextHistory 与 newMessages。
func appendToolResultMessage(state *runState, result toolexec.Result) {
	rawMessage := ai.Message{
		Role:       ai.RoleTool,
		Content:    result.Content.Clone(),
		ToolCallID: result.ToolCallID,
		ToolName:   result.ToolName,
		IsError:    result.IsError,
	}
	state.contextHistory = append(state.contextHistory, rawMessage)
	state.newMessages = append(state.newMessages, rawMessage)
}

// toolResultsAligned 校验合并后的结果与原始调用的长度、ToolCallID、工具名
// 一一对齐。
func toolResultsAligned(calls ai.ToolCalls, results []toolexec.Result) bool {
	if len(calls) != len(results) {
		return false
	}
	for index := range calls {
		if calls[index].ID != results[index].ToolCallID || calls[index].Name != results[index].ToolName {
			return false
		}
	}
	return true
}

// commitLoopVetoResults 为被循环护栏 recover 阻止的批次提交完整协议组：
// 每个 Tool Call（含被排除工具——整批没有任何调用启动）按原始顺序补发
// synthetic start/end 事件，并写入与之一一对应的合成结果。
func commitLoopVetoResults(
	ctx context.Context,
	state *runState,
	calls ai.ToolCalls,
	listener EventListener,
) {
	for _, call := range calls {
		result := toolexec.Result{
			ToolCallID: call.ID,
			ToolName:   call.Name,
			Content: []ai.ContentBlock{ai.TextBlock(
				"工具循环护栏阻止了本批次执行：检测到重复且无进展的调用。请停止当前重试路径，改用不同方案，或明确说明无法继续。")},
			IsError:   true,
			ErrorCode: pierrors.ErrorCodeRunLoopDetected,
		}
		listener.OnEvent(ctx, NewAgentToolEvent(toolexec.NewStartEvent(call)))
		listener.OnEvent(ctx, NewAgentToolEvent(toolexec.NewEndEvent(call, result)))
		appendToolResultMessage(state, result)
	}
}

// loopReminderMessage 构造 warn 决策的 ephemeral 提醒：一条 Role=system
// 消息，只包含 Pattern、次数和工具名，不含参数、结果或消息正文。
func loopReminderMessage(intervention *loopdetect.Intervention) ai.Message {
	text := fmt.Sprintf(
		"循环护栏提醒：检测到重复的工具调用（模式 %s，第 %d 次，工具 %s）。"+
			"请检查之前的工具结果是否有变化；如果没有进展，请停止重试、改用其他方法，或明确说明无法继续。",
		intervention.Pattern, intervention.Count, strings.Join(intervention.ToolNames, ", "))
	return ai.Message{Role: ai.RoleSystem, Content: []ai.ContentBlock{ai.TextBlock(text)}}
}

// observeLoopDetection 记录一次护栏干预的无正文结构化观测：日志字段、
// Turn Span 属性与低基数 Counter；禁止记录参数、结果或 hash。
func observeLoopDetection(ctx context.Context, intervention *loopdetect.Intervention) {
	logsdk.Warn(ctx, "[Engine] 工具循环护栏干预",
		logsdk.Any("component", "engine"),
		logsdk.Any("pattern", string(intervention.Pattern)),
		logsdk.Any("level", string(intervention.Level)),
		logsdk.Any("count", intervention.Count),
		logsdk.Any("tool_names", intervention.ToolNames),
	)

	contexttracing.WithKV(ctx,
		contexttracing.KV(observability.AttrLoopDetectionPattern, string(intervention.Pattern)),
		contexttracing.KV(observability.AttrLoopDetectionLevel, string(intervention.Level)),
		contexttracing.KV(observability.AttrLoopDetectionCount, intervention.Count),
	)

	observability.RecordLoopDetectionIntervention(ctx,
		string(intervention.Pattern), string(intervention.Level))
}
