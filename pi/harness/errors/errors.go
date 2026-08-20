// Package errors defines all stable error codes and classified errors used by Pi.
package errors

import (
	"context"
	stderrors "errors"
	"fmt"
	"io/fs"
)

// ErrorCode is a stable machine-readable Pi error category.
type ErrorCode string

const (
	ErrorCodeUnknown              ErrorCode = "unknown"
	ErrorCodeConfigLoad           ErrorCode = "config_load_failed"
	ErrorCodeConfigInvalid        ErrorCode = "config_invalid"
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

func Classify(op string, err error) error {
	if err == nil {
		return nil
	}
	switch {
	case stderrors.Is(err, context.Canceled):
		return Wrap(ErrorCodeCanceled, op, err)
	case stderrors.Is(err, context.DeadlineExceeded):
		return Wrap(ErrorCodeDeadlineExceeded, op, err)
	case stderrors.Is(err, ErrClosed):
		return Wrap(ErrorCodeClosed, op, err)
	case stderrors.Is(err, ErrRequestInvalid):
		return Wrap(ErrorCodeRequestInvalid, op, err)
	case stderrors.Is(err, ErrWorkspaceInvalid):
		return Wrap(ErrorCodeWorkspaceInvalid, op, err)
	case stderrors.Is(err, ErrToolRuntime):
		return Wrap(ErrorCodeToolRuntime, op, err)
	case stderrors.Is(err, ErrRunLimitExceeded):
		return Wrap(ErrorCodeRunLimitExceeded, op, err)
	default:
		return Wrap(ErrorCodeInternal, op, err)
	}
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

func ClassifyInitialization(op string, err error) error {
	if err == nil {
		return nil
	}
	if stderrors.Is(err, ErrWorkspaceInvalid) {
		return Wrap(ErrorCodeWorkspaceInvalid, op, err)
	}
	return Wrap(ErrorCodeInitialization, op, err)
}
