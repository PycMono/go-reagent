package pi

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	logsdk "github.com/PycMono/go-logger-sdk"
	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/harness"
	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
)

// Loop owns provider phases, message history, validation, and tool scheduling for one run.
type Loop struct {
	provider       ai.Provider
	scheduler      *Scheduler
	enableThinking bool
}

type loopResult struct {
	newMessages []ai.Message
	invocations []ModelInvocation
}

// NewLoop creates the state-machine boundary for Agent execution.
func NewLoop(provider ai.Provider, scheduler *Scheduler, enableThinking bool) *Loop {
	return &Loop{provider: provider, scheduler: scheduler, enableThinking: enableThinking}
}

// Run executes provider turns until an Action response has no tool calls.
func (l *Loop) Run(ctx context.Context, runContext harness.Context, reporter Reporter) ([]ai.Message, error) {
	result, err := l.runDetailed(ctx, runContext, reporter)
	return result.newMessages, err
}

func (l *Loop) runDetailed(ctx context.Context, runContext harness.Context, reporter Reporter) (loopResult, error) {
	if err := ctx.Err(); err != nil {
		return loopResult{}, fmt.Errorf("Agent 运行已取消: %w", err)
	}

	contextHistory := append([]ai.Message(nil), runContext.Messages...)
	newMessages := make([]ai.Message, 0)
	invocations := make([]ModelInvocation, 0)
	finish := func(err error) (loopResult, error) {
		return loopResult{
			newMessages: append([]ai.Message(nil), newMessages...),
			invocations: append([]ModelInvocation(nil), invocations...),
		}, err
	}
	availableTools := append([]ai.ToolDefinition(nil), runContext.Tools...)
	slices.SortFunc(availableTools, func(a, b ai.ToolDefinition) int {
		return cmp.Compare(a.Name, b.Name)
	})
	turnCount := 0
	var callSequence uint32
	for {
		if err := ctx.Err(); err != nil {
			return finish(fmt.Errorf("Agent 运行已取消: %w", err))
		}
		turnCount++
		logsdk.Info(ctx, fmt.Sprintf("========== [Turn %d] 开始 ==========", turnCount),
			logsdk.Any("component", "engine"), logsdk.Any("turn", turnCount))

		if l.enableThinking {
			if reporter != nil {
				reporter.Report(ctx, NewThinkingEvent())
			}
			callSequence++
			thinkResp, err := l.generateWithRetry(ctx, contextHistory, nil)
			if err != nil {
				return finish(fmt.Errorf("Thinking 阶段生成失败: %w", pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "thinking", err)))
			}
			if thinkResp == nil {
				return finish(errors.New("Thinking 阶段生成失败: provider returned an empty response"))
			}
			if err := validateThinkingResponse(thinkResp); err != nil {
				return finish(fmt.Errorf("Thinking 阶段生成失败: %w", err))
			}
			if err := validateMeteredUsage(thinkResp.Usage); err != nil {
				return finish(fmt.Errorf("Thinking 阶段生成失败: %w", pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "model usage", err)))
			}
			invocations = append(invocations, ModelInvocation{
				Sequence: callSequence,
				Phase:    ModelInvocationPhaseThinking,
				Usage:    *thinkResp.Usage,
			})
			thinkingText, err := ai.TextContent(thinkResp.Content)
			if err != nil {
				return finish(fmt.Errorf("Thinking 阶段生成失败: response content: %w", err))
			}
			fmt.Printf("🧠 [内部思考 Trace]: %s\n", thinkingText)
			contextHistory = append(contextHistory, *thinkResp, ai.Message{
				Role:    ai.RoleUser,
				Content: []ai.ContentBlock{ai.TextBlock("请依据上述计划进入 Action。匹配技能时先完整读取对应 SKILL.md。")},
			})
		}

		if err := ctx.Err(); err != nil {
			return finish(fmt.Errorf("Agent 运行已取消: %w", err))
		}
		callSequence++
		actionResp, err := l.generateWithRetry(ctx, contextHistory, availableTools)
		if err != nil {
			return finish(fmt.Errorf("Action 阶段生成失败: %w", pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "action", err)))
		}
		if actionResp == nil {
			return finish(errors.New("Action 阶段生成失败: provider returned an empty response"))
		}
		if err := validateActionResponse(actionResp); err != nil {
			return finish(fmt.Errorf("Action 阶段生成失败: %w", err))
		}
		if err := validateMeteredUsage(actionResp.Usage); err != nil {
			return finish(fmt.Errorf("Action 阶段生成失败: %w", pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "model usage", err)))
		}
		invocations = append(invocations, ModelInvocation{
			Sequence: callSequence,
			Phase:    ModelInvocationPhaseAction,
			Usage:    *actionResp.Usage,
		})
		contextHistory = append(contextHistory, *actionResp)
		newMessages = append(newMessages, *actionResp)
		if len(actionResp.ToolCalls) == 0 {
			if reporter != nil {
				reporter.Report(ctx, NewMessageEvent(*actionResp))
			}
			return finish(nil)
		}
		if err := validateToolCalls(actionResp.ToolCalls); err != nil {
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
			if reporter != nil {
				reporter.Report(ctx, NewAgentToolEvent(event))
			}
		}
		results, err := l.scheduler.Schedule(ctx, actionResp.ToolCalls, availableTools, observer)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return finish(fmt.Errorf("Agent 运行已取消: %w", err))
			}
			return finish(fmt.Errorf("%w: schedule tools: %w", pierrors.ErrToolRuntime, err))
		}
		for _, result := range results {
			message := ai.Message{
				Role:       ai.RoleTool,
				Content:    result.Content,
				ToolCallID: result.ToolCallID,
				ToolName:   result.ToolName,
				IsError:    result.IsError,
			}
			contextHistory = append(contextHistory, message)
			newMessages = append(newMessages, message)
		}
	}
}

func validateThinkingResponse(response *ai.Message) error {
	if response.Role != ai.RoleAssistant {
		return fmt.Errorf("response must use assistant role, got %q", response.Role)
	}
	if response.ToolCallID != "" {
		return errors.New("response must not contain tool_call_id")
	}
	if len(response.ToolCalls) != 0 {
		return errors.New("provider returned tool calls while tools were disabled")
	}
	content, err := ai.TextContent(response.Content)
	if err != nil {
		return fmt.Errorf("response content: %w", err)
	}
	if strings.TrimSpace(content) == "" {
		return errors.New("response must contain a non-empty textual plan")
	}
	return nil
}

func validateActionResponse(response *ai.Message) error {
	if response.Role != ai.RoleAssistant {
		return fmt.Errorf("response must use assistant role, got %q", response.Role)
	}
	if response.ToolCallID != "" {
		return errors.New("response must not contain tool_call_id")
	}
	content, err := ai.TextContent(response.Content)
	if err != nil {
		return fmt.Errorf("response content: %w", err)
	}
	if content == "" && len(response.ToolCalls) == 0 {
		return errors.New("assistant message contains no content or tool calls")
	}

	return nil
}

func validateToolCalls(calls []ai.ToolCall) error {
	seen := make(map[string]struct{}, len(calls))
	for index, call := range calls {
		if call.ID == "" {
			return fmt.Errorf("tool call at index %d has empty ID", index)
		}
		if _, exists := seen[call.ID]; exists {
			return fmt.Errorf("duplicate tool call ID %q", call.ID)
		}
		seen[call.ID] = struct{}{}
	}

	return nil
}

func validateMeteredUsage(usage *ai.Usage) error {
	if usage == nil {
		return errors.New("usage is required")
	}
	if strings.TrimSpace(usage.PlatformID) == "" {
		return errors.New("usage platform ID is required")
	}
	if strings.TrimSpace(usage.Model) == "" {
		return errors.New("usage model is required")
	}
	if usage.InputTokens < 0 || usage.OutputTokens < 0 {
		return errors.New("usage tokens must be non-negative")
	}
	if usage.LatencyMS < 0 {
		return errors.New("usage latency must be non-negative")
	}

	if invalidUsageDecimal(usage.InputPriceUSDPerMillionTokens) ||
		invalidUsageDecimal(usage.OutputPriceUSDPerMillionTokens) ||
		invalidUsageDecimal(usage.CostUSD) {
		return errors.New("usage prices and cost are outside the supported range")
	}

	expectedCost := (float64(usage.InputTokens)*usage.InputPriceUSDPerMillionTokens +
		float64(usage.OutputTokens)*usage.OutputPriceUSDPerMillionTokens) / 1_000_000
	if math.Abs(usage.CostUSD-expectedCost) > 1e-12 {
		return errors.New("usage cost does not match token prices")
	}

	return nil
}

func invalidUsageDecimal(value float64) bool {
	return value < 0 || value >= ai.MaxUsageDecimalExclusive || math.IsNaN(value) || math.IsInf(value, 0)
}
