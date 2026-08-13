package providers_test

import (
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/pi/ai/providers"
)

func TestOptionsNormalizeAndValidateNormalizesFields(t *testing.T) {
	opts := providers.Options{
		ID:       " platform ",
		Protocol: providers.ProtocolOpenAI,
		BaseURL:  " https://example.com/v1/ ",
		APIKey:   " secret ",
		Model:    " model ",
	}

	if err := opts.NormalizeAndValidate(); err != nil {
		t.Fatalf("NormalizeAndValidate() error = %v", err)
	}
	if opts.ID != "platform" || opts.BaseURL != "https://example.com/v1/" || opts.APIKey != "secret" || opts.Model != "model" {
		t.Fatalf("normalized options = %#v", opts)
	}
}

func TestOptionsNormalizeAndValidateRejectsMissingProviderFields(t *testing.T) {
	tests := []struct {
		name string
		opts providers.Options
		want string
	}{
		{
			name: "API key",
			opts: providers.Options{Protocol: providers.ProtocolOpenAI, BaseURL: "https://example.com/", APIKey: " ", Model: "model"},
			want: "apiKey",
		},
		{
			name: "model",
			opts: providers.Options{Protocol: providers.ProtocolOpenAI, BaseURL: "https://example.com/", APIKey: "key", Model: " "},
			want: "model",
		},
		{
			name: "base URL",
			opts: providers.Options{Protocol: providers.ProtocolOpenAI, BaseURL: " ", APIKey: "key", Model: "model"},
			want: "baseURL",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.opts.NormalizeAndValidate()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("NormalizeAndValidate() error = %v, want containing %q", err, test.want)
			}
		})
	}
}
