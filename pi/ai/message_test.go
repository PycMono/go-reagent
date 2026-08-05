package ai_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/pi/ai"
)

func TestMessageUsageJSONRoundTripAndOmission(t *testing.T) {
	want := ai.Message{Role: ai.RoleAssistant, Usage: &ai.Usage{
		InputTokens: 120, OutputTokens: 30,
		InputPriceUSDPerMillionTokens:  0.15,
		OutputPriceUSDPerMillionTokens: 0.60,
		CostUSD:                        0.000036, LatencyMS: 245,
		PlatformID: "zhipu", Model: "glm-4.5-air",
	}}
	encoded, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got ai.Message
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip = %#v, want %#v", got, want)
	}

	withoutUsage, err := json.Marshal(ai.Message{Role: ai.RoleAssistant})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(withoutUsage), "usage") {
		t.Fatalf("nil Usage serialized: %s", withoutUsage)
	}
}

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
