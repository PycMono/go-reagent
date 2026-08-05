package agent

import (
	"errors"
	"fmt"
	"strings"

	"github.com/PycMono/go-reagent/pi/ai"
)

// validateThinkingResponse 确保 Thinking 阶段只返回 Assistant 的非空文本计划，
// 不允许模型在工具被禁用时返回 ToolCall 或 ToolCallID。
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

// validateActionResponse 确保 Action 阶段返回 Assistant 消息，并拒绝只应出现在
// 工具 Observation 上的 ToolCallID；校验失败时 Run 会停止本轮执行。
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

// validateToolCalls 确保同一批工具调用都有非空且唯一的 ID，
// 避免工具执行结果无法准确关联回模型发起的调用。
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
