package ai

// Role 表示消息在大模型对话中的角色。
type Role string

const (
	RoleSystem    Role = "system"    // RoleSystem 表示系统提示词。
	RoleUser      Role = "user"      // RoleUser 表示用户输入。
	RoleAssistant Role = "assistant" // RoleAssistant 表示模型输出。
	RoleTool      Role = "tool"      // RoleTool 表示工具执行结果。
)

// Message 表示对话上下文中传递的一条消息。
type Message struct {
	// Role 表示消息角色。
	Role Role `json:"role"`
	// Content 保存消息的内容块。
	Content []ContentBlock `json:"content,omitempty"`
	// Usage 保存生成当前模型消息时产生的用量信息。
	Usage *Usage `json:"usage,omitempty"`

	// ToolCalls 保存模型请求执行的工具调用，允许同时包含多个调用。
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`

	// ToolCallID 保存当前工具结果所对应的工具调用 ID。
	ToolCallID string `json:"tool_call_id,omitempty"`
	// ToolName 保存当前工具结果所对应的工具名称。
	ToolName string `json:"tool_name,omitempty"`
	// IsError 表示当前工具结果是否为错误结果。
	IsError bool `json:"is_error,omitempty"`
}
