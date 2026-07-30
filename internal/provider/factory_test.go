package provider

import (
	"strings"
	"testing"
)

func TestNewSelectsProtocolAdapter(t *testing.T) {
	base := Options{
		Name:    "test-platform",
		BaseURL: "https://example.com/v1/",
		APIKey:  "secret",
		Model:   "test-model",
	}
	tests := []struct {
		protocol string
		assert   func(*testing.T, LLMProvider)
	}{
		{
			protocol: "openai",
			assert: func(t *testing.T, llmProvider LLMProvider) {
				t.Helper()
				if _, ok := llmProvider.(*OpenAIProvider); !ok {
					t.Fatalf("provider = %T", llmProvider)
				}
			},
		},
		{
			protocol: "anthropic",
			assert: func(t *testing.T, llmProvider LLMProvider) {
				t.Helper()
				if _, ok := llmProvider.(*ClaudeProvider); !ok {
					t.Fatalf("provider = %T", llmProvider)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.protocol, func(t *testing.T) {
			options := base
			options.Protocol = tt.protocol
			llmProvider, err := New(options)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			tt.assert(t, llmProvider)
		})
	}
}

func TestNewRejectsInvalidOptionsWithoutLeakingAPIKey(t *testing.T) {
	secret := "never-print-this-api-key"
	tests := []struct {
		name    string
		options Options
		want    string
	}{
		{
			name:    "empty API key",
			options: Options{Name: "x", Protocol: "openai", BaseURL: "https://example.com/", Model: "m"},
			want:    "apiKey",
		},
		{
			name:    "empty model",
			options: Options{Name: "x", Protocol: "openai", BaseURL: "https://example.com/", APIKey: secret},
			want:    "model",
		},
		{
			name:    "empty URL",
			options: Options{Name: "x", Protocol: "openai", APIKey: secret, Model: "m"},
			want:    "baseURL",
		},
		{
			name:    "unsupported protocol",
			options: Options{Name: "x", Protocol: "other", BaseURL: "https://example.com/", APIKey: secret, Model: "m"},
			want:    "protocol",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.options)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("New() error = %v, want containing %q", err, tt.want)
			}
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("New() error leaks API key: %v", err)
			}
		})
	}
}
