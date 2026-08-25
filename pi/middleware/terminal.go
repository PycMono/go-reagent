package middleware

// ExecuteTool 是链末端的终端 handler，调用真实 Tool 并把结果写入
// Execution。由 ToolRuntime 组装时追加在所有中间件之后。
func ExecuteTool(e *Execution) {
	output, err := e.Tool.Execute(e.Ctx, e.Call.Arguments, e.Emit)
	e.Output = output
	if err != nil {
		e.Err = err
	}
}
