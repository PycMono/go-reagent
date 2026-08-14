package providers

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"

	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
)

type providerErrorInfo struct {
	statusCode      int
	providerCode    string
	contextOverflow bool
	quotaExceeded   bool
	err             error
}

func classifyError(info providerErrorInfo) error {
	code := pierrors.ErrorCodeAIGeneration
	switch {
	case errors.Is(info.err, context.Canceled):
		code = pierrors.ErrorCodeCanceled
	case errors.Is(info.err, context.DeadlineExceeded):
		code = pierrors.ErrorCodeDeadlineExceeded
	case info.contextOverflow:
		code = pierrors.ErrorCodeAIContextOverflow
	case info.quotaExceeded:
		code = pierrors.ErrorCodeAIQuotaExceeded
	case info.statusCode == http.StatusTooManyRequests:
		code = pierrors.ErrorCodeAIRateLimited
	case info.statusCode == http.StatusRequestTimeout,
		info.statusCode == http.StatusConflict,
		info.statusCode >= http.StatusInternalServerError:
		code = pierrors.ErrorCodeAITransient
	case isTransientProviderError(info.err):
		code = pierrors.ErrorCodeAITransient
	case info.statusCode == http.StatusUnauthorized,
		info.statusCode == http.StatusForbidden:
		code = pierrors.ErrorCodeAIUnauthorized
	case info.statusCode == http.StatusBadRequest:
		code = pierrors.ErrorCodeAIInvalidRequest
	}
	return pierrors.Wrap(code, "provider generate", info.err)
}

func isTransientProviderError(err error) bool {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var networkError net.Error
	return errors.As(err, &networkError)
}
