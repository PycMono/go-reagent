package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/PycMono/go-reagent/ai"
)

func TestMiddlewareRunsByOrderThenName(t *testing.T) {
	var sequence []string
	middleware := func(name string) Middleware {
		return func(next Handler) Handler {
			return func(ctx context.Context, execution Execution, emit UpdateEmitter) (ToolOutput, error) {
				sequence = append(sequence, name+":before")
				output, err := next(ctx, execution, emit)
				sequence = append(sequence, name+":after")
				return output, err
			}
		}
	}
	registrations := []MiddlewareRegistration{
		{Name: "zeta", Order: 100, Middleware: middleware("zeta")},
		{Name: "beta", Order: 50, Middleware: middleware("beta")},
		{Name: "alpha", Order: 50, Middleware: middleware("alpha")},
	}
	tool := testTool("echo", func(context.Context, json.RawMessage, UpdateEmitter) (ToolOutput, error) {
		sequence = append(sequence, "tool")
		return ToolOutput{Content: []ai.ContentBlock{ai.TextBlock("ok")}}, nil
	})
	registry := newTestRegistry(t, registrations, tool)

	result, err := registry.Execute(context.Background(), ai.ToolCall{ID: "call", Name: "echo", Arguments: json.RawMessage(`{"text":"x"}`)}, nil)
	if err != nil || result.IsError {
		t.Fatalf("Execute() = (%#v, %v)", result, err)
	}
	want := "alpha:before,beta:before,zeta:before,tool,zeta:after,beta:after,alpha:after"
	if got := strings.Join(sequence, ","); got != want {
		t.Fatalf("sequence = %q, want %q", got, want)
	}
}

func TestRecoveryMiddlewareConvertsPanicToGenericErrorResult(t *testing.T) {
	tool := testTool("panic", func(context.Context, json.RawMessage, UpdateEmitter) (ToolOutput, error) {
		panic("sensitive panic value")
	})
	registry := newTestRegistry(t, DefaultMiddlewareRegistrations(), tool)

	result, err := registry.Execute(context.Background(), ai.ToolCall{ID: "call", Name: "panic", Arguments: json.RawMessage(`{"text":"x"}`)}, nil)
	text := toolResultText(t, result)
	if err != nil || !result.IsError || strings.Contains(text, "sensitive panic value") || !strings.Contains(text, "failed") {
		t.Fatalf("Execute() = (%#v, %v)", result, err)
	}
}

func TestOutputMiddlewareMakesEmptySuccessExplicit(t *testing.T) {
	tool := testTool("empty", func(context.Context, json.RawMessage, UpdateEmitter) (ToolOutput, error) {
		return ToolOutput{}, nil
	})
	registry := newTestRegistry(t, DefaultMiddlewareRegistrations(), tool)

	result, err := registry.Execute(context.Background(), ai.ToolCall{ID: "call", Name: "empty", Arguments: json.RawMessage(`{"text":"x"}`)}, nil)
	if err != nil || result.IsError || toolResultText(t, result) != "(no output)" {
		t.Fatalf("Execute() = (%#v, %v)", result, err)
	}
}

func TestOutputMiddlewareTruncatesOnUtf8Boundary(t *testing.T) {
	content := strings.Repeat("a", maxToolOutputBytes-1) + "界" + strings.Repeat("z", 100)
	tool := testTool("large", func(context.Context, json.RawMessage, UpdateEmitter) (ToolOutput, error) {
		return ToolOutput{Content: []ai.ContentBlock{ai.TextBlock(content)}}, nil
	})
	registry := newTestRegistry(t, DefaultMiddlewareRegistrations(), tool)

	result, err := registry.Execute(context.Background(), ai.ToolCall{ID: "call", Name: "large", Arguments: json.RawMessage(`{"text":"x"}`)}, nil)
	text := toolResultText(t, result)
	if err != nil || result.IsError || !utf8.ValidString(text) || !strings.Contains(text, toolOutputTruncationMarker) {
		t.Fatalf("Execute() error=%v result=%#v valid=%v bytes=%d", err, result, utf8.ValidString(text), len(text))
	}
	if len(strings.TrimSuffix(text, toolOutputTruncationMarker)) > maxToolOutputBytes {
		t.Fatalf("retained content bytes = %d, want <= %d", len(strings.TrimSuffix(text, toolOutputTruncationMarker)), maxToolOutputBytes)
	}
	details, ok := result.Details.(map[string]any)
	if !ok || details["truncated"] != true {
		t.Fatalf("Details = %#v, want truncated=true", result.Details)
	}
}
