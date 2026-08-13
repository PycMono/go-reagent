package errors

import (
	stderrors "errors"
	"fmt"
	"testing"
)

func TestConversationErrorsHaveStableIdentityAndClassification(t *testing.T) {
	tests := []struct {
		name    string
		err     CodeError
		code    int
		message string
		biz     bool
	}{
		{name: "not found", err: ErrConversationNotFound, code: 10101, message: "conversation not found", biz: true},
		{name: "conflict", err: ErrConversationConflict, code: 10102, message: "conversation version conflict", biz: true},
		{name: "corrupt message", err: ErrConversationCorruptMessage, code: 10103, message: "conversation message is corrupt"},
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

	if stderrors.Is(ErrConversationNotFound, ErrConversationConflict) ||
		stderrors.Is(ErrConversationNotFound, ErrConversationCorruptMessage) ||
		stderrors.Is(ErrConversationConflict, ErrConversationCorruptMessage) {
		t.Fatal("conversation errors must have distinct identities")
	}
}

func TestConversationErrorWrapPreservesIdentityAndCause(t *testing.T) {
	cause := fmt.Errorf("invalid JSON payload")
	wrapper := ErrConversationCorruptMessage.Wrap(cause)

	if !stderrors.Is(wrapper, ErrConversationCorruptMessage) {
		t.Fatal("wrapped error lost conversation error identity")
	}
	if !stderrors.Is(wrapper, cause) {
		t.Fatal("wrapped error lost its cause")
	}
	if _, ok := AsSysError(wrapper); !ok {
		t.Fatal("wrapped corrupt-message error must remain a system error")
	}
}
