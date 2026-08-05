package pi

import (
	"context"
	"errors"
	"fmt"

	"github.com/PycMono/go-reagent/pi/agent"
	"github.com/PycMono/go-reagent/pi/ai"
)

var ErrClosed = errors.New("reagent: agent closed")

// Error carries one stable SDK error code while preserving the concrete cause.
type Error struct {
	Code ErrorCode
	Op   string
	Err  error
}

func (e *Error) Error() string { return fmt.Sprintf("reagent %s [%s]: %v", e.Op, e.Code, e.Err) }
func (e *Error) Unwrap() error { return e.Err }

func ErrorCodeOf(err error) ErrorCode {
	if err == nil {
		return ErrorCodeUnknown
	}
	var sdkErr *Error
	if errors.As(err, &sdkErr) {
		return sdkErr.Code
	}
	switch {
	case errors.Is(err, context.Canceled):
		return ErrorCodeCanceled
	case errors.Is(err, context.DeadlineExceeded):
		return ErrorCodeDeadlineExceeded
	case errors.Is(err, ErrClosed):
		return ErrorCodeClosed
	default:
		return ErrorCodeUnknown
	}
}

func wrap(code ErrorCode, op string, err error) error {
	if err == nil {
		return nil
	}
	var sdkErr *Error
	if errors.As(err, &sdkErr) {
		return err
	}
	return &Error{Code: code, Op: op, Err: err}
}

func classify(op string, err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, context.Canceled):
		return wrap(ErrorCodeCanceled, op, err)
	case errors.Is(err, context.DeadlineExceeded):
		return wrap(ErrorCodeDeadlineExceeded, op, err)
	case errors.Is(err, ErrClosed):
		return wrap(ErrorCodeClosed, op, err)
	case errors.Is(err, agent.ErrRequestInvalid):
		return wrap(ErrorCodeRequestInvalid, op, err)
	case errors.Is(err, ErrInvalid):
		return wrap(ErrorCodeWorkspaceInvalid, op, err)
	case errors.Is(err, ai.ErrGeneration):
		return wrap(ErrorCodeAIGeneration, op, err)
	case errors.Is(err, agent.ErrToolRuntime):
		return wrap(ErrorCodeToolRuntime, op, err)
	default:
		return wrap(ErrorCodeInternal, op, err)
	}
}

func classifyInitialization(op string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrInvalid) {
		return wrap(ErrorCodeWorkspaceInvalid, op, err)
	}
	return wrap(ErrorCodeInitialization, op, err)
}
