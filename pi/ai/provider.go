package ai

import "context"

// Provider 定义根据消息上下文和可用工具生成模型响应的能力。
type Provider interface {
	// Generate 根据消息上下文和可用工具生成一次模型响应。
	Generate(context.Context, []Message, []ToolDefinition) (*Message, error)
}
