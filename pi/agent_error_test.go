package pi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
	openaisdk "github.com/openai/openai-go/v3"
)

func TestAgentRunPreservesPartialResultAndOfficialOpenAIError(t *testing.T) {
	actionCount := 0
	sdk := newTestAgentWithHandler(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		if _, hasTools := body["tools"]; !hasTools {
			writeOpenAIMessage(t, w, "plan")
			return
		}

		actionCount++
		if actionCount == 1 {
			writeOpenAIToolCall(t, w, "call-read", "read", `{"path":"AGENTS.md"}`)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		if err := json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"message": "model unavailable",
				"type":    "server_error",
				"code":    "internal_error",
			},
		}); err != nil {
			t.Errorf("encode error response: %v", err)
		}
	})

	result, err := sdk.Run(context.Background(), pi.RunRequest{
		RunID: "partial-run",
		Input: pi.UserMessage("read the agent definition"),
	})
	if pi.ErrorCodeOf(err) != pi.ErrorCodeAIGeneration {
		t.Fatalf("error code = %q, error = %v", pi.ErrorCodeOf(err), err)
	}
	if !errors.Is(err, ai.ErrGeneration) {
		t.Fatalf("error does not wrap ai.ErrGeneration: %v", err)
	}
	var openAIErr *openaisdk.Error
	if !errors.As(err, &openAIErr) {
		t.Fatalf("error type = %T, want *openai.Error in chain", err)
	}
	if result.RunID != "partial-run" || len(result.NewMessages) != 2 {
		t.Fatalf("RunResult = %#v", result)
	}
	callMessage := result.NewMessages[0]
	if callMessage.Role != pi.RoleAssistant || len(callMessage.ToolCalls) != 1 || callMessage.ToolCalls[0].ID != "call-read" {
		t.Fatalf("tool call message = %#v", callMessage)
	}
	toolMessage := result.NewMessages[1]
	if toolMessage.Role != pi.RoleTool || toolMessage.ToolCallID != "call-read" || toolMessage.IsError || len(toolMessage.Content) == 0 {
		t.Fatalf("tool result message = %#v", toolMessage)
	}
}

func writeOpenAIToolCall(t *testing.T, w http.ResponseWriter, id, name, arguments string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{
		"id": "chatcmpl-tool", "object": "chat.completion", "created": 1, "model": "model",
		"choices": []any{map[string]any{
			"index": 0,
			"message": map[string]any{
				"role": "assistant", "content": "",
				"tool_calls": []any{map[string]any{
					"id": id, "type": "function",
					"function": map[string]any{"name": name, "arguments": arguments},
				}},
			},
			"finish_reason": "tool_calls",
		}},
		"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
	}); err != nil {
		t.Errorf("encode tool call response: %v", err)
	}
}
