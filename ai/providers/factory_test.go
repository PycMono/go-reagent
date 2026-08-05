package providers_test

import (
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/ai"
	"github.com/PycMono/go-reagent/ai/providers"
)

func TestNewSelectsSupportedProtocol(t *testing.T) {
	base := ai.PlatformConfig{
		ID:      "test",
		BaseURL: "https://example.com/",
		APIKey:  "key",
		Model:   "model",
	}
	for _, protocol := range []ai.Protocol{ai.ProtocolOpenAI, ai.ProtocolAnthropic} {
		config := base
		config.Protocol = protocol
		client, err := providers.New(config)
		if err != nil || client == nil {
			t.Fatalf("New(%q) = %T, %v", protocol, client, err)
		}
	}
}

func TestNewRejectsInvalidConfigWithoutLeakingAPIKey(t *testing.T) {
	const secret = "never-print-this-api-key"
	tests := []struct {
		name   string
		config ai.PlatformConfig
		want   string
	}{
		{name: "empty API key", config: ai.PlatformConfig{Protocol: ai.ProtocolOpenAI, BaseURL: "https://example.com/", Model: "m"}, want: "apiKey"},
		{name: "empty model", config: ai.PlatformConfig{Protocol: ai.ProtocolOpenAI, BaseURL: "https://example.com/", APIKey: secret}, want: "model"},
		{name: "empty URL", config: ai.PlatformConfig{Protocol: ai.ProtocolOpenAI, APIKey: secret, Model: "m"}, want: "baseURL"},
		{name: "unsupported protocol", config: ai.PlatformConfig{Protocol: "other", BaseURL: "https://example.com/", APIKey: secret, Model: "m"}, want: "protocol"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := providers.New(test.config)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("New() error = %v, want containing %q", err, test.want)
			}
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("New() error leaks API key: %v", err)
			}
		})
	}
}
