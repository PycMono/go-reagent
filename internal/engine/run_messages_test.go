package engine_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	ctxpkg "github.com/PycMono/go-reagent/internal/context"
	"github.com/PycMono/go-reagent/internal/engine"
	"github.com/PycMono/go-reagent/internal/schema"
)

func TestAgentLoopReturnsDirectAssistantIncrement(t *testing.T) {
	provider := &fakeProvider{responses: []*schema.Message{{
		Role:    schema.RoleAssistant,
		Content: blocks("done"),
	}}}
	registry := &fakeRegistry{}
	loop := engine.NewAgentLoop(provider, engine.NewToolScheduler(registry, 1), false)
	runContext := ctxpkg.RunContext{Messages: []schema.Message{
		{Role: schema.RoleSystem, Content: blocks("system")},
		{Role: schema.RoleUser, Content: blocks("hello")},
	}}

	newMessages, err := loop.Run(context.Background(), runContext, nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(newMessages) != 1 || newMessages[0].Role != schema.RoleAssistant || messageText(t, newMessages[0]) != "done" {
		t.Fatalf("NewMessages = %#v, want one assistant message containing done", newMessages)
	}
}

func TestAgentLoopReturnsToolConversationInOrder(t *testing.T) {
	call := schema.ToolCall{ID: "call-1", Name: "echo", Arguments: json.RawMessage(`{"text":"hello"}`)}
	provider := &fakeProvider{responses: []*schema.Message{
		{Role: schema.RoleAssistant, ToolCalls: []schema.ToolCall{call}},
		{Role: schema.RoleAssistant, Content: blocks("finished")},
	}}
	registry := &fakeRegistry{
		definitions: []schema.ToolDefinition{{Name: "echo"}},
		results: map[string]schema.ToolResult{
			"echo": toolResult(call, "hello", false),
		},
	}
	loop := engine.NewAgentLoop(provider, engine.NewToolScheduler(registry, 1), false)
	runContext := ctxpkg.RunContext{
		Messages: []schema.Message{{Role: schema.RoleUser, Content: blocks("run echo")}},
		Tools:    registry.definitions,
	}

	newMessages, err := loop.Run(context.Background(), runContext, nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(newMessages) != 3 {
		t.Fatalf("NewMessages count = %d, want 3: %#v", len(newMessages), newMessages)
	}
	if newMessages[0].Role != schema.RoleAssistant || len(newMessages[0].ToolCalls) != 1 || newMessages[0].ToolCalls[0].ID != "call-1" {
		t.Fatalf("NewMessages[0] = %#v, want assistant tool call", newMessages[0])
	}
	if newMessages[1].Role != schema.RoleTool || newMessages[1].ToolCallID != "call-1" || messageText(t, newMessages[1]) != "hello" {
		t.Fatalf("NewMessages[1] = %#v, want matching tool result", newMessages[1])
	}
	if newMessages[2].Role != schema.RoleAssistant || messageText(t, newMessages[2]) != "finished" {
		t.Fatalf("NewMessages[2] = %#v, want final assistant message", newMessages[2])
	}
}

func TestAgentLoopExcludesThinkingScaffoldingFromIncrement(t *testing.T) {
	provider := &fakeProvider{responses: []*schema.Message{
		{Role: schema.RoleAssistant, Content: blocks("internal plan")},
		{Role: schema.RoleAssistant, Content: blocks("done")},
	}}
	registry := &fakeRegistry{}
	loop := engine.NewAgentLoop(provider, engine.NewToolScheduler(registry, 1), true)
	runContext := ctxpkg.RunContext{Messages: []schema.Message{
		{Role: schema.RoleUser, Content: blocks("hello")},
	}}

	newMessages, err := loop.Run(context.Background(), runContext, nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(newMessages) != 1 || messageText(t, newMessages[0]) != "done" {
		t.Fatalf("NewMessages = %#v, want only final assistant message", newMessages)
	}
}

func TestAgentLoopReturnsCompletedMessagesWithProviderError(t *testing.T) {
	call := schema.ToolCall{ID: "call-1", Name: "echo", Arguments: json.RawMessage(`{}`)}
	provider := &fakeProvider{responses: []*schema.Message{{
		Role:      schema.RoleAssistant,
		ToolCalls: []schema.ToolCall{call},
	}}}
	registry := &fakeRegistry{
		definitions: []schema.ToolDefinition{{Name: "echo"}},
		results: map[string]schema.ToolResult{
			"echo": toolResult(call, "completed before failure", false),
		},
	}
	loop := engine.NewAgentLoop(provider, engine.NewToolScheduler(registry, 1), false)
	runContext := ctxpkg.RunContext{
		Messages: []schema.Message{{Role: schema.RoleUser, Content: blocks("run echo")}},
		Tools:    registry.definitions,
	}

	newMessages, err := loop.Run(context.Background(), runContext, nil)
	if err == nil || !strings.Contains(err.Error(), "unexpected provider call 2") {
		t.Fatalf("Run() error = %v, want second provider call failure", err)
	}
	if len(newMessages) != 2 || newMessages[0].Role != schema.RoleAssistant || newMessages[1].Role != schema.RoleTool {
		t.Fatalf("NewMessages = %#v, want completed assistant tool call and tool result", newMessages)
	}
	if got := messageText(t, newMessages[1]); got != "completed before failure" {
		t.Fatalf("tool result = %q, want completed before failure", got)
	}
}
