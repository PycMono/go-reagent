package reagent_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/PycMono/go-reagent"
)

func TestLoadConfigUsesConfigorAndStableErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"currentPlatform":"missing","platforms":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := reagent.LoadConfig(path)
	if reagent.ErrorCodeOf(err) != reagent.ErrorCodeConfigInvalid {
		t.Fatalf("code = %q, error = %v", reagent.ErrorCodeOf(err), err)
	}
	var sdkErr *reagent.Error
	if !errors.As(err, &sdkErr) {
		t.Fatalf("error type = %T", err)
	}
}

func TestMessageHelpers(t *testing.T) {
	message := reagent.UserMessage("hello")
	if message.Role != reagent.RoleUser || len(message.Content) != 1 || message.Content[0] != reagent.TextBlock("hello") {
		t.Fatalf("message = %#v", message)
	}
}

func TestErrorCodeValuesAreStable(t *testing.T) {
	want := map[reagent.ErrorCode]string{
		reagent.ErrorCodeUnknown:          "unknown",
		reagent.ErrorCodeConfigLoad:       "config_load_failed",
		reagent.ErrorCodeConfigInvalid:    "config_invalid",
		reagent.ErrorCodeInitialization:   "initialization_failed",
		reagent.ErrorCodeRequestInvalid:   "request_invalid",
		reagent.ErrorCodeWorkspaceInvalid: "workspace_invalid",
		reagent.ErrorCodeAIGeneration:     "ai_generation_failed",
		reagent.ErrorCodeToolRuntime:      "tool_runtime_failed",
		reagent.ErrorCodeCanceled:         "canceled",
		reagent.ErrorCodeDeadlineExceeded: "deadline_exceeded",
		reagent.ErrorCodeClosed:           "agent_closed",
		reagent.ErrorCodeInternal:         "internal",
	}
	for code, value := range want {
		if string(code) != value {
			t.Fatalf("code %q = %q, want %q", code, code, value)
		}
	}
}
