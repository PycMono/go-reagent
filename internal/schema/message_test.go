package schema_test

import (
	"encoding/json"
	"testing"

	"github.com/PycMono/go-reagent/internal/schema"
)

func TestToolCallArgumentsRemainJSON(t *testing.T) {
	message := schema.Message{
		Role: schema.RoleAssistant,
		ToolCalls: []schema.ToolCall{
			{
				ID:        "call-1",
				Name:      "bash",
				Arguments: json.RawMessage(`{"command":"pwd"}`),
			},
		},
	}

	data, err := json.Marshal(message)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	toolCalls := decoded["tool_calls"].([]any)
	arguments := toolCalls[0].(map[string]any)["arguments"]
	if _, ok := arguments.(map[string]any); !ok {
		t.Fatalf("arguments type = %T, want JSON object", arguments)
	}
}

func TestToolProtocolTypesExposeHarnessMetadata(t *testing.T) {
	result := schema.ToolResult{
		ToolCallID: "call-1",
		ToolName:   "bash",
		Content:    []schema.ContentBlock{schema.TextBlock("ok")},
		IsError:    false,
	}
	definition := schema.ToolDefinition{
		Name:        "bash",
		Label:       "Read file",
		Description: "execute a command",
		InputSchema: map[string]any{"type": "object"},
	}

	resultJSON, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal ToolResult: %v", err)
	}
	if got, want := string(resultJSON), `{"tool_call_id":"call-1","tool_name":"bash","content":[{"type":"text","text":"ok"}],"is_error":false}`; got != want {
		t.Fatalf("ToolResult JSON = %s, want %s", got, want)
	}

	definitionJSON, err := json.Marshal(definition)
	if err != nil {
		t.Fatalf("marshal ToolDefinition: %v", err)
	}
	if got, want := string(definitionJSON), `{"name":"bash","label":"Read file","description":"execute a command","input_schema":{"type":"object"}}`; got != want {
		t.Fatalf("ToolDefinition JSON = %s, want %s", got, want)
	}

	definition.Label = ""
	definitionJSON, err = json.Marshal(definition)
	if err != nil {
		t.Fatalf("marshal unlabeled ToolDefinition: %v", err)
	}
	if got, want := string(definitionJSON), `{"name":"bash","description":"execute a command","input_schema":{"type":"object"}}`; got != want {
		t.Fatalf("unlabeled ToolDefinition JSON = %s, want %s", got, want)
	}

	parallelDefinition := schema.ToolDefinition{
		Name:         "read_file",
		Description:  "read a file",
		InputSchema:  map[string]any{"type": "object"},
		ParallelSafe: true,
	}
	parallelJSON, err := json.Marshal(parallelDefinition)
	if err != nil {
		t.Fatalf("marshal parallel ToolDefinition: %v", err)
	}
	if got, want := string(parallelJSON), `{"name":"read_file","description":"read a file","input_schema":{"type":"object"},"parallel_safe":true}`; got != want {
		t.Fatalf("parallel ToolDefinition JSON = %s, want %s", got, want)
	}
}

func TestToolMessageJSONContract(t *testing.T) {
	message := schema.Message{
		Role:       schema.RoleTool,
		Content:    []schema.ContentBlock{schema.TextBlock("denied")},
		ToolCallID: "call-1",
		ToolName:   "read",
		IsError:    true,
	}
	encoded, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"role":"tool","content":[{"type":"text","text":"denied"}],"tool_call_id":"call-1","tool_name":"read","is_error":true}`
	if string(encoded) != want {
		t.Fatalf("json = %s", encoded)
	}
}
