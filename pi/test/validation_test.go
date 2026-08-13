package test

import (
	"context"
	"errors"
	"testing"

	"github.com/PycMono/go-reagent/pi"
	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
)

func TestValidateRunRequestRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name    string
		request pi.RunRequest
	}{
		{name: "empty input", request: pi.RunRequest{}},
		{name: "blank input", request: pi.RunRequest{Input: "  "}},
		{name: "empty context name", request: pi.RunRequest{Input: "hello", Context: []pi.ContextBlock{{Content: "value"}}}},
		{name: "empty context content", request: pi.RunRequest{Input: "hello", Context: []pi.ContextBlock{{Name: "profile"}}}},
		{name: "unsupported history content type", request: pi.RunRequest{Input: "hello", History: []pi.HistoryMessage{{
			ContentType: "image", SenderType: pi.HistorySenderTypeCustomer, Content: "content",
		}}}},
		{name: "unsupported history sender type", request: pi.RunRequest{Input: "hello", History: []pi.HistoryMessage{{
			ContentType: pi.HistoryContentTypeText, SenderType: "system", Content: "content",
		}}}},
		{name: "empty history content", request: pi.RunRequest{Input: "hello", History: []pi.HistoryMessage{{
			ContentType: pi.HistoryContentTypeText, SenderType: pi.HistorySenderTypeAI, Content: "  ",
		}}}},
	}
	runtime := newPublicAgent(t, &agentProviderFake{}, &agentToolRuntimeFake{})

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := runtime.Run(context.Background(), test.request, nil); !errors.Is(err, pierrors.ErrRequestInvalid) {
				t.Fatalf("Agent.Run() error = %v, want ErrRequestInvalid", err)
			}
		})
	}
}
