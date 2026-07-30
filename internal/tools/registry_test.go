package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	logsdk "github.com/PycMono/go-logger-sdk"
	"github.com/PycMono/go-reagent/internal/logtest"
	"github.com/PycMono/go-reagent/internal/schema"
)

type stubTool struct {
	name           string
	definitionName string
	output         string
	err            error
	panicOnExecute bool
	mu             sync.Mutex
	received       json.RawMessage
	calls          atomic.Int32
}

func (t *stubTool) Name() string {
	return t.name
}

func (t *stubTool) Definition() schema.ToolDefinition {
	name := t.definitionName
	if name == "" {
		name = t.name
	}
	return schema.ToolDefinition{
		Name:        name,
		Description: "stub tool",
		InputSchema: map[string]any{"type": "object"},
	}
}

func (t *stubTool) Execute(_ context.Context, args json.RawMessage) (string, error) {
	t.calls.Add(1)
	t.mu.Lock()
	t.received = append(json.RawMessage(nil), args...)
	t.mu.Unlock()
	if t.panicOnExecute {
		panic("sensitive panic value")
	}
	return t.output, t.err
}

func TestRegistryRegistersAndSortsDefinitions(t *testing.T) {
	registry := NewRegistry()
	for _, name := range []string{"zeta", "alpha"} {
		if err := registry.Register(&stubTool{name: name}); err != nil {
			t.Fatalf("Register(%q) error = %v", name, err)
		}
	}

	definitions := registry.GetAvailableTools()
	if len(definitions) != 2 || definitions[0].Name != "alpha" || definitions[1].Name != "zeta" {
		t.Fatalf("definitions = %#v", definitions)
	}
}

func TestRegistryRejectsInvalidRegistrations(t *testing.T) {
	var typedNil *stubTool
	tests := []struct {
		name string
		tool BaseTool
		want string
	}{
		{name: "nil", tool: nil, want: "nil"},
		{name: "typed nil", tool: typedNil, want: "nil"},
		{name: "blank name", tool: &stubTool{name: " "}, want: "name"},
		{name: "definition mismatch", tool: &stubTool{name: "route", definitionName: "schema"}, want: "must match"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewRegistry().Register(tt.tool)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Register() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestRegistryRejectsDuplicateWithoutOverwriting(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(&stubTool{name: "echo", output: "first"}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(&stubTool{name: "echo", output: "second"}); err == nil {
		t.Fatal("duplicate Register() error = nil")
	}

	result := registry.Execute(context.Background(), schema.ToolCall{ID: "call-1", Name: "echo"})
	if result.IsError || result.Output != "first" {
		t.Fatalf("result = %#v", result)
	}
}

func TestRegistryExecutesRegisteredTool(t *testing.T) {
	tool := &stubTool{name: "echo", output: "ok"}
	registry := NewRegistry()
	if err := registry.Register(tool); err != nil {
		t.Fatal(err)
	}
	call := schema.ToolCall{
		ID:        "call-1",
		Name:      "echo",
		Arguments: json.RawMessage(`{"text":"hi"}`),
	}

	result := registry.Execute(context.Background(), call)
	if result.IsError || result.ToolCallID != call.ID || result.Output != "ok" {
		t.Fatalf("result = %#v", result)
	}
	tool.mu.Lock()
	received := append(json.RawMessage(nil), tool.received...)
	tool.mu.Unlock()
	if string(received) != string(call.Arguments) {
		t.Fatalf("args = %s, want %s", received, call.Arguments)
	}
}

func TestRegistryReturnsExecutionErrors(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(&stubTool{name: "broken", err: errors.New("disk failed")}); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		call schema.ToolCall
		want string
	}{
		{name: "unknown", call: schema.ToolCall{ID: "call-unknown", Name: "missing"}, want: "not registered"},
		{name: "tool error", call: schema.ToolCall{ID: "call-broken", Name: "broken"}, want: "disk failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := registry.Execute(context.Background(), tt.call)
			if !result.IsError || result.ToolCallID != tt.call.ID || !strings.Contains(result.Output, tt.want) {
				t.Fatalf("result = %#v", result)
			}
		})
	}
}

func TestRegistryHonorsCanceledAndNilContexts(t *testing.T) {
	tool := &stubTool{name: "echo", output: "ok"}
	registry := NewRegistry()
	if err := registry.Register(tool); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	for name, ctx := range map[string]context.Context{"canceled": ctx, "nil": nil} {
		t.Run(name, func(t *testing.T) {
			result := registry.Execute(ctx, schema.ToolCall{ID: "call-" + name, Name: "echo"})
			if !result.IsError || result.ToolCallID != "call-"+name {
				t.Fatalf("result = %#v", result)
			}
		})
	}
	if calls := tool.calls.Load(); calls != 0 {
		t.Fatalf("tool calls = %d, want 0", calls)
	}
}

func TestRegistryRecoversToolPanicWithoutExposingValue(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(&stubTool{name: "panic", panicOnExecute: true}); err != nil {
		t.Fatal(err)
	}

	result := registry.Execute(context.Background(), schema.ToolCall{ID: "call-panic", Name: "panic"})
	if !result.IsError || result.ToolCallID != "call-panic" || !strings.Contains(result.Output, "panicked") {
		t.Fatalf("result = %#v", result)
	}
	if strings.Contains(result.Output, "sensitive panic value") {
		t.Fatalf("panic value leaked in result: %q", result.Output)
	}
}

func TestRegistryEmitsStructuredRegistrationAndPanicLogs(t *testing.T) {
	recorder := &logtest.Recorder{}
	logsdk.SetLogger(recorder)
	t.Cleanup(func() {
		logsdk.SetLogger(logsdk.NewLogrus(logsdk.Options{LogFormat: "json", Module: "go-reagent"}))
	})

	registry := NewRegistry()
	if err := registry.Register(&stubTool{name: "panic", panicOnExecute: true}); err != nil {
		t.Fatal(err)
	}
	_ = registry.Execute(context.Background(), schema.ToolCall{ID: "call-panic", Name: "panic"})

	registered, ok := recorder.Find("info", "[Registry] 成功挂载工具")
	if !ok || registered.Fields["component"] != "registry" || registered.Fields["tool"] != "panic" {
		t.Fatalf("registration event = %#v, found = %v", registered, ok)
	}
	panicked, ok := recorder.Find("error", "工具执行 panic")
	if !ok || panicked.Fields["component"] != "registry" || panicked.Fields["tool"] != "panic" || panicked.Fields["stack"] == nil {
		t.Fatalf("panic event = %#v, found = %v", panicked, ok)
	}
}

func TestRegistrySupportsConcurrentDiscoveryAndExecution(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(&stubTool{name: "echo", output: "ok"}); err != nil {
		t.Fatal(err)
	}

	var waitGroup sync.WaitGroup
	for index := range 50 {
		waitGroup.Add(2)
		go func() {
			defer waitGroup.Done()
			_ = registry.GetAvailableTools()
		}()
		go func(index int) {
			defer waitGroup.Done()
			result := registry.Execute(context.Background(), schema.ToolCall{
				ID:   fmt.Sprintf("call-%d", index),
				Name: "echo",
			})
			if result.IsError {
				t.Errorf("Execute() result = %#v", result)
			}
		}(index)
	}
	waitGroup.Wait()
}
