package test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
)

func TestRunResultInvocationJSONContract(t *testing.T) {
	result := pi.RunResult{Invocations: []pi.ModelInvocation{{
		Sequence: 1,
		Phase:    pi.ModelInvocationPhaseAction,
		Usage:    ai.Usage{InputTokens: 10, OutputTokens: 4},
	}}}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"phase":"action"`) ||
		!strings.Contains(string(encoded), `"input_tokens":10`) {
		t.Fatalf("RunResult JSON = %s", encoded)
	}
}

func TestRunRequestIgnoresRemovedTrackingFields(t *testing.T) {
	var request pi.RunRequest
	if err := json.Unmarshal([]byte(`{
			"run_id":"run-1",
			"metadata":{"tenant":"one"},
			"input":{"content_type":"text","content":"hello","sender_type":"customer"}
		}`), &request); err != nil {
		t.Fatal(err)
	}
	if request.Input.Content != "hello" || request.Input.SenderType != "customer" {
		t.Fatalf("RunRequest.Input = %#v", request.Input)
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"run_id"`) || strings.Contains(string(encoded), `"metadata"`) {
		t.Fatalf("RunRequest retained removed tracking fields: %s", encoded)
	}
}

func TestMessageJSONContract(t *testing.T) {
	const source = `{
		"content_type":"text",
		"create_time":"2026-08-13 17:23:54",
		"create_ts":"1786613034574",
		"file_url":"",
		"talker_name":"艾小小",
		"content":"您好，感谢您的关注和信任。",
		"id":"578633729103114240",
		"sender_type":"ai"
	}`
	var message pi.Message
	if err := json.Unmarshal([]byte(source), &message); err != nil {
		t.Fatal(err)
	}
	if message.ContentType != "text" ||
		message.CreateTime != "2026-08-13 17:23:54" ||
		message.CreateTS != "1786613034574" ||
		message.FileURL != "" ||
		message.TalkerName != "艾小小" ||
		message.Content != "您好，感谢您的关注和信任。" ||
		message.ID != "578633729103114240" ||
		message.SenderType != "ai" {
		t.Fatalf("Message = %#v", message)
	}
}

func TestMessage2AIMapsBusinessSenders(t *testing.T) {
	tests := []struct {
		name       string
		senderType string
		wantRole   ai.Role
	}{
		{name: "customer", senderType: "customer", wantRole: ai.RoleUser},
		{name: "AI", senderType: "ai", wantRole: ai.RoleAssistant},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := (pi.Message{
				ContentType: "text",
				Content:     "content",
				SenderType:  test.senderType,
			}).Message2AI()
			if err != nil {
				t.Fatal(err)
			}
			if got.Role != test.wantRole || len(got.Content) != 1 || got.Content[0] != ai.TextBlock("content") {
				t.Fatalf("Message2AI() = %#v", got)
			}
		})
	}
}

func TestToolCallArgumentsRemainJSON(t *testing.T) {
	message := ai.Message{
		Role: ai.RoleAssistant,
		ToolCalls: []ai.ToolCall{
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
	result := pi.ToolResult{
		ToolCallID: "call-1",
		ToolName:   "bash",
		Content:    []ai.ContentBlock{ai.TextBlock("ok")},
		IsError:    false,
	}
	definition := ai.ToolDefinition{
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

	parallelDefinition := ai.ToolDefinition{
		Name:         "read",
		Description:  "read a file",
		InputSchema:  map[string]any{"type": "object"},
		ParallelSafe: true,
	}
	parallelJSON, err := json.Marshal(parallelDefinition)
	if err != nil {
		t.Fatalf("marshal parallel ToolDefinition: %v", err)
	}
	if got, want := string(parallelJSON), `{"name":"read","description":"read a file","input_schema":{"type":"object"},"parallel_safe":true}`; got != want {
		t.Fatalf("parallel ToolDefinition JSON = %s, want %s", got, want)
	}
}

func TestToolMessageJSONContract(t *testing.T) {
	message := ai.Message{
		Role:       ai.RoleTool,
		Content:    []ai.ContentBlock{ai.TextBlock("denied")},
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
