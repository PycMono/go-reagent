package engine_test

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"sync"
	"testing"

	"github.com/PycMono/go-reagent/ai"
	"github.com/PycMono/go-reagent/internal/engine"
	"github.com/PycMono/go-reagent/internal/schema"
)

type recordingReporter struct {
	mu     sync.Mutex
	events []schema.AgentEvent
}

func (r *recordingReporter) Report(_ context.Context, event schema.AgentEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

func (r *recordingReporter) Events() []schema.AgentEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]schema.AgentEvent(nil), r.events...)
}

type reporterFunc func(context.Context, schema.AgentEvent)

func (f reporterFunc) Report(ctx context.Context, event schema.AgentEvent) { f(ctx, event) }

func TestMultiReporterSortsAndIsolatesPanic(t *testing.T) {
	var got []string
	reporter := engine.NewMultiReporter([]engine.ReporterRegistration{
		{Name: "z", Order: 20, Reporter: reporterFunc(func(context.Context, schema.AgentEvent) { got = append(got, "z") })},
		{Name: "panic", Order: 10, Reporter: reporterFunc(func(context.Context, schema.AgentEvent) { panic("boom") })},
		{Name: "a", Order: 20, Reporter: reporterFunc(func(context.Context, schema.AgentEvent) { got = append(got, "a") })},
	})

	reporter.Report(context.Background(), schema.NewThinkingEvent())
	if want := []string{"a", "z"}; !slices.Equal(got, want) {
		t.Fatalf("reported to = %v, want %v", got, want)
	}
}

func TestAgentLoopReportsEveryLifecycleEventWithoutAggregation(t *testing.T) {
	provider := &fakeProvider{responses: []*ai.Message{
		{Role: ai.RoleAssistant, Content: blocks("plan one")},
		{
			Role:    ai.RoleAssistant,
			Content: blocks("starting tool"),
			ToolCalls: []ai.ToolCall{{
				ID:        "call-1",
				Name:      "read",
				Arguments: json.RawMessage(`{"path":"a.txt"}`),
			}},
		},
		{Role: ai.RoleAssistant, Content: blocks("plan two")},
		{Role: ai.RoleAssistant, Content: blocks("done")},
	}}
	registry := &fakeRegistry{
		definitions: []ai.ToolDefinition{{Name: "read"}},
		results: map[string]schema.ToolResult{
			"read": toolResult(ai.ToolCall{ID: "call-1", Name: "read"}, "file A", false),
		},
	}
	reporter := &recordingReporter{}
	agentEngine := newAgentLoopForTest(provider, registry, t.TempDir(), true)

	if err := agentEngine.Run(context.Background(), "read a", reporter); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	call := ai.ToolCall{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"a.txt"}`)}
	result := toolResult(call, "file A", false)
	want := []schema.AgentEvent{
		schema.NewThinkingEvent(),
		schema.NewToolStartEvent(call),
		schema.NewToolEndEvent(call, result),
		schema.NewThinkingEvent(),
		schema.NewMessageEvent(ai.Message{Role: ai.RoleAssistant, Content: blocks("done")}),
	}
	if got := reporter.Events(); !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %#v, want %#v", got, want)
	}
}
