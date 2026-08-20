package providers

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/pi/ai"
)

func TestProviderMessageConvertersRejectInvalidToolCallArguments(t *testing.T) {
	messages := []ai.Message{{
		Role: ai.RoleAssistant,
		ToolCalls: []ai.ToolCall{{
			ID: "call-1", Name: "lookup", Arguments: json.RawMessage(`{"broken"`),
		}},
	}}

	tests := []struct {
		name    string
		convert func() error
	}{
		{
			name: "OpenAI",
			convert: func() error {
				_, err := toOpenAIMessages(messages)
				return err
			},
		},
		{
			name: "Anthropic",
			convert: func() error {
				_, _, err := toAnthropicMessages(messages)
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.convert()
			if err == nil || !strings.Contains(err.Error(), `tool call "call-1" arguments`) {
				t.Fatalf("convert() error = %v, want invalid tool call arguments", err)
			}
		})
	}
}

func TestProviderToolConvertersRejectNonObjectInputSchema(t *testing.T) {
	definitions := []ai.ToolDefinition{{
		Name: "lookup", InputSchema: map[string]any{"type": "string"},
	}}

	tests := []struct {
		name    string
		convert func() error
	}{
		{
			name: "OpenAI",
			convert: func() error {
				_, err := toOpenAITools(definitions)
				return err
			},
		},
		{
			name: "Anthropic",
			convert: func() error {
				_, err := toAnthropicTools(definitions)
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.convert()
			if err == nil || !strings.Contains(err.Error(), `tool "lookup" input schema type must be object`) {
				t.Fatalf("convert() error = %v, want non-object schema error", err)
			}
		})
	}
}

func TestProviderConversionFailureStreamEmitsStartThenError(t *testing.T) {
	messages := []ai.Message{{Role: ai.Role("unsupported")}}
	tests := []struct {
		name   string
		stream ai.Stream
	}{
		{name: "OpenAI", stream: (&OpenAIImpl{name: "test"}).Stream(context.Background(), messages, nil)},
		{name: "Anthropic", stream: (&AnthropicImpl{name: "test"}).Stream(context.Background(), messages, nil)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var events []ai.StreamEventType
			for test.stream.Next() {
				events = append(events, test.stream.Current().Type)
			}
			if len(events) != 2 || events[0] != ai.StreamEventStart || events[1] != ai.StreamEventError {
				t.Fatalf("events = %v, want [start error]", events)
			}
			message, err := test.stream.Result()
			if message != nil || err == nil {
				t.Fatalf("Result() = (%#v, %v), want (nil, error)", message, err)
			}
			if err := test.stream.Close(); err != nil {
				t.Fatalf("Close() error = %v", err)
			}
		})
	}
}
