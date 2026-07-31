package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/internal/schema"
)

func TestCompileSchemaValidatorRejectsMalformedSchema(t *testing.T) {
	_, err := compileSchemaValidator(schema.ToolDefinition{
		Name:        "broken",
		InputSchema: map[string]any{"type": "definitely-not-a-json-schema-type"},
	})
	if err == nil || !strings.Contains(err.Error(), "broken") {
		t.Fatalf("compileSchemaValidator() error = %v", err)
	}
}

func TestSchemaValidatorAcceptsJsonNumbersWithoutPrecisionLoss(t *testing.T) {
	validate, err := compileSchemaValidator(schema.ToolDefinition{
		Name: "number",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"value": map[string]any{"type": "integer"},
			},
			"required":             []string{"value"},
			"additionalProperties": false,
		},
	})
	if err != nil {
		t.Fatalf("compileSchemaValidator() error = %v", err)
	}
	if err := validate(json.RawMessage(`{"value":9007199254740993}`)); err != nil {
		t.Fatalf("validate() error = %v", err)
	}
}
