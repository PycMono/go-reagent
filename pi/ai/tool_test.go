package ai

import (
	"encoding/json"
	"reflect"
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

func TestToolOutputLimitTextUsesSharedUTF8ByteBudget(t *testing.T) {
	details := map[string]any{"source": "tool"}
	original := ToolOutput{
		Content: ContentBlocks{TextBlock("ab"), TextBlock("你c")},
		Details: details,
	}

	limited := original.LimitText(5)

	wantContent := ContentBlocks{
		TextBlock("ab"),
		TextBlock("你"),
		TextBlock("\n[output truncated]"),
	}
	if !reflect.DeepEqual(limited.Content, wantContent) {
		t.Fatalf("LimitText() content = %#v, want %#v", limited.Content, wantContent)
	}
	if got := limited.Details; !reflect.DeepEqual(got, map[string]any{"source": "tool", "truncated": true}) {
		t.Fatalf("LimitText() details = %#v", got)
	}
	if !reflect.DeepEqual(original.Content, ContentBlocks{TextBlock("ab"), TextBlock("你c")}) {
		t.Fatalf("LimitText() mutated original content: %#v", original.Content)
	}
	if _, exists := details["truncated"]; exists {
		t.Fatalf("LimitText() mutated original details: %#v", details)
	}
}

func TestToolOutputLimitTextDoesNotChargeNonTextBlocks(t *testing.T) {
	original := ToolOutput{Content: ContentBlocks{
		ImageBlock("https://example.com/image.png"),
		TextBlock("ab"),
	}}

	limited := original.LimitText(1)

	if len(limited.Content) != 3 || limited.Content[0].Type != ContentTypeImage ||
		limited.Content[1].Text != "a" || limited.Content[2].Text != "\n[output truncated]" {
		t.Fatalf("LimitText() content = %#v", limited.Content)
	}
}

func TestToolOutputLimitTextPreservesOpaqueDetailsWhenTruncated(t *testing.T) {
	original := ToolOutput{Content: ContentBlocks{TextBlock("ab")}, Details: "opaque"}

	limited := original.LimitText(1)

	want := map[string]any{"tool_details": "opaque", "truncated": true}
	if !reflect.DeepEqual(limited.Details, want) {
		t.Fatalf("LimitText() details = %#v, want %#v", limited.Details, want)
	}
}

func TestToolOutputLimitTextSanitizesInvalidUTF8WithoutTruncation(t *testing.T) {
	original := ToolOutput{Content: ContentBlocks{TextBlock(string([]byte{'a', 0xff, 'b'}))}}

	limited := original.LimitText(10)

	if len(limited.Content) != 1 || limited.Content[0].Text != "a�b" {
		t.Fatalf("LimitText() content = %#v", limited.Content)
	}
	if limited.Details != nil {
		t.Fatalf("LimitText() details = %#v, want nil", limited.Details)
	}
}
