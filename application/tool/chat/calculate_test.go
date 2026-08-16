package chat

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"

	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
)

func TestCalculateToolEvaluatesArithmetic(t *testing.T) {
	tests := []struct {
		expression string
		want       float64
	}{
		{expression: "1+2*3", want: 7},
		{expression: "(1+2)*3", want: 9},
		{expression: "1.5*2", want: 3},
		{expression: "7%4", want: 3},
		{expression: "2**3", want: 8},
		{expression: "-2 + +3", want: 1},
		{expression: "1/4", want: 0.25},
	}
	tool := newCalculateTool()
	for _, test := range tests {
		t.Run(test.expression, func(t *testing.T) {
			output, err := tool.Execute(context.Background(), mustJSON(map[string]any{"expression": test.expression}), nil)
			if err != nil {
				t.Fatal(err)
			}
			got := decodeToolJSON[calculateResult](t, output)
			if got.Expression != test.expression || got.Result != test.want {
				t.Fatalf("result = %#v, want %g", got, test.want)
			}
		})
	}
}

func TestCalculateToolDefinitionIsStrictAndParallelSafe(t *testing.T) {
	definition := newCalculateTool().Definition()
	if definition.Name != "calculate" || definition.Description == "" || !definition.ParallelSafe {
		t.Fatalf("definition = %#v", definition)
	}
	schema := definition.InputSchema.(map[string]any)
	properties := schema["properties"].(map[string]any)
	if schema["additionalProperties"] != false || len(properties) != 1 {
		t.Fatalf("schema = %#v", schema)
	}
}

func TestCalculateToolRejectsNonArithmeticAST(t *testing.T) {
	tool := newCalculateTool()
	for _, expression := range []string{
		`now()`, `foo`, `foo.bar`, `[1, 2]`, `{"x": 1}`, `true ? 1 : 2`, `"1"`, `1 < 2`, `nil`,
	} {
		t.Run(expression, func(t *testing.T) {
			_, err := tool.Execute(context.Background(), mustJSON(map[string]any{"expression": expression}), nil)
			if pierrors.ErrorCodeOf(err) != pierrors.ErrorCodeToolInvalidArguments {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestCalculateToolRejectsInvalidOrUnsafeArithmetic(t *testing.T) {
	tests := []string{
		"", "   ", "1+", "1/0", "0/0", "10**1000",
		"9223372036854775807+1", "3037000500*3037000500", "1%0",
		strings.Repeat("1", 257), strings.Repeat("1+", 64) + "1",
	}
	tool := newCalculateTool()
	for _, expression := range tests {
		t.Run(expression[:min(len(expression), 40)], func(t *testing.T) {
			_, err := tool.Execute(context.Background(), mustJSON(map[string]any{"expression": expression}), nil)
			if pierrors.ErrorCodeOf(err) != pierrors.ErrorCodeToolInvalidArguments {
				t.Fatalf("expression = %q, error = %v", expression, err)
			}
		})
	}
}

func TestCalculateToolRejectsUnknownAndTrailingArguments(t *testing.T) {
	tool := newCalculateTool()
	for _, arguments := range []string{
		`{"expression":"1+1","unexpected":true}`,
		`{"expression":"1+1"} {}`,
		`not-json`,
	} {
		_, err := tool.Execute(context.Background(), json.RawMessage(arguments), nil)
		if pierrors.ErrorCodeOf(err) != pierrors.ErrorCodeToolInvalidArguments {
			t.Fatalf("arguments = %s, error = %v", arguments, err)
		}
	}
}

func TestCalculateToolHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := newCalculateTool().Execute(ctx, mustJSON(map[string]any{"expression": "1+1"}), nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}

func TestCalculateResultsAreFinite(t *testing.T) {
	output, err := newCalculateTool().Execute(context.Background(), mustJSON(map[string]any{"expression": "1.25+2.5"}), nil)
	if err != nil {
		t.Fatal(err)
	}
	got := decodeToolJSON[calculateResult](t, output)
	if math.IsNaN(got.Result) || math.IsInf(got.Result, 0) {
		t.Fatalf("result = %g", got.Result)
	}
}

func mustJSON(value any) json.RawMessage {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return data
}
