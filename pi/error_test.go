package pi_test

import (
	"testing"

	"github.com/PycMono/go-reagent/pi"
)

func TestMessageHelpers(t *testing.T) {
	message := pi.UserMessage("hello")
	if message.Role != pi.RoleUser || len(message.Content) != 1 || message.Content[0] != pi.TextBlock("hello") {
		t.Fatalf("message = %#v", message)
	}
}

func TestErrorCodeValuesAreStable(t *testing.T) {
	want := map[pi.ErrorCode]string{
		pi.ErrorCodeUnknown:          "unknown",
		pi.ErrorCodeConfigLoad:       "config_load_failed",
		pi.ErrorCodeConfigInvalid:    "config_invalid",
		pi.ErrorCodeInitialization:   "initialization_failed",
		pi.ErrorCodeRequestInvalid:   "request_invalid",
		pi.ErrorCodeWorkspaceInvalid: "workspace_invalid",
		pi.ErrorCodeAIGeneration:     "ai_generation_failed",
		pi.ErrorCodeToolRuntime:      "tool_runtime_failed",
		pi.ErrorCodeCanceled:         "canceled",
		pi.ErrorCodeDeadlineExceeded: "deadline_exceeded",
		pi.ErrorCodeClosed:           "agent_closed",
		pi.ErrorCodeInternal:         "internal",
	}
	for code, value := range want {
		if string(code) != value {
			t.Fatalf("code %q = %q, want %q", code, code, value)
		}
	}
}
