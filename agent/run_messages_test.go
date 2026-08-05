package agent_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/agent"
	"github.com/PycMono/go-reagent/ai"
)

func TestAgentLoopReturnsDirectAssistantIncrement(t *testing.T) {
	provider := &fakeProvider{responses: []*ai.Message{{
		Role:    ai.RoleAssistant,
		Content: blocks("done"),
	}}}
	registry := &fakeRegistry{}
	loop := agent.NewLoop(provider, agent.NewScheduler(registry, 1), false)
	runContext := agent.RunContext{Messages: []ai.Message{
		{Role: ai.RoleSystem, Content: blocks("system")},
		{Role: ai.RoleUser, Content: blocks("hello")},
	}}

	newMessages, err := loop.Run(context.Background(), runContext, nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(newMessages) != 1 || newMessages[0].Role != ai.RoleAssistant || messageText(t, newMessages[0]) != "done" {
		t.Fatalf("NewMessages = %#v, want one assistant message containing done", newMessages)
	}
}

func TestAgentLoopReturnsToolConversationInOrder(t *testing.T) {
	call := ai.ToolCall{ID: "call-1", Name: "echo", Arguments: json.RawMessage(`{"text":"hello"}`)}
	provider := &fakeProvider{responses: []*ai.Message{
		{Role: ai.RoleAssistant, ToolCalls: []ai.ToolCall{call}},
		{Role: ai.RoleAssistant, Content: blocks("finished")},
	}}
	registry := &fakeRegistry{
		definitions: []ai.ToolDefinition{{Name: "echo"}},
		results: map[string]agent.ToolResult{
			"echo": toolResult(call, "hello", false),
		},
	}
	loop := agent.NewLoop(provider, agent.NewScheduler(registry, 1), false)
	runContext := agent.RunContext{
		Messages: []ai.Message{{Role: ai.RoleUser, Content: blocks("run echo")}},
		Tools:    registry.definitions,
	}

	newMessages, err := loop.Run(context.Background(), runContext, nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(newMessages) != 3 {
		t.Fatalf("NewMessages count = %d, want 3: %#v", len(newMessages), newMessages)
	}
	if newMessages[0].Role != ai.RoleAssistant || len(newMessages[0].ToolCalls) != 1 || newMessages[0].ToolCalls[0].ID != "call-1" {
		t.Fatalf("NewMessages[0] = %#v, want assistant tool call", newMessages[0])
	}
	if newMessages[1].Role != ai.RoleTool || newMessages[1].ToolCallID != "call-1" || messageText(t, newMessages[1]) != "hello" {
		t.Fatalf("NewMessages[1] = %#v, want matching tool result", newMessages[1])
	}
	if newMessages[2].Role != ai.RoleAssistant || messageText(t, newMessages[2]) != "finished" {
		t.Fatalf("NewMessages[2] = %#v, want final assistant message", newMessages[2])
	}
}

func TestAgentLoopExcludesThinkingScaffoldingFromIncrement(t *testing.T) {
	provider := &fakeProvider{responses: []*ai.Message{
		{Role: ai.RoleAssistant, Content: blocks("internal plan")},
		{Role: ai.RoleAssistant, Content: blocks("done")},
	}}
	registry := &fakeRegistry{}
	loop := agent.NewLoop(provider, agent.NewScheduler(registry, 1), true)
	runContext := agent.RunContext{Messages: []ai.Message{
		{Role: ai.RoleUser, Content: blocks("hello")},
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
	call := ai.ToolCall{ID: "call-1", Name: "echo", Arguments: json.RawMessage(`{}`)}
	provider := &fakeProvider{responses: []*ai.Message{{
		Role:      ai.RoleAssistant,
		ToolCalls: []ai.ToolCall{call},
	}}}
	registry := &fakeRegistry{
		definitions: []ai.ToolDefinition{{Name: "echo"}},
		results: map[string]agent.ToolResult{
			"echo": toolResult(call, "completed before failure", false),
		},
	}
	loop := agent.NewLoop(provider, agent.NewScheduler(registry, 1), false)
	runContext := agent.RunContext{
		Messages: []ai.Message{{Role: ai.RoleUser, Content: blocks("run echo")}},
		Tools:    registry.definitions,
	}

	newMessages, err := loop.Run(context.Background(), runContext, nil)
	if err == nil || !strings.Contains(err.Error(), "unexpected provider call 2") {
		t.Fatalf("Run() error = %v, want second provider call failure", err)
	}
	if len(newMessages) != 2 || newMessages[0].Role != ai.RoleAssistant || newMessages[1].Role != ai.RoleTool {
		t.Fatalf("NewMessages = %#v, want completed assistant tool call and tool result", newMessages)
	}
	if got := messageText(t, newMessages[1]); got != "completed before failure" {
		t.Fatalf("tool result = %q, want completed before failure", got)
	}
}
