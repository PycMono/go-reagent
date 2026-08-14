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
		ErrorCodeAITransient: "ai_transient", ErrorCodeAIRateLimited: "ai_rate_limited",
		ErrorCodeAIContextOverflow: "ai_context_overflow", ErrorCodeAIUnauthorized: "ai_unauthorized",
		ErrorCodeAIQuotaExceeded: "ai_quota_exceeded", ErrorCodeAIInvalidRequest: "ai_invalid_request",
		ErrorCodeToolInvalidArguments: "tool_invalid_arguments", ErrorCodeToolResourceNotFound: "tool_resource_not_found",
		ErrorCodeToolPermissionDenied: "tool_permission_denied", ErrorCodeToolEditNoMatch: "tool_edit_no_match",
		ErrorCodeToolEditNotUnique: "tool_edit_not_unique", ErrorCodeToolTimeout: "tool_timeout",
		ErrorCodeToolPanic: "tool_panic",
		ErrorCodeCanceled:  "canceled", ErrorCodeDeadlineExceeded: "deadline_exceeded",
		ErrorCodeClosed: "agent_closed", ErrorCodeInternal: "internal",
	}
	for code, value := range want {
		if string(code) != value {
			t.Fatalf("code %q = %q, want %q", code, code, value)
		}
	}
}

func TestClassifyPreservesSpecificCodeAndCause(t *testing.T) {
	cause := stderrors.New("provider failed")
	err := Classify("Run", Wrap(ErrorCodeAITransient, "action", cause))
	if ErrorCodeOf(err) != ErrorCodeAITransient || !stderrors.Is(err, cause) {
		t.Fatalf("classified error = %v", err)
	}
	if ErrorCodeOf(context.Canceled) != ErrorCodeCanceled {
		t.Fatalf("canceled code = %q", ErrorCodeOf(context.Canceled))
	}
}
