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

	messageStart := pi.NewMessageStartEvent()
	messageUpdate := pi.NewMessageUpdateEvent(ai.TextBlock("do"))
	message := pi.NewMessageEndEvent(ai.Message{
		Role:    ai.RoleAssistant,
		Content: []ai.ContentBlock{ai.TextBlock("done")},
	})
	if messageStart.Type != pi.AgentEventMessageStart || messageUpdate.Type != pi.AgentEventMessageUpdate ||
		messageUpdate.Delta == nil || messageUpdate.Delta.Text != "do" ||
		message.Type != pi.AgentEventMessageEnd || message.Message == nil || message.Tool != nil {
		t.Fatalf("message events = %#v / %#v / %#v", messageStart, messageUpdate, message)
	}
}

func TestTextContentRejectsUnknownBlockTypes(t *testing.T) {
	_, err := ai.TextContent([]ai.ContentBlock{{Type: ai.ContentType("image")}})
	if err == nil {
		t.Fatal("TextContent() error = nil")
	}
}
