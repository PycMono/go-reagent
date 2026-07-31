package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	logsdk "github.com/PycMono/go-logger-sdk"
	ctxpkg "github.com/PycMono/go-reagent/internal/context"
	"github.com/PycMono/go-reagent/internal/provider"
	"github.com/PycMono/go-reagent/internal/schema"
	"github.com/PycMono/go-reagent/internal/tools"
)

const defaultMaxParallelTools = 4

// AgentEngine 是微型 OS 的核心驱动
type AgentEngine struct {
	provider    provider.LLMProvider
	registry    tools.Registry
	composer    *ctxpkg.PromptComposer
	skillLoader *ctxpkg.SkillLoader

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
		composer:         ctxpkg.NewPromptComposer(workDir),
		skillLoader:      ctxpkg.NewSkillLoader(workDir),
		WorkDir:          workDir,
		EnableThinking:   enableThinking,
		MaxParallelTools: defaultMaxParallelTools,
	}
}

// Run 启动 Agent 的生命周期
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
			Content: userPrompt,
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

			fmt.Printf("🧠 [内部思考 Trace]: %s\n", thinkResp.Content)
			contextHistory = append(contextHistory, *thinkResp, schema.Message{
				Role:    schema.RoleUser,
				Content: "请依据上述计划进入 Action。匹配技能时先完整读取对应 SKILL.md。",
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

		if actionResp.Content != "" && reporter != nil {
			reporter.OnMessage(ctx, actionResp.Content)
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

func hasToolDefinition(definitions []schema.ToolDefinition, name string) bool {
	for _, definition := range definitions {
		if definition.Name == name {
			return true
		}
	}
	return false
}

func logSkillDiagnostics(ctx context.Context, diagnostics []ctxpkg.SkillDiagnostic) {
	for _, diagnostic := range diagnostics {
		fields := []logsdk.Fields{
			logsdk.Any("component", "engine"),
			logsdk.Any("code", diagnostic.Code),
			logsdk.Any("path", diagnostic.Path),
			logsdk.Any("severity", diagnostic.Severity),
			logsdk.Any("detail", diagnostic.Message),
		}
		switch diagnostic.Severity {
		case ctxpkg.DiagnosticSeverityError:
			logsdk.Error(ctx, "[Engine] Agent Skill 诊断", fields...)
		case ctxpkg.DiagnosticSeverityWarning:
			logsdk.Warn(ctx, "[Engine] Agent Skill 诊断", fields...)
		default:
			logsdk.Info(ctx, "[Engine] Agent Skill 诊断", fields...)
		}
	}
}

func (e *AgentEngine) actionExecutionMode(
	calls []schema.ToolCall,
	definitions []schema.ToolDefinition,
) string {
	if len(calls) == 0 || e.MaxParallelTools <= 1 {
		return "serial"
	}
	parallelSafe := make(map[string]bool, len(definitions))
	for _, definition := range definitions {
		parallelSafe[definition.Name] = definition.ParallelSafe
	}

	hasParallelWave := false
	hasSerialCall := false
	for start := 0; start < len(calls); {
		if !parallelSafe[calls[start].Name] {
			hasSerialCall = true
			start++
			continue
		}
		end := start + 1
		for end < len(calls) && parallelSafe[calls[end].Name] {
			end++
		}
		if end-start > 1 {
			hasParallelWave = true
		} else {
			hasSerialCall = true
		}
		start = end
	}

	if hasParallelWave && hasSerialCall {
		return "mixed"
	}
	if hasParallelWave {
		return "parallel"
	}
	return "serial"
}

func (e *AgentEngine) executeToolCalls(
	ctx context.Context,
	calls []schema.ToolCall,
	definitions []schema.ToolDefinition,
	reporter Reporter,
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
			observations[start] = e.executeToolCall(ctx, start, calls[start], false, reporter)
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
		if err := e.executeParallelWave(ctx, calls, observations, start, end, reporter); err != nil {
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
	reporter Reporter,
) error {
	limit := e.MaxParallelTools
	if limit <= 0 {
		limit = 1
	}
	if waveSize := end - start; limit > waveSize {
		limit = waveSize
	}
	parallel := limit > 1

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

			observations[index] = e.executeToolCall(ctx, index, call, parallel, reporter)
		}(index, call)
	}
	waitGroup.Wait()
	return ctx.Err()
}

func (e *AgentEngine) executeToolCall(
	ctx context.Context,
	index int,
	call schema.ToolCall,
	parallel bool,
	reporter Reporter,
) schema.Message {
	commonFields := []logsdk.Fields{
		logsdk.Any("component", "engine"),
		logsdk.Any("tool_index", index),
		logsdk.Any("tool", call.Name),
		logsdk.Any("tool_call_id", call.ID),
		logsdk.Any("parallel", parallel),
	}
	mode := "串行"
	if parallel {
		mode = "并行"
	}
	logsdk.Info(ctx,
		fmt.Sprintf("  -> [Go-%d] 🛠️ 触发%s执行", index, mode),
		append(commonFields, logsdk.Any("arguments", call.Arguments))...,
	)
	if reporter != nil {
		reporter.OnToolCall(ctx, call.Name, string(call.Arguments))
	}
	result := e.registry.Execute(ctx, call)
	if reporter != nil {
		reporter.OnToolResult(ctx, call.Name, result.Output, result.IsError)
	}
	if result.IsError {
		logsdk.Error(ctx,
			fmt.Sprintf("  -> [Go-%d] ❌ 工具执行失败", index),
			append(commonFields, logsdk.Any("result", result.Output))...,
		)
	} else {
		logsdk.Info(ctx,
			fmt.Sprintf("  -> [Go-%d] ✅ 工具执行成功", index),
			append(commonFields, logsdk.Any("result_bytes", len(result.Output)))...,
		)
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
