package middleware

import (
	"errors"
	"runtime/debug"

	logsdk "github.com/PycMono/go-logger-sdk"
	"github.com/PycMono/go-reagent/pi/ai"
	pierrors "github.com/PycMono/go-reagent/pi/errors"
)

// PanicRecovery 捕获内层 handler 与 Tool 执行的 panic，转为 ToolPanic 错误。
// recover 后必须 Block：否则控制流回到外层 Next 循环会继续执行后续 handler。
func PanicRecovery(e *Execution) {
	defer func() {
		if recover() == nil {
			return
		}
		logsdk.Error(e.Ctx, "tool execution panic",
			logsdk.Any("component", "tool_runtime"),
			logsdk.Any("tool", e.Definition.Name),
			logsdk.Any("tool_call_id", e.Call.ID),
			logsdk.Any("phase", "panic"),
			logsdk.Any("stack", debug.Stack()),
		)
		e.Output = ai.ToolOutput{}
		e.Block(pierrors.Wrap(
			pierrors.ErrorCodeToolPanic,
			"tool panic",
			errors.New("tool execution failed"),
		))
	}()
	e.Next()
}
