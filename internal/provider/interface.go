package provider

import (
	"context"

	"github.com/PycMono/go-reagent/ai"
)

// LLMProvider 定义了与大模型通信的统一契约
type LLMProvider interface {
	// Generate 接收当前的上下文历史和可用工具列表，返回模型的新消息。
	// availableTools 为空时，代表引擎正在强制模型进入慢思考阶段。
	Generate(ctx context.Context, messages []ai.Message, availableTools []ai.ToolDefinition) (*ai.Message, error)
}
