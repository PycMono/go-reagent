package errors

import (
	"context"
	stderrors "errors"
	"testing"
)

func TestErrorCodeValuesAreStable(t *testing.T) {
	want := map[ErrorCode]string{
		ErrorCodeUnknown: "unknown", ErrorCodeConfigLoad: "config_load_failed",
		ErrorCodeConfigInvalid: "config_invalid", ErrorCodeInitialization: "initialization_failed",
		ErrorCodeRequestInvalid: "request_invalid", ErrorCodeWorkspaceInvalid: "workspace_invalid",
		ErrorCodeAIGeneration: "ai_generation_failed", ErrorCodeToolRuntime: "tool_runtime_failed",
		ErrorCodeCanceled: "canceled", ErrorCodeDeadlineExceeded: "deadline_exceeded",
		ErrorCodeClosed: "agent_closed", ErrorCodeInternal: "internal",
	}
	for code, value := range want {
		if string(code) != value {
			t.Fatalf("code %q = %q, want %q", code, code, value)
		}
	}
}

func TestClassifyPreservesIdentityAndCause(t *testing.T) {
	cause := stderrors.New("provider failed")
	err := Classify("Run", WrapGeneration("action", cause))
	if ErrorCodeOf(err) != ErrorCodeAIGeneration || !stderrors.Is(err, ErrGeneration) || !stderrors.Is(err, cause) {
		t.Fatalf("classified error = %v", err)
	}
	if ErrorCodeOf(context.Canceled) != ErrorCodeCanceled {
		t.Fatalf("canceled code = %q", ErrorCodeOf(context.Canceled))
	}
}
