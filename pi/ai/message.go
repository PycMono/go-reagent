package ai

import (
	"errors"
	"fmt"
	"strings"
)

// Role 表示消息在大模型对话中的角色。
type Role string

const (
	RoleSystem    Role = "system"    // RoleSystem 表示系统提示词。
	RoleUser      Role = "user"      // RoleUser 表示用户输入。
	RoleAssistant Role = "assistant" // RoleAssistant 表示模型输出。
	RoleTool      Role = "tool"      // RoleTool 表示工具执行结果。
)

// FinishReason 表示模型结束当前响应的原因。
type FinishReason string

const (
	FinishReasonStop    FinishReason = "stop"
	FinishReasonToolUse FinishReason = "tool_use"
	FinishReasonLength  FinishReason = "length"
)

// Message 表示对话上下文中传递的一条消息。
type Message struct {
	// Role 表示消息角色。
	Role Role `json:"role"`
	// Content 保存消息的内容块。
	Content []ContentBlock `json:"content,omitempty"`
	// Usage 保存生成当前模型消息时产生的用量信息。
	Usage *Usage `json:"usage,omitempty"`
	// FinishReason 保存模型结束当前响应的统一原因。
	FinishReason FinishReason `json:"finish_reason,omitempty"`

	// ToolCalls 保存模型请求执行的工具调用，允许同时包含多个调用。
	ToolCalls ToolCalls `json:"tool_calls,omitempty"`

	// ToolCallID 保存当前工具结果所对应的工具调用 ID。
	ToolCallID string `json:"tool_call_id,omitempty"`
	// ToolName 保存当前工具结果所对应的工具名称。
	ToolName string `json:"tool_name,omitempty"`
	// IsError 表示当前工具结果是否为错误结果。
	IsError bool `json:"is_error,omitempty"`
}

// ValidateThinking 校验一条 Thinking 阶段响应的完整契约：消息必须是纯文本
// 计划，不能携带工具结果标记，不能在未提供工具时返回工具调用，且必须带有
// 可信的计量数据。nil 响应视为空响应。
func (message *Message) ValidateThinking() error {
	if message == nil {
		return errors.New("provider returned an empty response")
	}
	if message.Role != RoleAssistant {
		return fmt.Errorf("response must use assistant role, got %q", message.Role)
	}
	if message.ToolCallID != "" {
		return errors.New("response must not contain tool_call_id")
	}
	if len(message.ToolCalls) != 0 {
		return errors.New("provider returned tool calls while tools were disabled")
	}

	content, err := TextContent(message.Content)
	if err != nil {
		return fmt.Errorf("response content: %w", err)
	}

	if strings.TrimSpace(content) == "" {
		return errors.New("response must contain a non-empty textual plan")
	}

	return message.Usage.ValidateMetered()
}

// ValidateAction 校验一条 Action 阶段响应的完整契约：必须包含正文或工具
// 调用，被长度截断的响应不允许执行其中的工具调用，且必须带有可信的计量
// 数据。nil 响应视为空响应。
func (message *Message) ValidateAction() error {
	if message == nil {
		return errors.New("provider returned an empty response")
	}
	if message.Role != RoleAssistant {
		return fmt.Errorf("response must use assistant role, got %q", message.Role)
	}
	if message.ToolCallID != "" {
		return errors.New("response must not contain tool_call_id")
	}
	content, err := TextContent(message.Content)
	if err != nil {
		return fmt.Errorf("response content: %w", err)
	}
	if content == "" && len(message.ToolCalls) == 0 {
		return errors.New("assistant message contains no content or tool calls")
	}
	if message.FinishReason == FinishReasonLength && len(message.ToolCalls) != 0 {
		return errors.New("truncated response must not execute tool calls")
	}

	return message.Usage.ValidateMetered()
}
