package pi

import (
	"context"
	"encoding/json"
	"errors"
	"runtime/debug"
	"sort"
	"strings"
	"unicode/utf8"

	logsdk "github.com/PycMono/go-logger-sdk"
	"github.com/PycMono/go-reagent/pi/ai"
	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
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
func DefaultMiddlewareRegistrations() []MiddlewareRegistration {
	return []MiddlewareRegistration{
		{Name: "panic_recovery", Order: 10, Middleware: panicRecoveryMiddleware()},
		{Name: "schema_validation", Order: 20, Middleware: schemaValidationMiddleware()},
		{Name: "logging", Order: 30, Middleware: loggingMiddleware()},
		{Name: "event_forwarding", Order: 40, Middleware: eventForwardingMiddleware()},
	}
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
