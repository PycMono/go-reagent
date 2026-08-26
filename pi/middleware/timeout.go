package middleware

import (
	"context"
	"errors"
	"time"

	"github.com/PycMono/go-reagent/pi/ai"
	pierrors "github.com/PycMono/go-reagent/pi/errors"
)

// Timeout 返回单次 Tool 执行的超时兜底 Handler。它是协作式取消：派生
// 带期限的 ctx 交给内层链，超时后把错误归一为 tool_timeout——Tool 必须
// 尊重 ctx 才会真正停下，否则会等它自然返回后才记录超时。
//
// 返回前把 e.Ctx 还原为父 ctx，因此可以安全地被 Retry 重跑：每次重试
// 都会从同一个父 ctx 派生新的期限。
func Timeout(d time.Duration) Handler {
	return func(e *Execution) {
		parent := e.Ctx
		ctx, cancel := context.WithTimeout(parent, d)
		defer cancel()
		e.Ctx = ctx
		defer func() { e.Ctx = parent }()

		e.Next()

		// 仅当是本 Handler 的期限触发（父 ctx 仍然存活）时归一为
		// tool_timeout；父 ctx 的取消/超期由 ToolRuntime 归一为
		// canceled/deadline_exceeded，不在此覆盖。
		if errors.Is(ctx.Err(), context.DeadlineExceeded) && parent.Err() == nil {
			e.Output = ai.ToolOutput{}
			e.Err = pierrors.Wrap(
				pierrors.ErrorCodeToolTimeout,
				"tool timeout",
				ctx.Err(),
			)
		}
	}
}
