package agent

import (
	"errors"
	"testing"

	"github.com/PycMono/go-reagent/ai"
)

func TestValidateRunRequestRejectsInvalidStructuredInput(t *testing.T) {
	validInput := ai.Message{Role: ai.RoleUser, Content: []ai.ContentBlock{ai.TextBlock("hello")}}
	tests := []struct {
		name    string
		request RunRequest
	}{
		{
			name: "non-user input",
			request: RunRequest{Input: ai.Message{
				Role:    ai.RoleAssistant,
				Content: []ai.ContentBlock{ai.TextBlock("hello")},
			}},
		},
		{name: "empty input", request: RunRequest{Input: ai.Message{Role: ai.RoleUser}}},
		{
			name: "input tool calls",
			request: RunRequest{Input: ai.Message{
				Role:      ai.RoleUser,
				Content:   []ai.ContentBlock{ai.TextBlock("hello")},
				ToolCalls: []ai.ToolCall{{ID: "call-1", Name: "read"}},
			}},
		},
		{
			name: "input tool result fields",
			request: RunRequest{Input: ai.Message{
				Role:       ai.RoleUser,
				Content:    []ai.ContentBlock{ai.TextBlock("hello")},
				ToolCallID: "call-1",
				ToolName:   "read",
			}},
		},
		{name: "empty context name", request: RunRequest{Input: validInput, Context: []ContextBlock{{Content: "value"}}}},
		{name: "empty context content", request: RunRequest{Input: validInput, Context: []ContextBlock{{Name: "profile"}}}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateRunRequest(test.request); !errors.Is(err, ErrRequestInvalid) {
				t.Fatalf("validateRunRequest() error = %v, want ErrRequestInvalid", err)
			}
		})
	}
}
