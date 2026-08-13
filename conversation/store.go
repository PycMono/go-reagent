package conversation

import (
	"context"

	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
)

type RunRequest struct {
	UserID         string            // 用户ID
	ConversationID string            // 会话ID
	RunID          string            // 运行ID
	Input          ai.Message        // 用户输入
	Context        []pi.ContextBlock //
}

type Runner interface {
	Run(context.Context, RunRequest, pi.Reporter) (pi.RunResult, error)
}
