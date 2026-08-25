package middleware

import (
	"github.com/PycMono/go-reagent/pi/ai"
	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
)

// SchemaValidation 在执行前校验 Tool 参数。校验失败 Block 短路——只
// return 不 Block 时，外层 Next 循环仍会继续执行后续 handler。
func SchemaValidation(e *Execution) {
	if e.ValidateArgs != nil {
		if err := e.ValidateArgs(e.Call.Arguments); err != nil {
			e.Output = ai.ToolOutput{}
			e.Block(pierrors.Wrap(
				pierrors.ErrorCodeToolInvalidArguments,
				"tool arguments",
				err,
			))
			return
		}
	}
	e.Next()
}
