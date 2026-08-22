package pi

import (
	"context"
	"encoding/json"
	"errors"
	"runtime/debug"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	contexttracing "github.com/PycMono/go-context-sdk/tracing"
	logsdk "github.com/PycMono/go-logger-sdk"
	"github.com/PycMono/go-reagent/pi/ai"
	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
	"github.com/PycMono/go-reagent/pi/harness/observability"
)

type Execution struct {
	Call         ai.ToolCall
	Definition   ai.ToolDefinition
	Tool         ai.Tool
	Observer     ToolEventObserver
	ValidateArgs func(json.RawMessage) error
}

type Handler func(context.Context, Execution, ai.UpdateEmitter) (ai.ToolOutput, error)
type Middleware func(Handler) Handler

type MiddlewareRegistration struct {
	Name       string
	Order      int
	Middleware Middleware
}

const (
	maxToolOutputBytes         = 50 * 1024
	toolOutputTruncationMarker = "\n[output truncated]"
)

// DefaultMiddlewareRegistrations returns a fresh ordered default middleware set.
//
// tracing（Order=5）包住现有 Middleware 与真实 Tool 调用（§4.6）；未注册
// Tool 在 ToolRuntime 入口即返回，不经过本链，不创建执行 Span。
func DefaultMiddlewareRegistrations() []MiddlewareRegistration {
	return []MiddlewareRegistration{
		{Name: "tracing", Order: 5, Middleware: tracingMiddleware()},
		{Name: "panic_recovery", Order: 10, Middleware: panicRecoveryMiddleware()},
		{Name: "schema_validation", Order: 20, Middleware: schemaValidationMiddleware()},
		{Name: "logging", Order: 30, Middleware: loggingMiddleware()},
		{Name: "event_forwarding", Order: 40, Middleware: eventForwardingMiddleware()},
	}
}

// tracingMiddleware 为每次实际 Tool 执行创建 execute_tool Span 并记录
// 执行指标（§4.6、§8.3）。Span 只记录元数据与长度，不采集参数/输出正文
// （§11 content.mode=none）；状态与生命周期由 WithSpan 管理。
func tracingMiddleware() Middleware {
	return func(next Handler) Handler {
		return func(ctx context.Context, execution Execution, emit ai.UpdateEmitter) (output ai.ToolOutput, err error) {
			startedAt := time.Now()
			err = contexttracing.WithSpan(ctx, observability.ToolSpanName(execution.Definition.Name), func(ctx context.Context) error {
				ctx = contexttracing.WithKV(ctx,
					contexttracing.OperationName("execute_tool"),
					contexttracing.ToolName(execution.Definition.Name),
					contexttracing.ToolCallID(execution.Call.ID),
					contexttracing.KV(observability.AttrToolParallelSafe, execution.Definition.ParallelSafe),
					contexttracing.KV(observability.AttrToolArgumentsSize, len(execution.Call.Arguments)),
				)

				var runErr error
				output, runErr = next(ctx, execution, emit)

				// Tool 业务性失败（IsError）设置 Error Status，但 Scheduler 成功
				// 返回这类结果不会把整个 Run 标记为内部错误（§4.9）。
				fields := []contexttracing.Field{
					contexttracing.KV(observability.AttrToolIsError, runErr != nil),
					contexttracing.KV(observability.AttrToolOutputSize, toolOutputSize(output)),
				}
				fields = append(fields, observability.ErrorFields(runErr)...)
				contexttracing.WithKV(ctx, fields...)
				observability.RecordToolExecution(ctx, execution.Definition.Name, runErr, time.Since(startedAt))
				return runErr
			}, contexttracing.WithErrorClassifier(observability.ClassifyError))
			return output, err
		}
	}
}

func toolOutputSize(output ai.ToolOutput) int {
	size := 0
	for _, block := range output.Content {
		size += len(block.Text)
	}
	return size
}

func composeHandler(registrations []MiddlewareRegistration) Handler {
	ordered := append([]MiddlewareRegistration(nil), registrations...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Order != ordered[j].Order {
			return ordered[i].Order < ordered[j].Order
		}
		return ordered[i].Name < ordered[j].Name
	})
	handler := Handler(func(ctx context.Context, execution Execution, emit ai.UpdateEmitter) (ai.ToolOutput, error) {
		return execution.Tool.Execute(ctx, execution.Call.Arguments, emit)
	})
	for index := len(ordered) - 1; index >= 0; index-- {
		handler = ordered[index].Middleware(handler)
	}
	return handler
}

func panicRecoveryMiddleware() Middleware {
	return func(next Handler) Handler {
		return func(ctx context.Context, execution Execution, emit ai.UpdateEmitter) (output ai.ToolOutput, err error) {
			defer func() {
				if recover() == nil {
					return
				}
				logsdk.Error(ctx, "tool execution panic",
					logsdk.Any("component", "tool_runtime"),
					logsdk.Any("tool", execution.Definition.Name),
					logsdk.Any("tool_call_id", execution.Call.ID),
					logsdk.Any("phase", "panic"),
					logsdk.Any("stack", debug.Stack()),
				)
				output = ai.ToolOutput{}
				err = pierrors.Wrap(
					pierrors.ErrorCodeToolPanic,
					"tool panic",
					errors.New("tool execution failed"),
				)
			}()
			return next(ctx, execution, emit)
		}
	}
}

func schemaValidationMiddleware() Middleware {
	return func(next Handler) Handler {
		return func(ctx context.Context, execution Execution, emit ai.UpdateEmitter) (ai.ToolOutput, error) {
			if execution.ValidateArgs != nil {
				if err := execution.ValidateArgs(execution.Call.Arguments); err != nil {
					return ai.ToolOutput{}, pierrors.Wrap(
						pierrors.ErrorCodeToolInvalidArguments,
						"tool arguments",
						err,
					)
				}
			}
			return next(ctx, execution, emit)
		}
	}
}

func loggingMiddleware() Middleware {
	return func(next Handler) Handler {
		return func(ctx context.Context, execution Execution, emit ai.UpdateEmitter) (ai.ToolOutput, error) {
			fields := []logsdk.Fields{
				logsdk.Any("component", "tool_runtime"),
				logsdk.Any("tool", execution.Definition.Name),
				logsdk.Any("tool_call_id", execution.Call.ID),
			}
			logsdk.Info(ctx, "tool execution", append(fields, logsdk.Any("phase", "start"))...)
			output, err := next(ctx, execution, emit)
			status := "success"
			if err != nil {
				status = "error"
			}
			logsdk.Info(ctx, "tool execution", append(fields,
				logsdk.Any("phase", "end"),
				logsdk.Any("byte_count", contentByteCount(output.Content)),
				logsdk.Any("status", status),
			)...)
			return output, err
		}
	}
}

func eventForwardingMiddleware() Middleware {
	return func(next Handler) Handler {
		return func(ctx context.Context, execution Execution, emit ai.UpdateEmitter) (ai.ToolOutput, error) {
			forward := func(update ai.ToolUpdate) {
				if execution.Observer != nil {
					execution.Observer(ctx, NewToolUpdate(execution.Call, update))
				}
				if emit != nil {
					emit(update)
				}
			}
			return next(ctx, execution, forward)
		}
	}
}

func limitToolOutput(output ai.ToolOutput) ai.ToolOutput {
	limited, truncated := limitContent(output.Content)
	output.Content = limited
	if truncated {
		output.Details = withTruncationDetail(output.Details)
	}
	return output
}

func limitContent(content []ai.ContentBlock) ([]ai.ContentBlock, bool) {
	remaining := maxToolOutputBytes
	limited := make([]ai.ContentBlock, 0, len(content)+1)
	truncated := false
	for _, block := range content {
		if block.Type != ai.ContentTypeText {
			limited = append(limited, block)
			continue
		}
		text := strings.ToValidUTF8(block.Text, "�")
		if len(text) <= remaining {
			block.Text = text
			limited = append(limited, block)
			remaining -= len(text)
			continue
		}
		cut := remaining
		for cut > 0 && !utf8.ValidString(text[:cut]) {
			cut--
		}
		block.Text = text[:cut]
		limited = append(limited, block)
		truncated = true
		break
	}
	if !truncated && len(limited) < len(content) {
		truncated = true
	}
	if truncated {
		limited = append(limited, ai.TextBlock(toolOutputTruncationMarker))
	}
	return limited, truncated
}

func withTruncationDetail(details any) any {
	if existing, ok := details.(map[string]any); ok {
		cloned := make(map[string]any, len(existing)+1)
		for key, value := range existing {
			cloned[key] = value
		}
		cloned["truncated"] = true
		return cloned
	}
	if details == nil {
		return map[string]any{"truncated": true}
	}
	return map[string]any{"tool_details": details, "truncated": true}
}

func contentByteCount(content []ai.ContentBlock) int {
	count := 0
	for _, block := range content {
		if block.Type == ai.ContentTypeText {
			count += len(block.Text)
		}
	}
	return count
}
