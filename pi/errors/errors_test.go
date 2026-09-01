package errors

import (
	"context"
	stderrors "errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"testing"
)

func TestClassifyAIProviderUsesNormalizedFacts(t *testing.T) {
	tests := []struct {
		name string
		info AIProviderErrorInfo
		want ErrorCode
	}{
		{name: "context overflow", info: AIProviderErrorInfo{ContextOverflow: true, Err: stderrors.New("overflow")}, want: ErrorCodeAIContextOverflow},
		{name: "quota", info: AIProviderErrorInfo{QuotaExceeded: true, Err: stderrors.New("quota")}, want: ErrorCodeAIQuotaExceeded},
		{name: "rate limit", info: AIProviderErrorInfo{StatusCode: http.StatusTooManyRequests, Err: stderrors.New("429")}, want: ErrorCodeAIRateLimited},
		{name: "request timeout", info: AIProviderErrorInfo{StatusCode: http.StatusRequestTimeout, Err: stderrors.New("408")}, want: ErrorCodeAITransient},
		{name: "conflict", info: AIProviderErrorInfo{StatusCode: http.StatusConflict, Err: stderrors.New("409")}, want: ErrorCodeAITransient},
		{name: "server", info: AIProviderErrorInfo{StatusCode: http.StatusBadGateway, Err: stderrors.New("502")}, want: ErrorCodeAITransient},
		{name: "unauthorized", info: AIProviderErrorInfo{StatusCode: http.StatusUnauthorized, Err: stderrors.New("401")}, want: ErrorCodeAIUnauthorized},
		{name: "forbidden", info: AIProviderErrorInfo{StatusCode: http.StatusForbidden, Err: stderrors.New("403")}, want: ErrorCodeAIUnauthorized},
		{name: "bad request", info: AIProviderErrorInfo{StatusCode: http.StatusBadRequest, Err: stderrors.New("400")}, want: ErrorCodeAIInvalidRequest},
		{name: "unknown", info: AIProviderErrorInfo{Err: stderrors.New("unknown")}, want: ErrorCodeAIGeneration},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyAIProvider(tt.info)
			if ErrorCodeOf(got) != tt.want || !stderrors.Is(got, tt.info.Err) {
				t.Fatalf("ClassifyAIProvider() = %v, want code %q with original cause", got, tt.want)
			}
		})
	}
}

func TestClassifyAIProviderUsesContextAndNetworkErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want ErrorCode
	}{
		{name: "canceled", err: context.Canceled, want: ErrorCodeCanceled},
		{name: "deadline", err: context.DeadlineExceeded, want: ErrorCodeDeadlineExceeded},
		{name: "DNS", err: &net.DNSError{IsTimeout: true}, want: ErrorCodeAITransient},
		{name: "EOF", err: io.ErrUnexpectedEOF, want: ErrorCodeAITransient},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyAIProvider(AIProviderErrorInfo{Err: tt.err})
			if ErrorCodeOf(got) != tt.want || !stderrors.Is(got, tt.err) {
				t.Fatalf("ClassifyAIProvider() = %v, want code %q with original cause", got, tt.want)
			}
		})
	}
}

func TestErrorCodeValuesAreStable(t *testing.T) {
	want := map[ErrorCode]string{
		ErrorCodeUnknown:        "unknown",
		ErrorCodeInitialization: "initialization_failed",
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

func TestWrapPreservesSpecificCodeAndCause(t *testing.T) {
	cause := stderrors.New("provider failed")
	err := Wrap(ErrorCodeAITransient, "action", cause)
	if ErrorCodeOf(err) != ErrorCodeAITransient || !stderrors.Is(err, cause) {
		t.Fatalf("classified error = %v", err)
	}
	if ErrorCodeOf(context.Canceled) != ErrorCodeCanceled {
		t.Fatalf("canceled code = %q", ErrorCodeOf(context.Canceled))
	}
}
