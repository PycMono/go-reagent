package test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
)

const (
	testMaxToolOutputBytes         = 50 * 1024
	testToolOutputTruncationMarker = "\n[output truncated]"
)

func TestMiddlewareRunsByOrderThenName(t *testing.T) {
	var sequence []string
	middleware := func(name string) pi.Middleware {
		return func(next pi.Handler) pi.Handler {
			return func(ctx context.Context, execution pi.Execution, emit ai.UpdateEmitter) (ai.ToolOutput, error) {
				sequence = append(sequence, name+":before")
				output, err := next(ctx, execution, emit)
				sequence = append(sequence, name+":after")
				return output, err
			}
		}
	}
	registrations := []pi.MiddlewareRegistration{
		{Name: "zeta", Order: 100, Middleware: middleware("zeta")},
		{Name: "beta", Order: 50, Middleware: middleware("beta")},
		{Name: "alpha", Order: 50, Middleware: middleware("alpha")},
	}
	tool := testTool("echo", func(context.Context, json.RawMessage, ai.UpdateEmitter) (ai.ToolOutput, error) {
		sequence = append(sequence, "tool")
		return ai.ToolOutput{Content: []ai.ContentBlock{ai.TextBlock("ok")}}, nil
	})
	toolRuntime := newTestToolRuntime(t, registrations, tool)

	result, err := toolRuntime.Execute(context.Background(), ai.ToolCall{ID: "call", Name: "echo", Arguments: json.RawMessage(`{"text":"x"}`)}, nil)
	if err != nil || result.IsError {
		t.Fatalf("Execute() = (%#v, %v)", result, err)
	}
	want := "alpha:before,beta:before,zeta:before,tool,zeta:after,beta:after,alpha:after"
	if got := strings.Join(sequence, ","); got != want {
		t.Fatalf("sequence = %q, want %q", got, want)
	}
}

func TestPanicRecoveryMiddlewareConvertsPanicToGenericErrorResult(t *testing.T) {
	tool := testTool("panic", func(context.Context, json.RawMessage, ai.UpdateEmitter) (ai.ToolOutput, error) {
		panic("sensitive panic value")
	})
	toolRuntime := newTestToolRuntime(t, pi.DefaultMiddlewareRegistrations(), tool)

	result, err := toolRuntime.Execute(context.Background(), ai.ToolCall{ID: "call", Name: "panic", Arguments: json.RawMessage(`{"text":"x"}`)}, nil)
	text := toolResultText(t, result)
	if err != nil || !result.IsError || result.ErrorCode != pierrors.ErrorCodeToolPanic ||
		strings.Contains(text, "sensitive panic value") || !strings.Contains(text, "failed") {
		t.Fatalf("Execute() = (%#v, %v)", result, err)
	}
	registrations := pi.DefaultMiddlewareRegistrations()
	if registrations[0].Name != "panic_recovery" {
		t.Fatalf("default recovery middleware = %q", registrations[0].Name)
	}
}

func TestToolRuntimeMakesEmptyToolOutputExplicitWithoutMiddleware(t *testing.T) {
	tool := testTool("empty", func(context.Context, json.RawMessage, ai.UpdateEmitter) (ai.ToolOutput, error) {
		return ai.ToolOutput{}, nil
	})
	toolRuntime := newTestToolRuntime(t, nil, tool)

	result, err := toolRuntime.Execute(context.Background(), ai.ToolCall{ID: "call", Name: "empty", Arguments: json.RawMessage(`{"text":"x"}`)}, nil)
	if err != nil || result.IsError || toolResultText(t, result) != "(no output)" {
		t.Fatalf("Execute() = (%#v, %v)", result, err)
	}
}

func TestToolRuntimeTruncatesToolOutputWithoutMiddleware(t *testing.T) {
	content := strings.Repeat("a", testMaxToolOutputBytes-1) + "界" + strings.Repeat("z", 100)
	tool := testTool("large", func(context.Context, json.RawMessage, ai.UpdateEmitter) (ai.ToolOutput, error) {
		return ai.ToolOutput{Content: []ai.ContentBlock{ai.TextBlock(content)}}, nil
	})
	toolRuntime := newTestToolRuntime(t, nil, tool)

	result, err := toolRuntime.Execute(context.Background(), ai.ToolCall{ID: "call", Name: "large", Arguments: json.RawMessage(`{"text":"x"}`)}, nil)
	text := toolResultText(t, result)
	if err != nil || result.IsError || !utf8.ValidString(text) || !strings.Contains(text, testToolOutputTruncationMarker) {
		t.Fatalf("Execute() error=%v result=%#v valid=%v bytes=%d", err, result, utf8.ValidString(text), len(text))
	}
	if len(strings.TrimSuffix(text, testToolOutputTruncationMarker)) > testMaxToolOutputBytes {
		t.Fatalf("retained content bytes = %d, want <= %d", len(strings.TrimSuffix(text, testToolOutputTruncationMarker)), testMaxToolOutputBytes)
	}
	details, ok := result.Details.(map[string]any)
	if !ok || details["truncated"] != true {
		t.Fatalf("Details = %#v, want truncated=true", result.Details)
	}
}
