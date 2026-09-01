package ai

import (
	"encoding/json"
	"testing"
)

func TestToolDefinitionInputSchemaObjectNormalizesNumbers(t *testing.T) {
	definition := ToolDefinition{
		Name: "web_fetch",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"maxCharacters": map[string]any{
					"type":    "integer",
					"minimum": json.Number("1"),
				},
			},
		},
	}

	schema, err := definition.InputSchemaObject()
	if err != nil {
		t.Fatal(err)
	}
	properties := schema["properties"].(map[string]any)
	maxCharacters := properties["maxCharacters"].(map[string]any)
	if minimum, ok := maxCharacters["minimum"].(int64); !ok || minimum != 1 {
		t.Fatalf("minimum = %#v (%T), want int64(1)", maxCharacters["minimum"], maxCharacters["minimum"])
	}
}

func TestToolDefinitionInputSchemaObjectRejectsNonObjectSchema(t *testing.T) {
	definition := ToolDefinition{Name: "lookup", InputSchema: map[string]any{"type": "string"}}
	if _, err := definition.InputSchemaObject(); err == nil {
		t.Fatal("non-object tool input schema must be rejected")
	}
}
