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

func TestCodeErrorBasics(t *testing.T) {
	cause := stderrors.New("connection reset")
	err := ErrToolRuntime.Wrap(cause)
	if err == nil {
		t.Fatal("Wrap should keep a non-nil cause as error")
	}
	if ErrToolRuntime.Wrap(nil) != nil {
		t.Fatal("Wrap(nil) must return nil")
	}

	coded, ok := AsCodeError(err)
	if !ok {
		t.Fatal("AsCodeError should decode the wrapped error")
	}
	if coded.Code() != 30000 {
		t.Fatalf("Code() = %d, want 30000", coded.Code())
	}
	if coded.Message() != "工具执行失败" {
		t.Fatalf("Message() = %q", coded.Message())
	}
	if !stderrors.Is(err, ErrToolRuntime) {
		t.Fatal("errors.Is should match by code")
	}
	if !stderrors.Is(err, cause) {
		t.Fatal("errors.Is should reach the wrapped cause")
	}
	if CodeOf(err) != 30000 {
		t.Fatalf("CodeOf = %d, want 30000", CodeOf(err))
	}
	if _, ok := AsCodeError(cause); ok {
		t.Fatal("plain cause should not decode as CodeError")
	}
}

func TestCodeErrorParams(t *testing.T) {
	tpl := New(30008, "最多 %d 张图片，收到 %d 张")
	if got := tpl.Params(3, 5).Message(); got != "最多 3 张图片，收到 5 张" {
		t.Fatalf("Params message = %q", got)
	}
	if tpl.Params(3, 5).Code() != 30008 {
		t.Fatal("Params must keep the code")
	}
}

func TestCodeOfContextSentinels(t *testing.T) {
	if CodeOf(context.Canceled) != ErrCanceled.Code() {
		t.Fatalf("canceled code = %d", CodeOf(context.Canceled))
	}
	if CodeOf(context.DeadlineExceeded) != ErrDeadlineExceeded.Code() {
		t.Fatalf("deadline code = %d", CodeOf(context.DeadlineExceeded))
	}
	if CodeOf(nil) != 0 {
		t.Fatalf("nil code = %d", CodeOf(nil))
	}
	if CodeOf(stderrors.New("failed")) != 0 {
		t.Fatalf("unclassified code = %d", CodeOf(stderrors.New("failed")))
	}
}

func TestMatch(t *testing.T) {
	if !ErrAITransient.Match(ErrAITransient.Wrap(stderrors.New("boom"))) {
		t.Fatal("wrapped sentinel should match its own code")
	}
	if ErrAIRateLimited.Match(ErrAITransient.Wrap(stderrors.New("boom"))) {
		t.Fatal("different code must not match")
	}
	if !ErrCanceled.Match(context.Canceled) {
		t.Fatal("raw context sentinel should match")
	}
	if ErrToolPanic.Match(nil) || ErrToolPanic.Match(stderrors.New("plain")) {
		t.Fatal("nil/unclassified must not match")
	}
}

func TestSentinelCodesAreUniqueAndPaired(t *testing.T) {
	sentinels := []*CodeError{
		ErrUnknown, ErrInitialization, ErrRequestInvalid, ErrWorkspaceInvalid,
		ErrAIGeneration, ErrAITransient, ErrAIRateLimited, ErrAIContextOverflow,
		ErrAIUnauthorized, ErrAIQuotaExceeded, ErrAIInvalidRequest,
		ErrToolRuntime, ErrToolInvalidArguments, ErrToolResourceNotFound,
		ErrToolPermissionDenied, ErrToolEditNoMatch, ErrToolEditNotUnique,
		ErrToolTimeout, ErrToolPanic,
		ErrCanceled, ErrDeadlineExceeded,
		ErrRunLimitExceeded, ErrRunLoopDetected,
		ErrClosed, ErrInternal,
	}
	seen := make(map[int]bool)
	for _, sentinel := range sentinels {
		if sentinel.Code() == 0 {
			t.Fatalf("%s has zero code", sentinel.Message())
		}
		if sentinel.Message() == "" {
			t.Fatalf("code %d has empty message", sentinel.Code())
		}
		if seen[sentinel.Code()] {
			t.Fatalf("duplicate code %d", sentinel.Code())
		}
		seen[sentinel.Code()] = true
	}
}

func TestHTTPStatusToCode(t *testing.T) {
	tests := []struct {
		name   string
		status int
		want   *CodeError
	}{
		{name: "rate limit", status: http.StatusTooManyRequests, want: ErrAIRateLimited},
		{name: "request timeout", status: http.StatusRequestTimeout, want: ErrAITransient},
		{name: "conflict", status: http.StatusConflict, want: ErrAITransient},
		{name: "server", status: http.StatusBadGateway, want: ErrAITransient},
		{name: "unauthorized", status: http.StatusUnauthorized, want: ErrAIUnauthorized},
		{name: "forbidden", status: http.StatusForbidden, want: ErrAIUnauthorized},
		{name: "bad request", status: http.StatusBadRequest, want: ErrAIInvalidRequest},
		{name: "unknown status", status: 0, want: ErrAIGeneration},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HTTPStatusToCode(tt.status); got != tt.want {
				t.Fatalf("HTTPStatusToCode(%d) = code %d, want %d", tt.status, got.Code(), tt.want.Code())
			}
		})
	}
}

func TestIsTransientNetwork(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "DNS", err: &net.DNSError{IsTimeout: true}, want: true},
		{name: "EOF", err: io.ErrUnexpectedEOF, want: true},
		{name: "plain", err: stderrors.New("failed"), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsTransientNetwork(tt.err); got != tt.want {
				t.Fatalf("IsTransientNetwork(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestClassifyToolUsesStableCodes(t *testing.T) {
	tests := []struct {
		err  error
		want *CodeError
	}{
		{err: fs.ErrNotExist, want: ErrToolResourceNotFound},
		{err: fs.ErrPermission, want: ErrToolPermissionDenied},
		{err: context.DeadlineExceeded, want: ErrToolTimeout},
		{err: stderrors.New("failed"), want: ErrToolRuntime},
		// 已携带稳定码的错误不再重分类。
		{err: ErrToolPanic.Wrap(stderrors.New("boom")), want: ErrToolPanic},
	}
	for _, tt := range tests {
		got := ClassifyTool(tt.err)
		if CodeOf(got) != tt.want.Code() || !stderrors.Is(got, tt.err) {
			t.Fatalf("ClassifyTool(%v) = %v", tt.err, got)
		}
	}
}
