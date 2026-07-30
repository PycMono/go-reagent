package engine_test

import (
	"context"
	"encoding/json"
	"reflect"
	"sync"
	"testing"

	"github.com/PycMono/go-reagent/internal/engine"
	"github.com/PycMono/go-reagent/internal/schema"
)

type reporterEvent struct {
	kind     string
	toolName string
	payload  string
	isError  bool
}

type recordingReporter struct {
	mu     sync.Mutex
	events []reporterEvent
}

func (r *recordingReporter) OnThinking(context.Context) {
	r.record(reporterEvent{kind: "thinking"})
}

func (r *recordingReporter) OnToolCall(_ context.Context, toolName string, args string) {
	r.record(reporterEvent{kind: "tool_call", toolName: toolName, payload: args})
}

func (r *recordingReporter) OnToolResult(_ context.Context, toolName string, result string, isError bool) {
	r.record(reporterEvent{kind: "tool_result", toolName: toolName, payload: result, isError: isError})
}

func (r *recordingReporter) OnMessage(_ context.Context, content string) {
	r.record(reporterEvent{kind: "message", payload: content})
}

func (r *recordingReporter) record(event reporterEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

func (r *recordingReporter) Events() []reporterEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]reporterEvent(nil), r.events...)
}

func TestMultiReporterForwardsEveryLifecycleMethod(t *testing.T) {
	first := &recordingReporter{}
	second := &recordingReporter{}
	reporter := engine.NewMultiReporter(first, nil, second)
	ctx := context.Background()

	reporter.OnThinking(ctx)
	reporter.OnToolCall(ctx, "read_file", `{"path":"a.txt"}`)
	reporter.OnToolResult(ctx, "read_file", "file A", false)
	reporter.OnMessage(ctx, "done")

	want := []reporterEvent{
		{kind: "thinking"},
		{kind: "tool_call", toolName: "read_file", payload: `{"path":"a.txt"}`},
		{kind: "tool_result", toolName: "read_file", payload: "file A"},
		{kind: "message", payload: "done"},
	}
	for name, recorder := range map[string]*recordingReporter{"first": first, "second": second} {
		if got := recorder.Events(); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s events = %#v, want %#v", name, got, want)
		}
	}
}

func TestAgentEngineReportsEveryLifecycleEventWithoutAggregation(t *testing.T) {
	provider := &fakeProvider{responses: []*schema.Message{
		{Role: schema.RoleAssistant, Content: "plan one"},
		{
			Role:    schema.RoleAssistant,
			Content: "starting tool",
			ToolCalls: []schema.ToolCall{{
				ID:        "call-1",
				Name:      "read_file",
				Arguments: json.RawMessage(`{"path":"a.txt"}`),
			}},
		},
		{Role: schema.RoleAssistant, Content: "plan two"},
		{Role: schema.RoleAssistant, Content: "done"},
	}}
	registry := &fakeRegistry{
		definitions: []schema.ToolDefinition{{Name: "read_file"}},
		results: map[string]schema.ToolResult{
			"read_file": {ToolCallID: "call-1", Output: "file A"},
		},
	}
	reporter := &recordingReporter{}
	agentEngine := engine.NewAgentEngine(provider, registry, "/workspace", true)

	if err := agentEngine.Run(context.Background(), "read a", reporter); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	want := []reporterEvent{
		{kind: "thinking"},
		{kind: "message", payload: "starting tool"},
		{kind: "tool_call", toolName: "read_file", payload: `{"path":"a.txt"}`},
		{kind: "tool_result", toolName: "read_file", payload: "file A"},
		{kind: "thinking"},
		{kind: "message", payload: "done"},
	}
	if got := reporter.Events(); !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %#v, want %#v", got, want)
	}
}
