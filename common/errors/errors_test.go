package errors

import (
	stderrors "errors"
	"fmt"
	"net/http"
	"testing"
)

func TestCommonErrorsHaveStableIdentityAndClassification(t *testing.T) {
	tests := []struct {
		name    string
		err     CodeError
		code    int
		message string
		biz     bool
	}{
		{name: "not found", err: ErrNotFound, code: 10006, message: "resource not found", biz: true},
		{name: "conflict", err: ErrConflict, code: 10007, message: "resource conflict", biz: true},
		{name: "internal", err: ErrInternal, code: 10009, message: "internal server error"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.err.Code() != test.code || test.err.Message() != test.message || test.err.Error() != test.message {
				t.Fatalf("error identity = (%d, %q, %q), want (%d, %q, %q)",
					test.err.Code(), test.err.Message(), test.err.Error(), test.code, test.message, test.message)
			}
			_, isBiz := AsBizError(test.err)
			_, isSys := AsSysError(test.err)
			if isBiz != test.biz || isSys == test.biz {
				t.Fatalf("error classification = biz:%v sys:%v, want biz:%v sys:%v", isBiz, isSys, test.biz, !test.biz)
			}
		})
	}

	if stderrors.Is(ErrNotFound, ErrConflict) ||
		stderrors.Is(ErrNotFound, ErrInternal) ||
		stderrors.Is(ErrConflict, ErrInternal) {
		t.Fatal("common errors must have distinct identities")
	}
}

func TestCommonErrorWrapPreservesIdentityAndCause(t *testing.T) {
	cause := fmt.Errorf("invalid JSON payload")
	wrapper := ErrInternal.Wrap(cause)

	if !stderrors.Is(wrapper, ErrInternal) {
		t.Fatal("wrapped error lost common error identity")
	}
	if !stderrors.Is(wrapper, cause) {
		t.Fatal("wrapped error lost its cause")
	}
	if _, ok := AsSysError(wrapper); !ok {
		t.Fatal("wrapped corrupt-message error must remain a system error")
	}
}

// TestHTTPStatusForCode 锁定错误码 → HTTP 状态映射表（响应层
// ginsdk.Send 的 statusFor 参数）。
func TestHTTPStatusForCode(t *testing.T) {
	tests := []struct {
		code int
		want int
	}{
		{ErrInvalidParam.Code(), http.StatusBadRequest},
		{ErrUnauthorized.Code(), http.StatusUnauthorized},
		{ErrForbidden.Code(), http.StatusForbidden},
		{ErrUserNotFound.Code(), http.StatusNotFound},
		{ErrNotFound.Code(), http.StatusNotFound},
		{ErrConflict.Code(), http.StatusConflict},
		{ErrRateLimited.Code(), http.StatusTooManyRequests},
		{ErrInternal.Code(), http.StatusInternalServerError},
		{-1, http.StatusInternalServerError},
	}
	for _, test := range tests {
		if got := HTTPStatusForCode(test.code); got != test.want {
			t.Errorf("HTTPStatusForCode(%d) = %d, want %d", test.code, got, test.want)
		}
	}
}
