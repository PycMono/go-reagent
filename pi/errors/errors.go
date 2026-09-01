// Package errors defines all stable error codes and classified errors used by Pi.
//
// 它是 pi 全域唯一的错误码与分类框架所在（引用别名统一为 pierrors）。
// 分层规则：
//   - 稳定 ErrorCode 与 Wrap/ErrorCodeOf 分类机制只允许定义在本包；
//   - 需要跨包识别的领域错误类型（如 loopdetect.Error、governor 的
//     limitError）随领域包定义——集中到本包会造成包循环并污染通用框架；
//   - 业务/HTTP 层的 BizError 码在 common/errors，不经本包。
package errors

import (
	"context"
	stderrors "errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
)

// AIProviderErrorInfo contains provider-neutral facts used to classify an AI
// generation failure. Provider adapters are responsible for normalizing their
// SDK-specific errors into this structure.
type AIProviderErrorInfo struct {
	StatusCode      int
	ContextOverflow bool
	QuotaExceeded   bool
	Err             error
}

// ErrorCode is a stable machine-readable Pi error category.
type ErrorCode string

const (
	ErrorCodeUnknown              ErrorCode = "unknown"
	ErrorCodeInitialization       ErrorCode = "initialization_failed"
	ErrorCodeRequestInvalid       ErrorCode = "request_invalid"
	ErrorCodeWorkspaceInvalid     ErrorCode = "workspace_invalid"
	ErrorCodeAIGeneration         ErrorCode = "ai_generation_failed"
	ErrorCodeAITransient          ErrorCode = "ai_transient"
	ErrorCodeAIRateLimited        ErrorCode = "ai_rate_limited"
	ErrorCodeAIContextOverflow    ErrorCode = "ai_context_overflow"
	ErrorCodeAIUnauthorized       ErrorCode = "ai_unauthorized"
	ErrorCodeAIQuotaExceeded      ErrorCode = "ai_quota_exceeded"
	ErrorCodeAIInvalidRequest     ErrorCode = "ai_invalid_request"
	ErrorCodeToolRuntime          ErrorCode = "tool_runtime_failed"
	ErrorCodeToolInvalidArguments ErrorCode = "tool_invalid_arguments"
	ErrorCodeToolResourceNotFound ErrorCode = "tool_resource_not_found"
	ErrorCodeToolPermissionDenied ErrorCode = "tool_permission_denied"
	ErrorCodeToolEditNoMatch      ErrorCode = "tool_edit_no_match"
	ErrorCodeToolEditNotUnique    ErrorCode = "tool_edit_not_unique"
	ErrorCodeToolTimeout          ErrorCode = "tool_timeout"
	ErrorCodeToolPanic            ErrorCode = "tool_panic"
	ErrorCodeCanceled             ErrorCode = "canceled"
	ErrorCodeDeadlineExceeded     ErrorCode = "deadline_exceeded"
	ErrorCodeRunLimitExceeded     ErrorCode = "run_limit_exceeded"
	ErrorCodeRunLoopDetected      ErrorCode = "run_loop_detected"
	ErrorCodeClosed               ErrorCode = "agent_closed"
	ErrorCodeInternal             ErrorCode = "internal"
)

var (
	ErrClosed           = stderrors.New("reagent: agent closed")
	ErrWorkspaceInvalid = stderrors.New("agent workspace invalid")
	ErrRequestInvalid   = stderrors.New("agent request invalid")
	ErrToolRuntime      = stderrors.New("agent tool runtime failed")
	ErrRunLimitExceeded = stderrors.New("agent run limit exceeded")
)

// Error carries one stable Pi error code while preserving the concrete cause.
type Error struct {
	Code ErrorCode
	Op   string
	Err  error
}

func (err *Error) Error() string {
	return fmt.Sprintf("reagent %s [%s]: %v", err.Op, err.Code, err.Err)
}

func (err *Error) Unwrap() error { return err.Err }

func ErrorCodeOf(err error) ErrorCode {
	if err == nil {
		return ErrorCodeUnknown
	}
	var classified *Error
	if stderrors.As(err, &classified) {
		return classified.Code
	}
	switch {
	case stderrors.Is(err, context.Canceled):
		return ErrorCodeCanceled
	case stderrors.Is(err, context.DeadlineExceeded):
		return ErrorCodeDeadlineExceeded
	case stderrors.Is(err, ErrClosed):
		return ErrorCodeClosed
	case stderrors.Is(err, ErrRunLimitExceeded):
		return ErrorCodeRunLimitExceeded
	case stderrors.Is(err, ErrRequestInvalid):
		return ErrorCodeRequestInvalid
	default:
		return ErrorCodeUnknown
	}
}

func Wrap(code ErrorCode, op string, err error) error {
	if err == nil {
		return nil
	}
	var classified *Error
	if stderrors.As(err, &classified) {
		return err
	}
	return &Error{Code: code, Op: op, Err: err}
}

func ClassifyAIProvider(info AIProviderErrorInfo) error {
	code := ErrorCodeAIGeneration
	switch {
	case stderrors.Is(info.Err, context.Canceled):
		code = ErrorCodeCanceled
	case stderrors.Is(info.Err, context.DeadlineExceeded):
		code = ErrorCodeDeadlineExceeded
	case info.ContextOverflow:
		code = ErrorCodeAIContextOverflow
	case info.QuotaExceeded:
		code = ErrorCodeAIQuotaExceeded
	case info.StatusCode == http.StatusTooManyRequests:
		code = ErrorCodeAIRateLimited
	case info.StatusCode == http.StatusRequestTimeout,
		info.StatusCode == http.StatusConflict,
		info.StatusCode >= http.StatusInternalServerError:
		code = ErrorCodeAITransient
	case isTransientAIProviderError(info.Err):
		code = ErrorCodeAITransient
	case info.StatusCode == http.StatusUnauthorized,
		info.StatusCode == http.StatusForbidden:
		code = ErrorCodeAIUnauthorized
	case info.StatusCode == http.StatusBadRequest:
		code = ErrorCodeAIInvalidRequest
	}
	return Wrap(code, "provider generate", info.Err)
}

func isTransientAIProviderError(err error) bool {
	if stderrors.Is(err, io.EOF) || stderrors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var networkError net.Error
	return stderrors.As(err, &networkError)
}

func ClassifyTool(op string, err error) error {
	if err == nil {
		return nil
	}
	var classified *Error
	if stderrors.As(err, &classified) {
		return err
	}
	switch {
	case stderrors.Is(err, context.Canceled):
		return Wrap(ErrorCodeCanceled, op, err)
	case stderrors.Is(err, context.DeadlineExceeded):
		return Wrap(ErrorCodeToolTimeout, op, err)
	case stderrors.Is(err, fs.ErrNotExist):
		return Wrap(ErrorCodeToolResourceNotFound, op, err)
	case stderrors.Is(err, fs.ErrPermission):
		return Wrap(ErrorCodeToolPermissionDenied, op, err)
	default:
		return Wrap(ErrorCodeToolRuntime, op, err)
	}
}
