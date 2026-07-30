package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	logsdk "github.com/PycMono/go-logger-sdk"
	"github.com/PycMono/go-reagent/internal/provider"
	"github.com/PycMono/go-reagent/internal/schema"
	"github.com/PycMono/go-reagent/internal/tools"
)

const defaultMaxParallelTools = 4

// AgentEngine 是微型 OS 的核心驱动
type AgentEngine struct {
	provider provider.LLMProvider
	registry tools.Registry

	// WorkDir (工作区): 借鉴 OpenClaw 的理念，Agent 必须有一个明确的物理边界
	WorkDir string

	// EnableThinking 控制是否在每轮 Action 前执行无工具的慢思考阶段。
	EnableThinking bool

	// MaxParallelTools 限制单个并发安全波次中同时执行的工具数量；小于等于 0 时串行执行。
	MaxParallelTools int
}

func NewAgentEngine(
	p provider.LLMProvider,
	r tools.Registry,
	workDir string,
	enableThinking bool,
) *AgentEngine {
	return &AgentEngine{
		provider:         p,
		registry:         r,
		WorkDir:          workDir,
		EnableThinking:   enableThinking,
		MaxParallelTools: defaultMaxParallelTools,
	}
}

// Run 启动 Agent 的生命周期
func (e *AgentEngine) Run(ctx context.Context, userPrompt string) error {
	if e == nil || e.provider == nil {
		return errors.New("engine: LLM provider is required")
	}
	if e.registry == nil {
		return errors.New("engine: tool registry is required")
	}
	if ctx == nil {
		return errors.New("engine: context is required")
	}

	logsdk.Info(ctx, "Agent 引擎启动",
		logsdk.Any("component", "engine"),
		logsdk.Any("work_dir", e.WorkDir),
		logsdk.Any("thinking_enabled", e.EnableThinking),
	)

	contextHistory := []schema.Message{
		{
			Role:    schema.RoleSystem,
			Content: "You are go-reagent, an expert coding assistant. You have full access to tools in the workspace.",
		},
		{
			Role:    schema.RoleUser,
			Content: userPrompt,
		},
	}

	turnCount := 0

	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("Agent 运行已取消: %w", err)
		}

		turnCount++
		logsdk.Info(ctx, "Agent 轮次开始",
			logsdk.Any("component", "engine"),
			logsdk.Any("turn", turnCount),
		)

		// 获取当前挂载的所有工具定义
		availableTools := e.registry.GetAvailableTools()

		// ====================================================================
		// Phase 1: 慢思考阶段 (Thinking) - 剥夺工具，强制规划
		// ====================================================================
		if e.EnableThinking {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("Agent 运行已取消: %w", err)
			}

			logsdk.Info(ctx, "Thinking 阶段开始",
				logsdk.Any("component", "engine"),
				logsdk.Any("turn", turnCount),
				logsdk.Any("phase", "thinking"),
			)
			thinkResp, err := e.provider.Generate(ctx, contextHistory, nil)
			if err != nil {
				return fmt.Errorf("Thinking 阶段生成失败: %w", err)
			}
			if thinkResp == nil {
				return errors.New("Thinking 阶段生成失败: provider returned an empty response")
			}
			if err := validateThinkingResponse(thinkResp); err != nil {
				return fmt.Errorf("Thinking 阶段生成失败: %w", err)
			}

			fmt.Printf("🧠 [内部思考 Trace]: %s\n", thinkResp.Content)
			contextHistory = append(contextHistory, *thinkResp)
		}

		// ====================================================================
		// Phase 2: 行动阶段 (Action) - 恢复工具，顺着规划执行
		// ====================================================================
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("Agent 运行已取消: %w", err)
		}
		logsdk.Info(ctx, "Action 阶段开始",
			logsdk.Any("component", "engine"),
			logsdk.Any("turn", turnCount),
			logsdk.Any("phase", "action"),
		)

		actionResp, err := e.provider.Generate(ctx, contextHistory, availableTools)
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

		if actionResp.Content != "" {
			fmt.Printf("🤖 [对外回复]: %s\n", actionResp.Content)
		}

		if len(actionResp.ToolCalls) == 0 {
			logsdk.Info(ctx, "模型未请求调用工具，任务完成",
				logsdk.Any("component", "engine"),
				logsdk.Any("turn", turnCount),
			)
			break
		}
		if err := validateToolCalls(actionResp.ToolCalls); err != nil {
			return fmt.Errorf("Action 阶段返回了无效的工具调用: %w", err)
		}

		logsdk.Info(ctx, "调度工具调用",
			logsdk.Any("component", "engine"),
			logsdk.Any("turn", turnCount),
			logsdk.Any("tool_count", len(actionResp.ToolCalls)),
		)

		observationMsgs, err := e.executeToolCalls(ctx, actionResp.ToolCalls, availableTools)
		if err != nil {
			return fmt.Errorf("Agent 运行已取消: %w", err)
		}
		contextHistory = append(contextHistory, observationMsgs...)
	}

	return nil
}

func (e *AgentEngine) executeToolCalls(
	ctx context.Context,
	calls []schema.ToolCall,
	definitions []schema.ToolDefinition,
) ([]schema.Message, error) {
	parallelSafe := make(map[string]bool, len(definitions))
	for _, definition := range definitions {
		parallelSafe[definition.Name] = definition.ParallelSafe
	}

	observations := make([]schema.Message, len(calls))
	for start := 0; start < len(calls); {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		if !parallelSafe[calls[start].Name] {
			observations[start] = e.executeToolCall(ctx, start, calls[start])
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			start++
			continue
		}

		end := start + 1
		for end < len(calls) && parallelSafe[calls[end].Name] {
			end++
		}
		if err := e.executeParallelWave(ctx, calls, observations, start, end); err != nil {
			return nil, err
		}
		start = end
	}

	return observations, nil
}

func (e *AgentEngine) executeParallelWave(
	ctx context.Context,
	calls []schema.ToolCall,
	observations []schema.Message,
	start int,
	end int,
) error {
	limit := e.MaxParallelTools
	if limit <= 0 {
		limit = 1
	}
	if waveSize := end - start; limit > waveSize {
		limit = waveSize
	}

	semaphore := make(chan struct{}, limit)
	var waitGroup sync.WaitGroup
	for index := start; index < end; index++ {
		call := calls[index]
		waitGroup.Add(1)
		go func(index int, call schema.ToolCall) {
			defer waitGroup.Done()

			select {
			case semaphore <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-semaphore }()
			if ctx.Err() != nil {
				return
			}

			observations[index] = e.executeToolCall(ctx, index, call)
		}(index, call)
	}
	waitGroup.Wait()
	return ctx.Err()
}

func (e *AgentEngine) executeToolCall(ctx context.Context, index int, call schema.ToolCall) schema.Message {
	commonFields := []logsdk.Fields{
		logsdk.Any("component", "engine"),
		logsdk.Any("tool_index", index),
		logsdk.Any("tool", call.Name),
		logsdk.Any("tool_call_id", call.ID),
	}
	logsdk.Info(ctx, "工具执行开始", append(commonFields, logsdk.Any("arguments", call.Arguments))...)
	result := e.registry.Execute(ctx, call)
	if result.IsError {
		logsdk.Error(ctx, "工具执行失败", append(commonFields, logsdk.Any("result", result.Output))...)
	} else {
		logsdk.Info(ctx, "工具执行成功", append(commonFields, logsdk.Any("result_bytes", len(result.Output)))...)
	}
	return schema.Message{
		Role:       schema.RoleUser,
		Content:    result.Output,
		ToolCallID: call.ID,
	}
}

func validateThinkingResponse(response *schema.Message) error {
	if response.Role != schema.RoleAssistant {
		return fmt.Errorf("response must use assistant role, got %q", response.Role)
	}
	if response.ToolCallID != "" {
		return errors.New("response must not contain tool_call_id")
	}
	if len(response.ToolCalls) != 0 {
		return errors.New("provider returned tool calls while tools were disabled")
	}
	if strings.TrimSpace(response.Content) == "" {
		return errors.New("response must contain a non-empty textual plan")
	}
	return nil
}

func validateActionResponse(response *schema.Message) error {
	if response.Role != schema.RoleAssistant {
		return fmt.Errorf("response must use assistant role, got %q", response.Role)
	}
	if response.ToolCallID != "" {
		return errors.New("response must not contain tool_call_id")
	}
	return nil
}

func validateToolCalls(calls []schema.ToolCall) error {
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
