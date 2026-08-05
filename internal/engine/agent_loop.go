package engine

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"

	logsdk "github.com/PycMono/go-logger-sdk"
	"github.com/PycMono/go-reagent/ai"
	ctxpkg "github.com/PycMono/go-reagent/internal/context"
	"github.com/PycMono/go-reagent/internal/schema"
)

// AgentLoop owns provider phases, message history, validation, and tool scheduling for one run.
type AgentLoop struct {
	provider       ai.Client
	scheduler      *ToolScheduler
	enableThinking bool
}

// NewAgentLoop creates the state-machine boundary for Agent execution.
func NewAgentLoop(p ai.Client, scheduler *ToolScheduler, enableThinking bool) *AgentLoop {
	return &AgentLoop{provider: p, scheduler: scheduler, enableThinking: enableThinking}
}

// Run executes provider turns until an Action response has no tool calls.
func (l *AgentLoop) Run(ctx context.Context, runContext ctxpkg.RunContext, reporter Reporter) ([]ai.Message, error) {
	if l == nil || l.provider == nil {
		return nil, errors.New("agent loop: LLM provider is required")
	}
	if l.scheduler == nil {
		return nil, errors.New("agent loop: tool scheduler is required")
	}
	if ctx == nil {
		return nil, errors.New("agent loop: context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("Agent 运行已取消: %w", err)
	}

	contextHistory := append([]ai.Message(nil), runContext.Messages...)
	newMessages := make([]ai.Message, 0)
	finish := func(err error) ([]ai.Message, error) {
		return append([]ai.Message(nil), newMessages...), err
	}
	availableTools := append([]ai.ToolDefinition(nil), runContext.Tools...)
	slices.SortFunc(availableTools, func(a, b ai.ToolDefinition) int {
		return cmp.Compare(a.Name, b.Name)
	})
	turnCount := 0
	for {
		if err := ctx.Err(); err != nil {
			return finish(fmt.Errorf("Agent 运行已取消: %w", err))
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
				return finish(fmt.Errorf("Thinking 阶段生成失败: %w", err))
			}
			if thinkResp == nil {
				return finish(errors.New("Thinking 阶段生成失败: provider returned an empty response"))
			}
			if err := validateThinkingResponse(thinkResp); err != nil {
				return finish(fmt.Errorf("Thinking 阶段生成失败: %w", err))
			}
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
		actionResp, err := l.provider.Generate(ctx, contextHistory, availableTools)
		if err != nil {
			return finish(fmt.Errorf("Action 阶段生成失败: %w", err))
		}
		if actionResp == nil {
			return finish(errors.New("Action 阶段生成失败: provider returned an empty response"))
		}
		if err := validateActionResponse(actionResp); err != nil {
			return finish(fmt.Errorf("Action 阶段生成失败: %w", err))
		}
		contextHistory = append(contextHistory, *actionResp)
		newMessages = append(newMessages, *actionResp)
		if len(actionResp.ToolCalls) == 0 {
			if reporter != nil {
				reporter.Report(ctx, schema.NewMessageEvent(*actionResp))
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
		observer := func(ctx context.Context, event schema.ToolEvent) {
			if reporter != nil {
				reporter.Report(ctx, schema.NewAgentToolEvent(event))
			}
		}
		results, err := l.scheduler.Schedule(ctx, actionResp.ToolCalls, availableTools, observer)
		if err != nil {
			return finish(fmt.Errorf("Agent 运行已取消: %w", err))
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
