package pi

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"

	logsdk "github.com/PycMono/go-logger-sdk"
	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/harness"
	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
)

// Loop owns provider phases, message history, validation, and tool scheduling.
// Loop 实例可并发复用：消息、计数、预算等全部 Run 状态只保存在方法局部和
// 每次运行传入的 request-local Governor 中。
type Loop struct {
	provider       ai.Provider
	scheduler      *Scheduler
	enableThinking bool
	compaction     harness.CompactionConfig
}

type loopResult struct {
	newMessages []ai.Message
	invocations []ModelInvocation
}

// NewLoop creates the state-machine boundary for Agent execution.
func NewLoop(provider ai.Provider, scheduler *Scheduler, enableThinking bool) *Loop {
	return NewLoopWithCompaction(provider, scheduler, enableThinking, harness.CompactionConfig{})
}

// NewLoopWithCompaction 与 NewLoop 相同，但显式注入压缩配置；
// 零值配置关闭主动压缩与 L1，reactive 兜底始终启用。
func NewLoopWithCompaction(
	provider ai.Provider,
	scheduler *Scheduler,
	enableThinking bool,
	compaction harness.CompactionConfig,
) *Loop {
	return &Loop{provider: provider, scheduler: scheduler, enableThinking: enableThinking, compaction: compaction}
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

	newMessages := make([]ai.Message, 0)
	invocations := make([]ModelInvocation, 0)
	finish := func(err error) (loopResult, error) {
		return loopResult{
			newMessages: append([]ai.Message(nil), newMessages...),
			invocations: append([]ModelInvocation(nil), invocations...),
		}, err
	}

	availableTools := append(ai.ToolDefinitions(nil), runContext.Tools...)
	slices.SortFunc(availableTools, func(a, b ai.ToolDefinition) int {
		return cmp.Compare(a.Name, b.Name)
	})

	contextHistory := append([]ai.Message(nil), runContext.Messages...)
	turnCount := 0
	var callSequence uint32
	recordInvocation := func(phase ModelInvocationPhase, usage ai.Usage) ModelInvocation {
		callSequence++
		invocation := ModelInvocation{
			Sequence: callSequence,
			Phase:    phase,
			Usage:    usage,
		}
		invocations = append(invocations, invocation)
		return invocation
	}

	observeCompaction := func(usage ai.Usage) error {
		return governor.observe(recordInvocation(ModelInvocationPhaseCompaction, usage))
	}
	compactionRt := newCompactionRuntime(l.compaction, runContext.CurrentInputIndex)

	for {
		if err := ctx.Err(); err != nil {
			return finish(fmt.Errorf("Agent 运行已取消: %w", err))
		}

		// 防止死循环，退出机制
		if err := governor.beforeTurn(); err != nil {
			return finish(err)
		}

		governor.startTurn()
		turnCount = governor.getTurns()
		logsdk.Info(ctx, fmt.Sprintf("========== [Turn %d] 开始 ==========", turnCount),
			logsdk.Any("component", "engine"), logsdk.Any("turn", turnCount))

		if l.enableThinking {
			reporter.Report(ctx, NewThinkingEvent())
			compactedHistory, compactErr := l.maybeCompact(ctx, contextHistory, nil, compactionRt, observeCompaction)
			if compactErr != nil {
				return finish(fmt.Errorf("thinking 阶段生成失败: %w", pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "thinking", compactErr)))
			}
			
			contextHistory = compactedHistory
			generated, err := l.generate(ctx, contextHistory, nil, nil, observeCompaction, compactionRt)
			contextHistory = generated.context
			if err != nil {
				return finish(fmt.Errorf("thinking 阶段生成失败: %w", pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "thinking", err)))
			}

			thinkResp := generated.message
			if err = thinkResp.ValidateThinking(); err != nil {
				return finish(fmt.Errorf("Thinking 阶段生成失败: %w", pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "thinking", err)))
			}
			if err = governor.observe(recordInvocation(ModelInvocationPhaseThinking, *thinkResp.Usage)); err != nil {
				return finish(err)
			}

			contextHistory = append(contextHistory, *thinkResp, ai.Message{
				Role:    ai.RoleUser,
				Content: []ai.ContentBlock{ai.TextBlock("请依据上述计划进入 Action。匹配技能时先完整读取对应 SKILL.md。")},
			})
		}

		if err := ctx.Err(); err != nil {
			return finish(fmt.Errorf("Agent 运行已取消: %w", err))
		}
		reporter.Report(ctx, NewMessageStartEvent())

		compactedHistory, compactErr := l.maybeCompact(ctx, contextHistory, availableTools, compactionRt, observeCompaction)
		if compactErr != nil {
			return finish(fmt.Errorf("Action 阶段生成失败: %w", pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "action", compactErr)))
		}
		contextHistory = compactedHistory
		generated, err := l.generate(ctx, contextHistory, availableTools, func(block ai.ContentBlock) {
			reporter.Report(ctx, NewMessageUpdateEvent(block))
		}, observeCompaction, compactionRt)
		if err != nil {
			return finish(fmt.Errorf("Action 阶段生成失败: %w", pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "action", err)))
		}
		contextHistory = generated.context
		actionResp := generated.message
		if err = actionResp.ValidateAction(); err != nil {
			return finish(fmt.Errorf("Action 阶段生成失败: %w", pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "action", err)))
		}

		actionInvocation := recordInvocation(ModelInvocationPhaseAction, *actionResp.Usage)
		if observeErr := governor.observe(actionInvocation); observeErr != nil {
			// 预算已达到：无工具的完整 Action 仍是可持久化的业务消息；
			// 带工具的 Action 不能写入 NewMessages，也得不到 message_end。
			if len(actionResp.ToolCalls) == 0 {
				contextHistory = append(contextHistory, *actionResp)
				newMessages = append(newMessages, *actionResp)
				reporter.Report(ctx, NewMessageEndEvent(*actionResp))
			}
			return finish(observeErr)
		}

		contextHistory = append(contextHistory, *actionResp)
		newMessages = append(newMessages, *actionResp)
		reporter.Report(ctx, NewMessageEndEvent(*actionResp))

		if len(actionResp.ToolCalls) == 0 {
			return finish(nil)
		}
		if err := actionResp.ToolCalls.Validate(); err != nil {
			return finish(fmt.Errorf("Action 阶段返回了无效的工具调用: %w", err))
		}

		mode := l.scheduler.Mode(actionResp.ToolCalls, availableTools)
		logsdk.Info(ctx, "[Engine] 模型请求调用工具",
			logsdk.Any("component", "engine"),
			logsdk.Any("turn", turnCount),
			logsdk.Any("tool_count", len(actionResp.ToolCalls)),
			logsdk.Any("execution_mode", mode),
		)
		observer := func(ctx context.Context, event ToolEvent) {
			reporter.Report(ctx, NewAgentToolEvent(event))
		}

		results, err := l.scheduler.Schedule(ctx, actionResp.ToolCalls, availableTools, observer)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return finish(fmt.Errorf("Agent 运行已取消: %w", err))
			}
			return finish(fmt.Errorf("%w: schedule tools: %w", pierrors.ErrToolRuntime, err))
		}

		for _, result := range results {
			rawMessage := ai.Message{
				Role:       ai.RoleTool,
				Content:    append([]ai.ContentBlock(nil), result.Content...),
				ToolCallID: result.ToolCallID,
				ToolName:   result.ToolName,
				IsError:    result.IsError,
			}
			contextHistory = append(contextHistory, rawMessage)
			newMessages = append(newMessages, rawMessage)
		}
	}
}
