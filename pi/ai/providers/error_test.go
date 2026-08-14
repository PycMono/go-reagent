package providers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"

	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
	anthropicsdk "github.com/anthropics/anthropic-sdk-go"
	openaisdk "github.com/openai/openai-go/v3"
)

func TestClassifyErrorUsesNormalizedProviderFacts(t *testing.T) {
	tests := []struct {
		name string
		info providerErrorInfo
		want pierrors.ErrorCode
	}{
		{name: "context overflow", info: providerErrorInfo{contextOverflow: true, err: errors.New("overflow")}, want: pierrors.ErrorCodeAIContextOverflow},
		{name: "quota", info: providerErrorInfo{quotaExceeded: true, err: errors.New("quota")}, want: pierrors.ErrorCodeAIQuotaExceeded},
		{name: "rate limit", info: providerErrorInfo{statusCode: http.StatusTooManyRequests, err: errors.New("429")}, want: pierrors.ErrorCodeAIRateLimited},
		{name: "request timeout", info: providerErrorInfo{statusCode: http.StatusRequestTimeout, err: errors.New("408")}, want: pierrors.ErrorCodeAITransient},
		{name: "conflict", info: providerErrorInfo{statusCode: http.StatusConflict, err: errors.New("409")}, want: pierrors.ErrorCodeAITransient},
		{name: "server", info: providerErrorInfo{statusCode: http.StatusBadGateway, err: errors.New("502")}, want: pierrors.ErrorCodeAITransient},
		{name: "unauthorized", info: providerErrorInfo{statusCode: http.StatusUnauthorized, err: errors.New("401")}, want: pierrors.ErrorCodeAIUnauthorized},
		{name: "forbidden", info: providerErrorInfo{statusCode: http.StatusForbidden, err: errors.New("403")}, want: pierrors.ErrorCodeAIUnauthorized},
		{name: "bad request", info: providerErrorInfo{statusCode: http.StatusBadRequest, err: errors.New("400")}, want: pierrors.ErrorCodeAIInvalidRequest},
		{name: "unknown", info: providerErrorInfo{err: errors.New("unknown")}, want: pierrors.ErrorCodeAIGeneration},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyError(tt.info)
			if pierrors.ErrorCodeOf(got) != tt.want || !errors.Is(got, tt.info.err) {
				t.Fatalf("classifyError() = %v, want code %q with original cause", got, tt.want)
			}
		})
	}
}

func TestClassifyErrorUsesContextAndStandardNetworkErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want pierrors.ErrorCode
	}{
		{name: "canceled", err: context.Canceled, want: pierrors.ErrorCodeCanceled},
		{name: "deadline", err: context.DeadlineExceeded, want: pierrors.ErrorCodeDeadlineExceeded},
		{name: "DNS", err: &net.DNSError{IsTimeout: true}, want: pierrors.ErrorCodeAITransient},
		{name: "EOF", err: io.ErrUnexpectedEOF, want: pierrors.ErrorCodeAITransient},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyError(providerErrorInfo{err: tt.err})
			if pierrors.ErrorCodeOf(got) != tt.want || !errors.Is(got, tt.err) {
				t.Fatalf("classifyError() = %v, want code %q with original cause", got, tt.want)
			}
		})
	}
}

func TestOpenAIClassifyErrorExtractsOfficialSDKError(t *testing.T) {
	request, _ := http.NewRequest(http.MethodPost, "https://example.test/v1/chat/completions", nil)
	apiErr := &openaisdk.Error{
		Code:       "context_length_exceeded",
		StatusCode: http.StatusBadRequest,
		Request:    request,
		Response:   &http.Response{StatusCode: http.StatusBadRequest},
	}
	got := (&OpenAIImpl{}).classifyError(apiErr)
	if pierrors.ErrorCodeOf(got) != pierrors.ErrorCodeAIContextOverflow || !errors.Is(got, apiErr) {
		t.Fatalf("classifyError() = %v", got)
	}
}

func TestAnthropicClassifyErrorExtractsOfficialSDKError(t *testing.T) {
	request, _ := http.NewRequest(http.MethodPost, "https://example.test/v1/messages", nil)
	var apiErr anthropicsdk.Error
	if err := json.Unmarshal([]byte(`{"error":{"type":"billing_error","message":"billing"}}`), &apiErr); err != nil {
		t.Fatal(err)
	}
	apiErr.StatusCode = http.StatusBadRequest
	apiErr.Request = request
	apiErr.Response = &http.Response{StatusCode: http.StatusBadRequest}
	got := (&AnthropicImpl{}).classifyError(&apiErr)
	if pierrors.ErrorCodeOf(got) != pierrors.ErrorCodeAIQuotaExceeded || !errors.Is(got, &apiErr) {
		t.Fatalf("classifyError() = %v", got)
	}
}
