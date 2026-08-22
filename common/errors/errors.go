package errors

import (
	stderrors "errors"
	"fmt"
	"net/http"
)

// CodeError is an error with a stable application error code.
type CodeError interface {
	error
	Code() int
	Message() string
	Unwrap() error
}

type codeError struct {
	code  int
	msg   string
	cause error
}

func newCodeError(code int, message string, cause error) *codeError {
	return &codeError{code: code, msg: message, cause: cause}
}

func (e *codeError) Code() int       { return e.code }
func (e *codeError) Message() string { return e.msg }
func (e *codeError) Error() string   { return e.msg }
func (e *codeError) Unwrap() error   { return e.cause }

func (e *codeError) Is(target error) bool {
	if codeErr, ok := target.(CodeError); ok {
		return codeErr.Code() == e.code
	}
	return stderrors.Is(e.cause, target)
}

func (e *codeError) Params(args ...any) *codeError {
	return newCodeError(e.code, fmt.Sprintf(e.msg, args...), e.cause)
}

func (e *codeError) Wrap(err error) *codeError {
	return newCodeError(e.code, e.msg, err)
}

// BizError represents an expected business failure.
type BizError struct{ *codeError }

func NewBizError(code int, message string) *BizError {
	return &BizError{newCodeError(code, message, nil)}
}

func (e *BizError) Wrap(err error) *BizError {
	return &BizError{e.codeError.Wrap(err)}
}

func (e *BizError) Params(args ...any) *BizError {
	return &BizError{e.codeError.Params(args...)}
}

func AsBizError(err error) (*BizError, bool) {
	var bizErr *BizError
	if stderrors.As(err, &bizErr) {
		return bizErr, true
	}
	return nil, false
}

// SysError represents an unexpected system failure.
type SysError struct{ *codeError }

func NewSysError(code int, message string) *SysError {
	return &SysError{newCodeError(code, message, nil)}
}

func (e *SysError) Wrap(err error) *SysError {
	return &SysError{e.codeError.Wrap(err)}
}

func (e *SysError) Params(args ...any) *SysError {
	return &SysError{e.codeError.Params(args...)}
}

func AsSysError(err error) (*SysError, bool) {
	var sysErr *SysError
	if stderrors.As(err, &sysErr) {
		return sysErr, true
	}
	return nil, false
}

var (
	ErrInvalidParam = NewBizError(10001, "invalid parameter")
	ErrUnauthorized = NewBizError(10002, "unauthorized")
	ErrForbidden    = NewBizError(10003, "forbidden")
	ErrUserNotFound = NewBizError(10004, "user not found")
	ErrUserExists   = NewBizError(10005, "user already exists")
	ErrNotFound     = NewBizError(10006, "resource not found")
	ErrConflict     = NewBizError(10007, "resource conflict")
	ErrRateLimited  = NewBizError(10008, "rate limited")
	ErrInternal     = NewSysError(10009, "internal server error")
)

// HTTPStatusForCode 是本项目错误码 → HTTP 状态的统一映射，供响应层
// （ginsdk.Send 的 statusFor 参数）使用。
func HTTPStatusForCode(code int) int {
	switch code {
	case ErrInvalidParam.Code():
		return http.StatusBadRequest
	case ErrUnauthorized.Code():
		return http.StatusUnauthorized
	case ErrForbidden.Code():
		return http.StatusForbidden
	case ErrUserNotFound.Code(), ErrNotFound.Code():
		return http.StatusNotFound
	case ErrConflict.Code():
		return http.StatusConflict
	case ErrRateLimited.Code():
		return http.StatusTooManyRequests
	default:
		return http.StatusInternalServerError
	}
}
