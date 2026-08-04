package mysql

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/PycMono/go-reagent/internal/conversation"
	"github.com/PycMono/go-reagent/internal/schema"
)

func TestMessageCodecRoundTripsSupportedMessages(t *testing.T) {
	messages := []schema.Message{
		{Role: schema.RoleUser, Content: []schema.ContentBlock{schema.TextBlock("hello")}},
		{Role: schema.RoleAssistant, ToolCalls: []schema.ToolCall{{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"a.txt"}`)}}},
		{Role: schema.RoleTool, ToolCallID: "call-1", ToolName: "read", IsError: true, Content: []schema.ContentBlock{schema.TextBlock("failed")}},
	}

	for _, message := range messages {
		row, err := encodeMessage(message)
		if err != nil {
			t.Fatalf("encodeMessage(%q) error = %v", message.Role, err)
		}
		decoded, err := decodeMessage(row)
		if err != nil {
			t.Fatalf("decodeMessage(%q) error = %v", message.Role, err)
		}
		if !reflect.DeepEqual(decoded, message) {
			t.Fatalf("decoded = %#v, want %#v", decoded, message)
		}
	}
}

func TestMessageCodecRejectsCorruptRows(t *testing.T) {
	userPayload, err := json.Marshal(schema.Message{Role: schema.RoleUser, Content: []schema.ContentBlock{schema.TextBlock("hello")}})
	if err != nil {
		t.Fatal(err)
	}
	systemPayload, err := json.Marshal(schema.Message{Role: schema.RoleSystem, Content: []schema.ContentBlock{schema.TextBlock("system")}})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		row  messageRow
	}{
		{name: "invalid JSON", row: messageRow{Role: string(schema.RoleUser), Payload: jsonPayload(`{"role":`)}},
		{name: "unknown role", row: messageRow{Role: string(schema.RoleSystem), Payload: jsonPayload(systemPayload)}},
		{name: "role mismatch", row: messageRow{Role: string(schema.RoleAssistant), Payload: jsonPayload(userPayload)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := decodeMessage(tt.row)
			if !errors.Is(err, conversation.ErrCorruptMessage) {
				t.Fatalf("decodeMessage() error = %v, want ErrCorruptMessage", err)
			}
		})
	}
}
