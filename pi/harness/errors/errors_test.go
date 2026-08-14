package errors

import (
	"context"
	stderrors "errors"
	"io/fs"
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

func TestClassifyToolUsesStableCodes(t *testing.T) {
	tests := []struct {
		err  error
		want ErrorCode
	}{
		{err: fs.ErrNotExist, want: ErrorCodeToolResourceNotFound},
		{err: fs.ErrPermission, want: ErrorCodeToolPermissionDenied},
		{err: context.DeadlineExceeded, want: ErrorCodeToolTimeout},
		{err: stderrors.New("failed"), want: ErrorCodeToolRuntime},
	}
	for _, tt := range tests {
		got := ClassifyTool("execute", tt.err)
		if ErrorCodeOf(got) != tt.want || !stderrors.Is(got, tt.err) {
			t.Fatalf("ClassifyTool(%v) = %v", tt.err, got)
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
