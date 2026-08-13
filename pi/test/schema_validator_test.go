package test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
)

func TestCompileSchemaValidatorRejectsMalformedSchema(t *testing.T) {
	tool := &stubTool{
		definition: ai.ToolDefinition{
			Name:        "broken",
			InputSchema: map[string]any{"type": "definitely-not-a-json-schema-type"},
		},
		execute: func(context.Context, json.RawMessage, ai.UpdateEmitter) (ai.ToolOutput, error) {
			return ai.ToolOutput{}, nil
		},
	}
	_, err := pi.NewToolRuntime(pi.ToolRuntimeOptions{Tools: []ai.Tool{tool}})
	if err == nil || !strings.Contains(err.Error(), "broken") {
		t.Fatalf("pi.NewToolRuntime() error = %v", err)
	}
}

func TestSchemaValidatorAcceptsJsonNumbersWithoutPrecisionLoss(t *testing.T) {
	tool := &stubTool{
		definition: ai.ToolDefinition{
			Name: "number",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"value": map[string]any{"type": "integer"},
				},
				"required":             []string{"value"},
				"additionalProperties": false,
			},
		},
		execute: func(context.Context, json.RawMessage, ai.UpdateEmitter) (ai.ToolOutput, error) {
			return ai.ToolOutput{Content: []ai.ContentBlock{ai.TextBlock("ok")}}, nil
		},
	}
	toolRuntime := newTestToolRuntime(t, pi.DefaultMiddlewareRegistrations(), tool)
	result, err := toolRuntime.Execute(context.Background(), ai.ToolCall{
		ID:        "large-integer",
		Name:      "number",
		Arguments: json.RawMessage(`{"value":9007199254740993}`),
	}, nil)
	if err != nil || result.IsError {
		t.Fatalf("ToolRuntime.Execute() = (%#v, %v)", result, err)
	}
}
