package agent_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/PycMono/go-reagent/pi/agent"
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

func (echoTool) Execute(_ context.Context, raw json.RawMessage, _ agent.UpdateEmitter) (agent.ToolOutput, error) {
	return agent.ToolOutput{Content: []ai.ContentBlock{ai.TextBlock(string(raw))}}, nil
}

func TestRegistryValidatesArgumentsWithJSONSchema(t *testing.T) {
	registry, err := agent.NewRegistry(agent.RegistryOptions{
		Tools:       []agent.Tool{echoTool{}},
		Middlewares: agent.DefaultMiddlewareRegistrations(),
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := registry.Execute(context.Background(), ai.ToolCall{
		ID: "1", Name: "echo", Arguments: []byte(`{}`),
	}, nil)
	if err != nil || !result.IsError {
		t.Fatalf("result/error = %#v, %v", result, err)
	}
}
