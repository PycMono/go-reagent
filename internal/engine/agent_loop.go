package engine

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"

	logsdk "github.com/PycMono/go-logger-sdk"
	ctxpkg "github.com/PycMono/go-reagent/internal/context"
	"github.com/PycMono/go-reagent/internal/provider"
	"github.com/PycMono/go-reagent/internal/schema"
)

// AgentLoop owns provider phases, message history, validation, and tool scheduling for one run.
type AgentLoop struct {
	provider       provider.LLMProvider
	scheduler      *ToolScheduler
	enableThinking bool
}

// NewAgentLoop creates the state-machine boundary for Agent execution.
func NewAgentLoop(p provider.LLMProvider, scheduler *ToolScheduler, enableThinking bool) *AgentLoop {
	return &AgentLoop{provider: p, scheduler: scheduler, enableThinking: enableThinking}
}

// Run executes provider turns until an Action response has no tool calls.
func (l *AgentLoop) Run(ctx context.Context, runContext ctxpkg.RunContext, reporter Reporter) error {
	if l == nil || l.provider == nil {
		return errors.New("agent loop: LLM provider is required")
	}
	if l.scheduler == nil {
		return errors.New("agent loop: tool scheduler is required")
	}
	if ctx == nil {
		return errors.New("agent loop: context is required")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("Agent 运行已取消: %w", err)
	}

	contextHistory := append([]schema.Message(nil), runContext.Messages...)
	availableTools := append([]schema.ToolDefinition(nil), runContext.Tools...)
	slices.SortFunc(availableTools, func(a, b schema.ToolDefinition) int {
		return cmp.Compare(a.Name, b.Name)
	})
	turnCount := 0
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("Agent 运行已取消: %w", err)
		}
		turnCount++
		logsdk.Info(ctx, fmt.Sprintf("========== [Turn %d] 开始 ==========", turnCount),
			logsdk.Any("component", "engine"), logsdk.Any("turn", turnCount))

		if l.enableThinking {
			if reporter != nil {
				reporter.Report(ctx, schema.NewThinkingEvent())
			}
			thinkResp, err := l.provider.Generate(ctx, contextHistory, nil)
			if err != nil {
				return fmt.Errorf("Thinking 阶段生成失败: %w", err)
			}
			if thinkResp == nil {
				return errors.New("Thinking 阶段生成失败: provider returned an empty response")
			}
			if err := validateThinkingResponse(thinkResp); err != nil {
				return fmt.Errorf("Thinking 阶段生成失败: %w", err)
			}
			thinkingText, err := schema.TextContent(thinkResp.Content)
			if err != nil {
				return fmt.Errorf("Thinking 阶段生成失败: response content: %w", err)
			}
			fmt.Printf("🧠 [内部思考 Trace]: %s\n", thinkingText)
			contextHistory = append(contextHistory, *thinkResp, schema.Message{
				Role:    schema.RoleUser,
				Content: []schema.ContentBlock{schema.TextBlock("请依据上述计划进入 Action。匹配技能时先完整读取对应 SKILL.md。")},
			})
		}

		if err := ctx.Err(); err != nil {
			return fmt.Errorf("Agent 运行已取消: %w", err)
		}
		actionResp, err := l.provider.Generate(ctx, contextHistory, availableTools)
		if err != nil {
			return fmt.Errorf("Action 阶段生成失败: %w", err)
		}
		if actionResp == nil {
			return errors.New("Action 阶段生成失败: provider returned an empty response")
		}
		if err := validateActionResponse(actionResp); err != nil {
			return fmt.Errorf("Action 阶段生成失败: %w", err)
		}
		contextHistory = append(contextHistory, *actionResp)
		if len(actionResp.ToolCalls) == 0 {
			if reporter != nil {
				reporter.Report(ctx, schema.NewMessageEvent(*actionResp))
			}
			return nil
		}
		if err := validateToolCalls(actionResp.ToolCalls); err != nil {
			return fmt.Errorf("Action 阶段返回了无效的工具调用: %w", err)
		}

		mode := l.scheduler.Mode(actionResp.ToolCalls, availableTools)
		logsdk.Info(ctx, "[Engine] 模型请求调用工具",
			logsdk.Any("component", "engine"),
			logsdk.Any("turn", turnCount),
			logsdk.Any("tool_count", len(actionResp.ToolCalls)),
			logsdk.Any("execution_mode", mode),
		)
		observer := func(ctx context.Context, event schema.ToolEvent) {
			if reporter != nil {
				reporter.Report(ctx, schema.NewAgentToolEvent(event))
			}
		}
		results, err := l.scheduler.Schedule(ctx, actionResp.ToolCalls, availableTools, observer)
		if err != nil {
			return fmt.Errorf("Agent 运行已取消: %w", err)
		}
		for _, result := range results {
			contextHistory = append(contextHistory, schema.Message{
				Role:       schema.RoleTool,
				Content:    result.Content,
				ToolCallID: result.ToolCallID,
				ToolName:   result.ToolName,
				IsError:    result.IsError,
			})
		}
	}
}
