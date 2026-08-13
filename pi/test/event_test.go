package test

import (
	"encoding/json"
	"testing"

	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
)

func TestAgentEventConstructorsSetDiscriminatedPayloads(t *testing.T) {
	call := ai.ToolCall{ID: "call-1", Name: "exec", Arguments: json.RawMessage(`{"command":"pwd"}`)}
	start := pi.NewToolStartEvent(call)
	if start.Type != pi.AgentEventToolStart || start.Tool == nil || start.Tool.Phase != pi.ToolEventStart {
		t.Fatalf("start = %#v", start)
	}

	message := pi.NewMessageEvent(ai.Message{
		Role:    ai.RoleAssistant,
		Content: []ai.ContentBlock{ai.TextBlock("done")},
	})
	if message.Type != pi.AgentEventMessage || message.Message == nil || message.Tool != nil {
		t.Fatalf("message = %#v", message)
	}
}

func TestTextContentRejectsUnknownBlockTypes(t *testing.T) {
	_, err := ai.TextContent([]ai.ContentBlock{{Type: ai.ContentType("image")}})
	if err == nil {
		t.Fatal("TextContent() error = nil")
	}
}
