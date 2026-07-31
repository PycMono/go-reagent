package schema_test

import (
	"encoding/json"
	"testing"

	"github.com/PycMono/go-reagent/internal/schema"
)

func TestAgentEventConstructorsSetDiscriminatedPayloads(t *testing.T) {
	call := schema.ToolCall{ID: "call-1", Name: "exec", Arguments: json.RawMessage(`{"command":"pwd"}`)}
	start := schema.NewToolStartEvent(call)
	if start.Type != schema.AgentEventToolStart || start.Tool == nil || start.Tool.Phase != schema.ToolEventStart {
		t.Fatalf("start = %#v", start)
	}

	message := schema.NewMessageEvent(schema.Message{
		Role:    schema.RoleAssistant,
		Content: []schema.ContentBlock{schema.TextBlock("done")},
	})
	if message.Type != schema.AgentEventMessage || message.Message == nil || message.Tool != nil {
		t.Fatalf("message = %#v", message)
	}
}

func TestTextContentRejectsUnknownBlockTypes(t *testing.T) {
	_, err := schema.TextContent([]schema.ContentBlock{{Type: schema.ContentType("image")}})
	if err == nil {
		t.Fatal("TextContent() error = nil")
	}
}
