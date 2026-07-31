package engine

import (
	"context"
	"errors"
	"fmt"

	logsdk "github.com/PycMono/go-logger-sdk"
	ctxpkg "github.com/PycMono/go-reagent/internal/context"
	"github.com/PycMono/go-reagent/internal/schema"
)

// Run 启动 Agent 的生命周期。
func (e *AgentEngine) Run(ctx context.Context, userPrompt string, reporter Reporter) error {
	if e == nil || e.provider == nil {
		return errors.New("engine: LLM provider is required")
	}
	if e.registry == nil {
		return errors.New("engine: tool registry is required")
	}
	if ctx == nil {
		return errors.New("engine: context is required")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("Agent 运行已取消: %w", err)
	}

	logsdk.Info(ctx, "[Engine] 引擎启动，锁定工作区",
		logsdk.Any("component", "engine"),
		logsdk.Any("work_dir", e.WorkDir),
	)
	logsdk.Info(ctx, "[Engine] 慢思考模式 (Thinking Phase)",
		logsdk.Any("component", "engine"),
		logsdk.Any("thinking_enabled", e.EnableThinking),
	)

	availableTools := e.registry.GetAvailableTools()
	snapshot, err := e.skillLoader.Discover(ctxpkg.DefaultSkillEnvironment())
	if err != nil {
		return fmt.Errorf("发现 Agent Skills 失败: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("Agent 运行已取消: %w", err)
	}
	logSkillDiagnostics(ctx, snapshot.Diagnostics())
	if len(snapshot.Skills()) > 0 && !hasToolDefinition(availableTools, "read_file") {
		return errors.New("发现可用 Agent Skills，但 Registry 未挂载 read_file")
	}
	systemMessage, promptReport := e.composer.Build(snapshot)
	if promptReport.Truncated {
		logsdk.Warn(ctx, "[Engine] Agent Skill Prompt 已截断",
			logsdk.Any("component", "engine"),
			logsdk.Any("code", "skill_prompt_truncated"),
			logsdk.Any("included_skills", promptReport.IncludedSkills),
			logsdk.Any("omitted_skills", promptReport.OmittedSkills),
			logsdk.Any("shortened_descriptions", promptReport.ShortenedDescriptions),
		)
	}
	contextHistory := []schema.Message{
		systemMessage,
		{
			Role:    schema.RoleUser,
			Content: []schema.ContentBlock{schema.TextBlock(userPrompt)},
		},
	}

	turnCount := 0

	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("Agent 运行已取消: %w", err)
		}

		turnCount++
		logsdk.Info(ctx, fmt.Sprintf("========== [Turn %d] 开始 ==========", turnCount),
			logsdk.Any("component", "engine"),
			logsdk.Any("turn", turnCount),
		)

		// ====================================================================
		// Phase 1: 慢思考阶段 (Thinking) - 剥夺工具，强制规划
		// ====================================================================
		if e.EnableThinking {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("Agent 运行已取消: %w", err)
			}

			logsdk.Info(ctx, "[Engine][Phase 1] 剥夺工具访问权，强制进入慢思考与规划阶段",
				logsdk.Any("component", "engine"),
				logsdk.Any("turn", turnCount),
				logsdk.Any("phase", "thinking"),
			)
			if reporter != nil {
				reporter.OnThinking(ctx)
			}
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

		// ====================================================================
		// Phase 2: 行动阶段 (Action) - 恢复工具，顺着规划执行
		// ====================================================================
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("Agent 运行已取消: %w", err)
		}
		logsdk.Info(ctx, "[Engine][Phase 2] 恢复工具挂载，等待模型采取行动",
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

		actionText, err := schema.TextContent(actionResp.Content)
		if err != nil {
			return fmt.Errorf("Action 阶段生成失败: response content: %w", err)
		}
		if actionText != "" && reporter != nil {
			reporter.OnMessage(ctx, actionText)
		}

		if len(actionResp.ToolCalls) == 0 {
			logsdk.Info(ctx, "[Engine] 模型未请求调用工具，任务宣告完成",
				logsdk.Any("component", "engine"),
				logsdk.Any("turn", turnCount),
			)
			break
		}
		if err := validateToolCalls(actionResp.ToolCalls); err != nil {
			return fmt.Errorf("Action 阶段返回了无效的工具调用: %w", err)
		}

		executionMode := e.actionExecutionMode(actionResp.ToolCalls, availableTools)
		scheduleMessage := fmt.Sprintf("[Engine] 模型请求调用 %d 个工具", len(actionResp.ToolCalls))
		switch executionMode {
		case "parallel":
			scheduleMessage = fmt.Sprintf("[Engine] 模型请求并发调用 %d 个工具", len(actionResp.ToolCalls))
		case "mixed":
			scheduleMessage = fmt.Sprintf("[Engine] 模型请求混合调度 %d 个工具", len(actionResp.ToolCalls))
		}
		logsdk.Info(ctx, scheduleMessage,
			logsdk.Any("component", "engine"),
			logsdk.Any("turn", turnCount),
			logsdk.Any("tool_count", len(actionResp.ToolCalls)),
			logsdk.Any("execution_mode", executionMode),
		)

		observationMsgs, err := e.executeToolCalls(ctx, actionResp.ToolCalls, availableTools, reporter)
		if err != nil {
			return fmt.Errorf("Agent 运行已取消: %w", err)
		}
		aggregationMessage := "[Engine] 所有工具执行完毕，开始聚合观察结果 (Observation)"
		switch executionMode {
		case "parallel":
			aggregationMessage = "[Engine] 所有并发工具执行完毕，开始聚合观察结果 (Observation)"
		case "mixed":
			aggregationMessage = "[Engine] 混合工具执行完毕，开始聚合观察结果 (Observation)"
		}
		logsdk.Info(ctx, aggregationMessage,
			logsdk.Any("component", "engine"),
			logsdk.Any("turn", turnCount),
			logsdk.Any("tool_count", len(actionResp.ToolCalls)),
			logsdk.Any("execution_mode", executionMode),
		)
		contextHistory = append(contextHistory, observationMsgs...)
	}

	return nil
}

// hasToolDefinition 检查当前提供给模型的工具列表中是否存在指定名称的工具。
func hasToolDefinition(definitions []schema.ToolDefinition, name string) bool {
	for _, definition := range definitions {
		if definition.Name == name {
			return true
		}
	}
	return false
}
