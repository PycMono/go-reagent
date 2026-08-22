package pi

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"

	contexttracing "github.com/PycMono/go-context-sdk/tracing"
	logsdk "github.com/PycMono/go-logger-sdk"
	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/harness"
	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
	"github.com/PycMono/go-reagent/pi/harness/observability"
)

// Loop owns provider phases, message history, validation, and tool scheduling.
// Loop 实例可并发复用：消息、计数、预算等全部 Run 状态只保存在方法局部和
// 每次运行传入的 request-local Governor 中。
//
// 可观测性走 SDK 全局默认（go-context-sdk StartSpan / go-observability-sdk
// 包级 Metrics）：Runtime 未安装时全部 Noop，Loop 不持有门面或开关。
type Loop struct {
	provider       ai.Provider
	scheduler      *Scheduler
	enableThinking bool
	compaction     harness.CompactionConfig
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
func NewLoop(provider ai.Provider, scheduler *Scheduler, enableThinking bool, options ...LoopOption) *Loop {
	return NewLoopWithCompaction(provider, scheduler, enableThinking, harness.CompactionConfig{}, options...)
}

// NewLoopWithCompaction 与 NewLoop 相同，但显式注入压缩配置；
// 零值配置关闭主动压缩与 L1，reactive 兜底始终启用。
func NewLoopWithCompaction(
	provider ai.Provider,
	scheduler *Scheduler,
	enableThinking bool,
	compaction harness.CompactionConfig,
	options ...LoopOption,
) *Loop {
	loop := &Loop{
		provider:       provider,
		scheduler:      scheduler,
		enableThinking: enableThinking,
		compaction:     compaction,
	}
	for _, option := range options {
		if option != nil {
			option(loop)
		}
	}
	return loop
}

// invocationObserver 在摘要 Usage 校验后调用（§9.3）：入账并累加预算，
// 返回的 finalizer 在契约判定后固定 Outcome 并记录指标。
type invocationObserver func(usage ai.Usage, requestIndex uint32, finishReason string) (finalize func(error), err error)

// runState 是一次 Run 的全部可变状态（§2.1 request-local 约束）。
type runState struct {
	newMessages    []ai.Message
	invocations    []ModelInvocation
	contextHistory []ai.Message
	availableTools ai.ToolDefinitions
	callSequence   uint32
}

func (l *Loop) runDetailed(
	ctx context.Context,
	runContext harness.Context,
	reporter Reporter,
	governor *runGovernor,
) (loopResult, error) {
	if err := ctx.Err(); err != nil {
		return loopResult{}, fmt.Errorf("Agent 运行已取消: %w", err)
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

	// §9.3 记账顺序：校验 Usage → 立即入账并累加预算 → 契约校验 → 固定
	// Outcome。observeCompaction 返回 finalizer，由调用方在契约判定后调用。
	observeCompaction := invocationObserver(func(usage ai.Usage, requestIndex uint32, finishReason string) (func(error), error) {
		index := l.recordInvocation(ctx, state, ModelInvocationPhaseCompaction, usage, requestIndex, finishReason)
		finalize := func(contractErr error) { l.finalizeInvocation(ctx, state, index, contractErr) }
		return finalize, governor.observe(state.invocations[index])
	})
	compactionRt := newCompactionRuntime(l.compaction, runContext.CurrentInputIndex)

	for {
		if err := ctx.Err(); err != nil {
			return finish(fmt.Errorf("Agent 运行已取消: %w", err))
		}

		// 防止死循环，退出机制
		if err := governor.beforeTurn(); err != nil {
			return finish(err)
		}

		done, err := l.runTurn(ctx, state, governor, reporter, compactionRt, observeCompaction)
		if done || err != nil {
			return finish(err)
		}
	}
}

// recordInvocation 追加一条可信 Invocation（§9.3：先于契约校验入账）并
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

// finalizeInvocation 在契约校验后固定 Outcome 并记录 P0 指标（§8.2）：
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

// runTurn 执行一个完整 Turn：可选 Thinking、Action 与该轮 Tool 批次
// （§4.3）。Turn Span 恰好覆盖 runTurnIn 的函数作用域（经
// contexttracing.WithSpan 管理状态与生命周期）。返回 done=true 表示 Run 结束
// （成功或终态错误）。
func (l *Loop) runTurn(
	ctx context.Context,
	state *runState,
	governor *runGovernor,
	reporter Reporter,
	rt *compactionRuntime,
	observeCompaction invocationObserver,
) (done bool, err error) {
	governor.startTurn()
	turnCount := governor.getTurns()
	logsdk.Info(ctx, fmt.Sprintf("========== [Turn %d] 开始 ==========", turnCount),
		logsdk.Any("component", "engine"), logsdk.Any("turn", turnCount))

	err = contexttracing.WithSpan(ctx, observability.SpanNameTurn, func(ctx context.Context) error {
		var turnErr error
		done, turnErr = l.runTurnIn(ctx, turnCount, state, governor, reporter, rt, observeCompaction)
		return turnErr
	}, contexttracing.WithErrorClassifier(observability.ClassifyError))
	return done, err
}

// runTurnIn 是 Turn Span 作用域内的业务主体。
func (l *Loop) runTurnIn(
	ctx context.Context,
	turnCount int,
	state *runState,
	governor *runGovernor,
	reporter Reporter,
	rt *compactionRuntime,
	observeCompaction invocationObserver,
) (done bool, err error) {
	contexttracing.WithKV(ctx,
		contexttracing.KV(observability.AttrTurnIndex, turnCount),
		contexttracing.KV(observability.AttrContextMessageCount, len(state.contextHistory)),
		contexttracing.KV(observability.AttrContextEstimatedToken,
			rt.meter.Estimate(harness.RequestFootprint{Messages: state.contextHistory, Tools: state.availableTools})),
		contexttracing.KV(observability.AttrToolsAvailableCount, len(state.availableTools)),
	)

	if l.enableThinking {
		reporter.Report(ctx, NewThinkingEvent())
		compactedHistory, compactErr := l.maybeCompact(ctx, state.contextHistory, nil, rt, observeCompaction)
		if compactErr != nil {
			return true, fmt.Errorf("thinking 阶段生成失败: %w", pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "thinking", compactErr))
		}

		state.contextHistory = compactedHistory
		generated, genErr := l.generateWithSpan(ctx, observability.GenerationPhaseThinking, state.contextHistory, nil, nil, observeCompaction, rt)
		state.contextHistory = generated.context
		if genErr != nil {
			return true, fmt.Errorf("thinking 阶段生成失败: %w", pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "thinking", genErr))
		}

		thinkResp := generated.message
		if thinkResp == nil || thinkResp.Usage == nil {
			return true, fmt.Errorf("Thinking 阶段生成失败: %w", pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "thinking", thinkResp.ValidateThinking()))
		}
		// §9.3：可信 Usage 先于契约校验入账并累加预算；契约与预算错误
		// 并存时返回契约错误，但 Totals 包含本次调用。
		thinkIndex := l.recordInvocation(ctx, state, ModelInvocationPhaseThinking,
			*thinkResp.Usage, generated.requestIndex, string(thinkResp.FinishReason))
		thinkBudgetErr := governor.observe(state.invocations[thinkIndex])
		thinkContractErr := thinkResp.ValidateThinking()
		l.finalizeInvocation(ctx, state, thinkIndex, thinkContractErr)
		if thinkContractErr != nil {
			return true, fmt.Errorf("Thinking 阶段生成失败: %w", pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "thinking", thinkContractErr))
		}
		if thinkBudgetErr != nil {
			return true, thinkBudgetErr
		}

		state.contextHistory = append(state.contextHistory, *thinkResp, ai.Message{
			Role:    ai.RoleUser,
			Content: []ai.ContentBlock{ai.TextBlock("请依据上述计划进入 Action。匹配技能时先完整读取对应 SKILL.md。")},
		})
	}

	if err := ctx.Err(); err != nil {
		return true, fmt.Errorf("Agent 运行已取消: %w", err)
	}
	reporter.Report(ctx, NewMessageStartEvent())

	compactedHistory, compactErr := l.maybeCompact(ctx, state.contextHistory, state.availableTools, rt, observeCompaction)
	if compactErr != nil {
		return true, fmt.Errorf("Action 阶段生成失败: %w", pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "action", compactErr))
	}
	state.contextHistory = compactedHistory
	generated, genErr := l.generateWithSpan(ctx, observability.GenerationPhaseAction, state.contextHistory, state.availableTools, func(block ai.ContentBlock) {
		reporter.Report(ctx, NewMessageUpdateEvent(block))
	}, observeCompaction, rt)
	if genErr != nil {
		return true, fmt.Errorf("Action 阶段生成失败: %w", pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "action", genErr))
	}
	state.contextHistory = generated.context
	actionResp := generated.message
	if actionResp == nil || actionResp.Usage == nil {
		return true, fmt.Errorf("Action 阶段生成失败: %w", pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "action", actionResp.ValidateAction()))
	}

	// §9.3：可信 Usage 先于契约校验入账并累加预算。
	actionIndex := l.recordInvocation(ctx, state, ModelInvocationPhaseAction,
		*actionResp.Usage, generated.requestIndex, string(actionResp.FinishReason))
	actionBudgetErr := governor.observe(state.invocations[actionIndex])
	actionContractErr := actionResp.ValidateAction()
	l.finalizeInvocation(ctx, state, actionIndex, actionContractErr)
	if actionContractErr != nil {
		return true, fmt.Errorf("Action 阶段生成失败: %w", pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "action", actionContractErr))
	}
	if actionBudgetErr != nil {
		// 预算已达到：无工具的完整 Action 仍是可持久化的业务消息；
		// 带工具的 Action 不能写入 NewMessages，也得不到 message_end。
		if len(actionResp.ToolCalls) == 0 {
			state.contextHistory = append(state.contextHistory, *actionResp)
			state.newMessages = append(state.newMessages, *actionResp)
			reporter.Report(ctx, NewMessageEndEvent(*actionResp))
		}
		return true, actionBudgetErr
	}

	state.contextHistory = append(state.contextHistory, *actionResp)
	state.newMessages = append(state.newMessages, *actionResp)
	reporter.Report(ctx, NewMessageEndEvent(*actionResp))

	if len(actionResp.ToolCalls) == 0 {
		return true, nil
	}
	if err := actionResp.ToolCalls.Validate(); err != nil {
		return true, fmt.Errorf("Action 阶段返回了无效的工具调用: %w", err)
	}

	mode := l.scheduler.Mode(actionResp.ToolCalls, state.availableTools)
	contexttracing.WithKV(ctx,
		contexttracing.KV(observability.AttrToolsRequestedCount, len(actionResp.ToolCalls)),
		contexttracing.KV(observability.AttrToolsExecutionMode, mode),
	)
	logsdk.Info(ctx, "[Engine] 模型请求调用工具",
		logsdk.Any("component", "engine"),
		logsdk.Any("turn", turnCount),
		logsdk.Any("tool_count", len(actionResp.ToolCalls)),
		logsdk.Any("execution_mode", mode),
	)
	observer := func(ctx context.Context, event ToolEvent) {
		reporter.Report(ctx, NewAgentToolEvent(event))
	}

	results, scheduleErr := l.scheduler.Schedule(ctx, actionResp.ToolCalls, state.availableTools, observer)
	if scheduleErr != nil {
		if errors.Is(scheduleErr, context.Canceled) || errors.Is(scheduleErr, context.DeadlineExceeded) {
			return true, fmt.Errorf("Agent 运行已取消: %w", scheduleErr)
		}
		return true, fmt.Errorf("%w: schedule tools: %w", pierrors.ErrToolRuntime, scheduleErr)
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
	return false, nil
}
