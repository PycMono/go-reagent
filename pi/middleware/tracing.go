package middleware

import (
	"context"

	contexttracing "github.com/PycMono/go-context-sdk/tracing"
	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/harness/observability"
)

// Tracing 为每次实际 Tool 执行创建 execute_tool Span 。
// Span 只记录元数据与长度，不采集参数/输出正文；状态与生命周期由
// WithSpan 管理。
func Tracing(e *Execution) {
	err := contexttracing.WithSpan(e.Ctx, observability.ToolSpanName(e.Definition.Name), func(ctx context.Context) error {
		e.Ctx = ctx
		contexttracing.WithKV(ctx,
			contexttracing.OperationName("execute_tool"),
			contexttracing.ToolName(e.Definition.Name),
			contexttracing.ToolCallID(e.Call.ID),
			contexttracing.KV(observability.AttrToolParallelSafe, e.Definition.ParallelSafe),
			contexttracing.KV(observability.AttrToolArgumentsSize, len(e.Call.Arguments)),
		)

		e.Next()

		// Tool 业务性失败（IsError）设置 Error Status，但 Scheduler 成功
		// 返回这类结果不会把整个 Run 标记为内部错误。
		fields := []contexttracing.Field{
			contexttracing.KV(observability.AttrToolIsError, e.Err != nil),
			contexttracing.KV(observability.AttrToolOutputSize, toolOutputSize(e.Output)),
		}
		fields = append(fields, observability.ErrorFields(e.Err)...)
		contexttracing.WithKV(ctx, fields...)
		return e.Err
	}, contexttracing.WithErrorClassifier(observability.ClassifyError))
	e.Err = err
}

func toolOutputSize(output ai.ToolOutput) int {
	size := 0
	for _, block := range output.Content {
		size += len(block.Text)
	}
	return size
}
