package conversation

import (
	"context"

	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
)

// RunRequest 保存会话业务执行一次 Agent 所需的输入。
type RunRequest struct {
	// UserID 是会话所属的用户标识。
	UserID string
	// ConversationID 是业务侧的会话标识。
	ConversationID string
	// RunID 是本轮持久化和调用总账使用的追踪标识。
	RunID string
	// Input 是本轮用户消息。
	Input ai.Message
	// ResponsePolicy 是仅附加到模型输入、不写入会话历史的本轮回复约束。
	ResponsePolicy string
	// Context 是本轮额外注入的业务上下文。
	Context []pi.ContextBlock
}

// Runner 定义带会话加载和持久化的一次运行行为。
type Runner interface {
	Run(context.Context, RunRequest, pi.Reporter) (pi.RunResult, error)
}
