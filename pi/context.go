package pi

import "github.com/PycMono/go-reagent/pi/ai"

// RunContext 是一次运行交给 Loop 使用的消息和工具快照。
type RunContext struct {
	// Messages 是当前运行发送给模型的初始消息。
	Messages []ai.Message
	// Tools 是当前运行允许模型调用的工具定义。
	Tools []ai.ToolDefinition
}
