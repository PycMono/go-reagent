package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/PycMono/go-reagent/ai"
)

func TestOpenAICompatibleProviderTranslatesToolConversation(t *testing.T) {
	requestBody := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("request path = %q, want %q", r.URL.Path, "/v1/chat/completions")
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q", got)
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		requestBody <- body

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"chatcmpl-1",
			"object":"chat.completion",
			"created":1,
			"model":"test-model",
			"choices":[{
				"index":0,
				"message":{
					"role":"assistant",
					"content":"",
					"tool_calls":[{
						"id":"call-new",
						"type":"function",
						"function":{"name":"get_weather","arguments":"{\"city\":\"北京\"}"}
					}]
				},
				"finish_reason":"tool_calls"
			}],
			"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}
		}`))
	}))
	defer server.Close()

	p := New(ai.PlatformConfig{ID: "test", APIKey: "test-key", BaseURL: server.URL + "/v1/", Model: "test-model"})
	messages := []ai.Message{
		{Role: ai.RoleSystem, Content: []ai.ContentBlock{ai.TextBlock("system prompt")}},
		{Role: ai.RoleUser, Content: []ai.ContentBlock{ai.TextBlock("weather")}},
		{
			Role: ai.RoleAssistant,
			ToolCalls: []ai.ToolCall{
				{ID: "call-old", Name: "get_weather", Arguments: json.RawMessage(`{"city":"上海"}`)},
			},
		},
		{Role: ai.RoleTool, Content: []ai.ContentBlock{ai.TextBlock("sunny")}, ToolCallID: "call-old"},
	}
	definitions := []ai.ToolDefinition{
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
	if len(result.ToolCalls) != 1 {
		t.Fatalf("tool calls = %#v", result.ToolCalls)
	}
	call := result.ToolCalls[0]
	if call.ID != "call-new" || call.Name != "get_weather" || string(call.Arguments) != `{"city":"北京"}` {
		t.Fatalf("tool call = %#v", call)
	}

	body := <-requestBody
	if body["model"] != "test-model" {
		t.Fatalf("model = %#v", body["model"])
	}
	requestMessages := body["messages"].([]any)
	if got := requestMessages[3].(map[string]any)["role"]; got != "tool" {
		t.Fatalf("tool result role = %#v", got)
	}
	if got := requestMessages[3].(map[string]any)["tool_call_id"]; got != "call-old" {
		t.Fatalf("tool_call_id = %#v", got)
	}
	if tools, ok := body["tools"].([]any); !ok || len(tools) != 1 {
		t.Fatalf("tools = %#v", body["tools"])
	}
}

func TestOpenAICompatibleProviderOmitsToolsDuringThinking(t *testing.T) {
	requestBody := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		requestBody <- body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"chatcmpl-2","object":"chat.completion","created":1,"model":"test-model",
			"choices":[{"index":0,"message":{"role":"assistant","content":"plan"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}
		}`))
	}))
	defer server.Close()

	p := New(ai.PlatformConfig{ID: "test", APIKey: "test-key", BaseURL: server.URL + "/v1/", Model: "test-model"})
	_, err := p.Generate(context.Background(), []ai.Message{{Role: ai.RoleUser, Content: []ai.ContentBlock{ai.TextBlock("plan")}}}, nil)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if _, exists := (<-requestBody)["tools"]; exists {
		t.Fatal("thinking request unexpectedly contains tools")
	}
}

func TestOpenAIMessagesMapNativeToolResults(t *testing.T) {
	message := ai.Message{
		Role:       ai.RoleTool,
		Content:    []ai.ContentBlock{ai.TextBlock("permission denied")},
		ToolCallID: "call-1",
		ToolName:   "read",
		IsError:    true,
	}

	messages, err := toOpenAIMessages([]ai.Message{message})
	if err != nil {
		t.Fatalf("toOpenAIMessages() error = %v", err)
	}
	encoded, err := json.Marshal(messages)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var decoded []map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if got := decoded[0]["role"]; got != "tool" {
		t.Fatalf("role = %#v", got)
	}
	if got := decoded[0]["tool_call_id"]; got != "call-1" {
		t.Fatalf("tool_call_id = %#v", got)
	}
	if _, exists := decoded[0]["details"]; exists {
		t.Fatalf("tool result serialized Details: %#v", decoded[0])
	}
}

func TestOpenAIMessagesRejectToolResultWithoutCallID(t *testing.T) {
	_, err := toOpenAIMessages([]ai.Message{{
		Role:    ai.RoleTool,
		Content: []ai.ContentBlock{ai.TextBlock("permission denied")},
	}})
	if err == nil {
		t.Fatal("toOpenAIMessages() error = nil")
	}
}
