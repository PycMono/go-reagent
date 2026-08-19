package providers

import (
	"encoding/json"
	"testing"

	"github.com/PycMono/go-reagent/pi/ai"
)

func TestOpenAIToolSchemaSerializesMCPJSONNumbersAsNumbers(t *testing.T) {
	tools, err := toOpenAITools([]ai.ToolDefinition{{
		Name: "web_fetch_exa",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"maxCharacters": map[string]any{
					"type":    "integer",
					"minimum": json.Number("1"),
				},
			},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(tools)
	if err != nil {
		t.Fatal(err)
	}
	var payload []struct {
		Function struct {
			Parameters map[string]any `json:"parameters"`
		} `json:"function"`
	}
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload) != 1 {
		t.Fatalf("tools = %#v", payload)
	}
	properties, ok := payload[0].Function.Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatalf("parameters = %#v", payload[0].Function.Parameters)
	}
	maxCharacters, ok := properties["maxCharacters"].(map[string]any)
	if !ok {
		t.Fatalf("properties = %#v", properties)
	}
	if minimum, ok := maxCharacters["minimum"].(float64); !ok || minimum != 1 {
		t.Fatalf("minimum = %#v (%T), want JSON number 1", maxCharacters["minimum"], maxCharacters["minimum"])
	}
}
