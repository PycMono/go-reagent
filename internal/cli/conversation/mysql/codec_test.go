package mysql

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/PycMono/go-reagent/internal/cli/conversation"
	"github.com/PycMono/go-reagent/pi/ai"
)

func TestMessageCodecRoundTripsSupportedMessages(t *testing.T) {
	messages := []ai.Message{
		{Role: ai.RoleUser, Content: []ai.ContentBlock{ai.TextBlock("hello")}},
		{Role: ai.RoleAssistant, ToolCalls: []ai.ToolCall{{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"a.txt"}`)}}, Usage: &ai.Usage{PlatformID: "test", Model: "model"}},
		{Role: ai.RoleTool, ToolCallID: "call-1", ToolName: "read", IsError: true, Content: []ai.ContentBlock{ai.TextBlock("failed")}},
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

func TestMessageCodecDecodesLegacyPayloadWithoutUsage(t *testing.T) {
	message, err := decodeMessage(messageRow{
		Role:    string(ai.RoleAssistant),
		Payload: jsonPayload(`{"role":"assistant","content":[{"type":"text","text":"legacy"}]}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if message.Usage != nil || len(message.Content) != 1 || message.Content[0].Text != "legacy" {
		t.Fatalf("decoded legacy message = %#v", message)
	}
}

func TestMessageCodecRejectsCorruptRows(t *testing.T) {
	userPayload, err := json.Marshal(ai.Message{Role: ai.RoleUser, Content: []ai.ContentBlock{ai.TextBlock("hello")}})
	if err != nil {
		t.Fatal(err)
	}
	systemPayload, err := json.Marshal(ai.Message{Role: ai.RoleSystem, Content: []ai.ContentBlock{ai.TextBlock("system")}})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		row  messageRow
	}{
		{name: "invalid JSON", row: messageRow{Role: string(ai.RoleUser), Payload: jsonPayload(`{"role":`)}},
		{name: "unknown role", row: messageRow{Role: string(ai.RoleSystem), Payload: jsonPayload(systemPayload)}},
		{name: "role mismatch", row: messageRow{Role: string(ai.RoleAssistant), Payload: jsonPayload(userPayload)}},
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
