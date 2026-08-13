package test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
)

type stubTool struct {
	definition ai.ToolDefinition
	execute    func(context.Context, json.RawMessage, ai.UpdateEmitter) (ai.ToolOutput, error)
}

func (t *stubTool) Definition() ai.ToolDefinition { return t.definition }

func (t *stubTool) Execute(
	ctx context.Context,
	args json.RawMessage,
	emit ai.UpdateEmitter,
) (ai.ToolOutput, error) {
	return t.execute(ctx, args, emit)
}

func testTool(name string, execute func(context.Context, json.RawMessage, ai.UpdateEmitter) (ai.ToolOutput, error)) *stubTool {
	return &stubTool{
		definition: ai.ToolDefinition{
			Name:        name,
			Description: "test tool",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"text": map[string]any{"type": "string"},
				},
				"required":             []string{"text"},
				"additionalProperties": false,
			},
		},
		execute: execute,
	}
}

func newTestToolRuntime(t *testing.T, registrations []pi.MiddlewareRegistration, tools ...ai.Tool) pi.ToolRuntime {
	t.Helper()
	toolRuntime, err := pi.NewToolRuntime(pi.ToolRuntimeOptions{Tools: tools, Middlewares: registrations})
	if err != nil {
		t.Fatalf("NewToolRuntime error = %v", err)
	}
	return toolRuntime
}

func TestToolRuntimeSortsDefinitions(t *testing.T) {
	execute := func(context.Context, json.RawMessage, ai.UpdateEmitter) (ai.ToolOutput, error) {
		return ai.ToolOutput{Content: []ai.ContentBlock{ai.TextBlock("ok")}}, nil
	}
	toolRuntime := newTestToolRuntime(t, pi.DefaultMiddlewareRegistrations(), testTool("zeta", execute), testTool("alpha", execute))

	definitions := toolRuntime.Definitions()
	if len(definitions) != 2 || definitions[0].Name != "alpha" || definitions[1].Name != "zeta" {
		t.Fatalf("definitions = %#v", definitions)
	}
}

func TestToolRuntimeRejectsInvalidCallsBeforeToolExecution(t *testing.T) {
	var calls atomic.Int32
	tool := testTool("echo", func(context.Context, json.RawMessage, ai.UpdateEmitter) (ai.ToolOutput, error) {
		calls.Add(1)
		return ai.ToolOutput{Content: []ai.ContentBlock{ai.TextBlock("unexpected")}}, nil
	})
	toolRuntime := newTestToolRuntime(t, pi.DefaultMiddlewareRegistrations(), tool)

	tests := []struct {
		name string
		args json.RawMessage
	}{
		{name: "invalid JSON", args: json.RawMessage(`{"text":`)},
		{name: "missing required field", args: json.RawMessage(`{}`)},
		{name: "wrong type", args: json.RawMessage(`{"text":42}`)},
		{name: "unknown field", args: json.RawMessage(`{"text":"ok","secret":true}`)},
		{name: "trailing JSON", args: json.RawMessage(`{"text":"ok"} {}`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var events []pi.ToolEvent
			call := ai.ToolCall{ID: "call-" + tt.name, Name: "echo", Arguments: tt.args}
			result, err := toolRuntime.Execute(context.Background(), call, func(_ context.Context, event pi.ToolEvent) {
				events = append(events, event)
			})
			if err != nil || !result.IsError {
				t.Fatalf("Execute() = (%#v, %v), want ordinary error result", result, err)
			}
			if len(events) != 2 || events[0].Phase != pi.ToolEventStart || events[1].Phase != pi.ToolEventEnd {
				t.Fatalf("events = %#v, want start/end", events)
			}
		})
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("tool calls = %d, want 0", got)
	}
}

func TestToolRuntimeEmitsStartUpdateEndOnceWithCallIdentity(t *testing.T) {
	tool := testTool("echo", func(_ context.Context, _ json.RawMessage, emit ai.UpdateEmitter) (ai.ToolOutput, error) {
		emit(ai.ToolUpdate{Content: []ai.ContentBlock{ai.TextBlock("chunk")}, Details: "progress"})
		return ai.ToolOutput{Content: []ai.ContentBlock{ai.TextBlock("done")}, Details: "final"}, nil
	})
	toolRuntime := newTestToolRuntime(t, pi.DefaultMiddlewareRegistrations(), tool)
	call := ai.ToolCall{ID: "call-42", Name: "echo", Arguments: json.RawMessage(`{"text":"hello"}`)}
	var events []pi.ToolEvent

	result, err := toolRuntime.Execute(context.Background(), call, func(_ context.Context, event pi.ToolEvent) {
		events = append(events, event)
	})
	if err != nil || result.IsError || result.ToolCallID != call.ID || result.ToolName != call.Name || result.Details != "final" {
		t.Fatalf("Execute() = (%#v, %v)", result, err)
	}
	if len(events) != 3 {
		t.Fatalf("events = %#v, want exactly three", events)
	}
	for index, phase := range []pi.ToolEventPhase{pi.ToolEventStart, pi.ToolEventUpdate, pi.ToolEventEnd} {
		if events[index].Phase != phase || events[index].Call.ID != call.ID || events[index].Call.Name != call.Name {
			t.Fatalf("events[%d] = %#v", index, events[index])
		}
	}
	if events[1].Update == nil || events[1].Update.Details != "progress" || toolEventText(t, events[1].Update.Content) != "chunk" {
		t.Fatalf("update = %#v", events[1].Update)
	}
	if events[2].Result == nil || events[2].Result.ToolCallID != call.ID {
		t.Fatalf("end = %#v", events[2])
	}
}

func TestToolRuntimeNormalizesOrdinaryAndContextErrors(t *testing.T) {
	tests := []struct {
		name       string
		output     ai.ToolOutput
		executeErr error
		wantGoErr  error
		wantText   string
	}{
		{name: "ordinary error uses error text", executeErr: errors.New("disk failed"), wantText: "disk failed"},
		{name: "ordinary error retains output", output: ai.ToolOutput{Content: []ai.ContentBlock{ai.TextBlock("partial")}, Details: "kept"}, executeErr: errors.New("disk failed"), wantText: "partial"},
		{name: "canceled", executeErr: context.Canceled, wantGoErr: context.Canceled, wantText: "context canceled"},
		{name: "deadline", executeErr: context.DeadlineExceeded, wantGoErr: context.DeadlineExceeded, wantText: "context deadline exceeded"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := testTool("echo", func(context.Context, json.RawMessage, ai.UpdateEmitter) (ai.ToolOutput, error) {
				return tt.output, tt.executeErr
			})
			toolRuntime := newTestToolRuntime(t, pi.DefaultMiddlewareRegistrations(), tool)
			result, err := toolRuntime.Execute(context.Background(), ai.ToolCall{ID: "call", Name: "echo", Arguments: json.RawMessage(`{"text":"x"}`)}, nil)
			if !result.IsError || !strings.Contains(toolResultText(t, result), tt.wantText) {
				t.Fatalf("result = %#v", result)
			}
			if !errors.Is(err, tt.wantGoErr) || (tt.wantGoErr == nil && err != nil) {
				t.Fatalf("error = %v, want %v", err, tt.wantGoErr)
			}
			if tt.output.Details != nil && result.Details != tt.output.Details {
				t.Fatalf("Details = %#v, want %#v", result.Details, tt.output.Details)
			}
		})
	}
}

func TestToolRuntimeUsesConcreteToolErrorTextWhenAdapterOutputIsEmpty(t *testing.T) {
	tool := testTool("concrete", func(context.Context, json.RawMessage, ai.UpdateEmitter) (ai.ToolOutput, error) {
		return ai.ToolOutput{}, errors.New("concrete operation failed")
	})
	toolRuntime := newTestToolRuntime(t, pi.DefaultMiddlewareRegistrations(), tool)

	result, err := toolRuntime.Execute(context.Background(), ai.ToolCall{
		ID:        "call-concrete",
		Name:      "concrete",
		Arguments: json.RawMessage(`{"text":"x"}`),
	}, nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.IsError || toolResultText(t, result) != "concrete operation failed" {
		t.Fatalf("result = %#v", result)
	}
}

func TestToolRuntimePreCanceledContextSkipsToolAndEvents(t *testing.T) {
	var calls atomic.Int32
	tool := testTool("echo", func(context.Context, json.RawMessage, ai.UpdateEmitter) (ai.ToolOutput, error) {
		calls.Add(1)
		return ai.ToolOutput{}, nil
	})
	toolRuntime := newTestToolRuntime(t, pi.DefaultMiddlewareRegistrations(), tool)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var events []pi.ToolEvent

	result, err := toolRuntime.Execute(ctx, ai.ToolCall{ID: "call", Name: "echo"}, func(_ context.Context, event pi.ToolEvent) {
		events = append(events, event)
	})
	if !errors.Is(err, context.Canceled) || len(result.Content) != 0 || calls.Load() != 0 || len(events) != 0 {
		t.Fatalf("Execute() = (%#v, %v), calls=%d events=%#v", result, err, calls.Load(), events)
	}
}

func TestToolRuntimeReturnsContextErrorWhenToolCancelsDuringExecution(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	tool := testTool("cancel", func(context.Context, json.RawMessage, ai.UpdateEmitter) (ai.ToolOutput, error) {
		cancel()
		return ai.ToolOutput{Content: []ai.ContentBlock{ai.TextBlock("partial")}}, nil
	})
	toolRuntime := newTestToolRuntime(t, nil, tool)

	result, err := toolRuntime.Execute(ctx, ai.ToolCall{ID: "call", Name: "cancel"}, nil)
	if !errors.Is(err, context.Canceled) || !result.IsError || toolResultText(t, result) != "partial" {
		t.Fatalf("Execute() = (%#v, %v), want partial canceled result", result, err)
	}
}

func TestToolRuntimeUnknownToolReturnsErrorResultWithoutEvents(t *testing.T) {
	toolRuntime := newTestToolRuntime(t, pi.DefaultMiddlewareRegistrations())
	var events []pi.ToolEvent
	call := ai.ToolCall{ID: "missing-id", Name: "missing"}
	result, err := toolRuntime.Execute(context.Background(), call, func(_ context.Context, event pi.ToolEvent) {
		events = append(events, event)
	})
	if err != nil || !result.IsError || result.ToolCallID != call.ID || result.ToolName != call.Name || !strings.Contains(toolResultText(t, result), "not registered") || len(events) != 0 {
		t.Fatalf("Execute() = (%#v, %v), events=%#v", result, err, events)
	}
}

func toolResultText(t *testing.T, result pi.ToolResult) string {
	t.Helper()
	return toolEventText(t, result.Content)
}

func toolEventText(t *testing.T, content []ai.ContentBlock) string {
	t.Helper()
	text, err := ai.TextContent(content)
	if err != nil {
		t.Fatalf("TextContent() error = %v", err)
	}
	return text
}
