// Package errors defines all stable error codes and classified errors used by Pi.
package errors

import (
	"context"
	stderrors "errors"
	"fmt"
)

// ErrorCode is a stable machine-readable Pi error category.
type ErrorCode string

const (
	ErrorCodeUnknown          ErrorCode = "unknown"
	ErrorCodeConfigLoad       ErrorCode = "config_load_failed"
	ErrorCodeConfigInvalid    ErrorCode = "config_invalid"
	ErrorCodeInitialization   ErrorCode = "initialization_failed"
	ErrorCodeRequestInvalid   ErrorCode = "request_invalid"
	ErrorCodeWorkspaceInvalid ErrorCode = "workspace_invalid"
	ErrorCodeAIGeneration     ErrorCode = "ai_generation_failed"
	ErrorCodeToolRuntime      ErrorCode = "tool_runtime_failed"
	ErrorCodeCanceled         ErrorCode = "canceled"
	ErrorCodeDeadlineExceeded ErrorCode = "deadline_exceeded"
	ErrorCodeClosed           ErrorCode = "agent_closed"
	ErrorCodeInternal         ErrorCode = "internal"
)

var (
	ErrClosed           = stderrors.New("reagent: agent closed")
	ErrWorkspaceInvalid = stderrors.New("agent workspace invalid")
	ErrGeneration       = stderrors.New("ai generation failed")
	ErrRequestInvalid   = stderrors.New("agent request invalid")
	ErrToolRuntime      = stderrors.New("agent tool runtime failed")
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
	case stderrors.Is(err, ErrGeneration):
		return Wrap(ErrorCodeAIGeneration, op, err)
	case stderrors.Is(err, ErrToolRuntime):
		return Wrap(ErrorCodeToolRuntime, op, err)
	default:
		return Wrap(ErrorCodeInternal, op, err)
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

// GenerationError preserves the provider error while classifying model generation.
type GenerationError struct {
	Op  string
	Err error
}

func (err *GenerationError) Error() string { return fmt.Sprintf("%s: %v", err.Op, err.Err) }
func (err *GenerationError) Unwrap() error { return err.Err }
func (err *GenerationError) Is(target error) bool {
	return target == ErrGeneration
}

func WrapGeneration(op string, err error) error {
	if err == nil {
		return nil
	}
	return &GenerationError{Op: op, Err: err}
}
