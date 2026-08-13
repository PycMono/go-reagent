package test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
)

type echoTool struct{}

func (echoTool) Definition() ai.ToolDefinition {
	return ai.ToolDefinition{
		Name:        "echo",
		Description: "echo text",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{"type": "string"},
			},
			"required": []string{"text"},
		},
	}
}

func (echoTool) Execute(_ context.Context, raw json.RawMessage, _ ai.UpdateEmitter) (ai.ToolOutput, error) {
	return ai.ToolOutput{Content: []ai.ContentBlock{ai.TextBlock(string(raw))}}, nil
}

func TestToolRuntimeValidatesArgumentsWithJSONSchema(t *testing.T) {
	toolRuntime, err := pi.NewToolRuntime(pi.ToolRuntimeOptions{
		Tools:       []ai.Tool{echoTool{}},
		Middlewares: pi.DefaultMiddlewareRegistrations(),
	})
	if err != nil {
		t.Fatalf("NewToolRuntime error = %v", err)
	}
	result, err := toolRuntime.Execute(context.Background(), ai.ToolCall{
		ID: "1", Name: "echo", Arguments: []byte(`{}`),
	}, nil)
	if err != nil || !result.IsError {
		t.Fatalf("result/error = %#v, %v", result, err)
	}
}
