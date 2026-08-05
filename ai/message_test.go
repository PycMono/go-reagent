package ai_test

import (
	"encoding/json"
	"testing"

	"github.com/PycMono/go-reagent/ai"
)

func TestMessageRoundTripPreservesToolArguments(t *testing.T) {
	want := ai.Message{
		Role: ai.RoleAssistant,
		ToolCalls: []ai.ToolCall{{
			ID:        "call-1",
			Name:      "read",
			Arguments: json.RawMessage(`{"path":"AGENTS.md"}`),
		}},
	}
	encoded, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got ai.Message
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	if string(got.ToolCalls[0].Arguments) != `{"path":"AGENTS.md"}` {
		t.Fatalf("arguments = %s", got.ToolCalls[0].Arguments)
	}
}

func TestProtocolValuesAreStable(t *testing.T) {
	if ai.ProtocolOpenAI != "openai" || ai.ProtocolAnthropic != "anthropic" {
		t.Fatalf("protocols = %q, %q", ai.ProtocolOpenAI, ai.ProtocolAnthropic)
	}
}
