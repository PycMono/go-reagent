package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/PycMono/go-reagent/internal/schema"
)

func TestClaudeProviderTranslatesToolConversation(t *testing.T) {
	requestBody := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("request path = %q, want %q", r.URL.Path, "/v1/messages")
		}
		if got := r.Header.Get("X-Api-Key"); got != "test-key" {
			t.Errorf("X-Api-Key = %q", got)
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		requestBody <- body

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg-1",
			"type":"message",
			"role":"assistant",
			"model":"test-model",
			"content":[
				{"type":"text","text":"checking"},
				{"type":"tool_use","id":"call-new","name":"get_weather","input":{"city":"北京"}}
			],
			"stop_reason":"tool_use",
			"stop_sequence":null,
			"usage":{"input_tokens":1,"output_tokens":1}
		}`))
	}))
	defer server.Close()

	p := newClaudeProvider("test-key", server.URL+"/", "test-model", "test")
	messages := []schema.Message{
		{Role: schema.RoleSystem, Content: "system prompt"},
		{Role: schema.RoleUser, Content: "weather"},
		{
			Role: schema.RoleAssistant,
			ToolCalls: []schema.ToolCall{
				{ID: "call-old", Name: "get_weather", Arguments: json.RawMessage(`{"city":"上海"}`)},
			},
		},
		{Role: schema.RoleUser, Content: "sunny", ToolCallID: "call-old"},
	}
	definitions := []schema.ToolDefinition{
		{
			Name:        "get_weather",
			Description: "get weather",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"city": map[string]any{"type": "string"},
				},
				"required": []string{"city"},
			},
		},
	}

	result, err := p.Generate(context.Background(), messages, definitions)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if result.Content != "checking" || len(result.ToolCalls) != 1 {
		t.Fatalf("result = %#v", result)
	}
	call := result.ToolCalls[0]
	if call.ID != "call-new" || call.Name != "get_weather" || string(call.Arguments) != `{"city":"北京"}` {
		t.Fatalf("tool call = %#v", call)
	}

	body := <-requestBody
	if body["model"] != "test-model" || body["max_tokens"] != float64(4096) {
		t.Fatalf("request model/tokens = %#v / %#v", body["model"], body["max_tokens"])
	}
	system := body["system"].([]any)
	if got := system[0].(map[string]any)["text"]; got != "system prompt" {
		t.Fatalf("system prompt = %#v", got)
	}
	requestMessages := body["messages"].([]any)
	toolResultContent := requestMessages[2].(map[string]any)["content"].([]any)
	if got := toolResultContent[0].(map[string]any)["type"]; got != "tool_result" {
		t.Fatalf("tool result block type = %#v", got)
	}
	if tools, ok := body["tools"].([]any); !ok || len(tools) != 1 {
		t.Fatalf("tools = %#v", body["tools"])
	}
}

func TestClaudeProviderOmitsToolsDuringThinking(t *testing.T) {
	requestBody := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		requestBody <- body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg-2","type":"message","role":"assistant","model":"test-model",
			"content":[{"type":"text","text":"plan"}],
			"stop_reason":"end_turn","stop_sequence":null,
			"usage":{"input_tokens":1,"output_tokens":1}
		}`))
	}))
	defer server.Close()

	p := newClaudeProvider("test-key", server.URL+"/", "test-model", "test")
	_, err := p.Generate(context.Background(), []schema.Message{{Role: schema.RoleUser, Content: "plan"}}, nil)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if _, exists := (<-requestBody)["tools"]; exists {
		t.Fatal("thinking request unexpectedly contains tools")
	}
}
