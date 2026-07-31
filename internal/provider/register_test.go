package provider

import (
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/internal/config"
)

func TestNewLLMProviderBuildsCurrentPlatform(t *testing.T) {
	cfg := &config.Config{
		CurrentPlatform: "test-platform",
		Platforms: []config.PlatformConfig{{
			ID:       "test-platform",
			Protocol: config.ProtocolOpenAI,
			BaseURL:  "https://example.com/v1/",
			APIKey:   "test-key",
			Model:    "test-model",
		}},
	}

	llmProvider, err := NewLLMProvider(cfg)
	if err != nil {
		t.Fatalf("NewLLMProvider() error = %v", err)
	}
	if llmProvider == nil {
		t.Fatal("NewLLMProvider() = nil")
	}
}

func TestNewLLMProviderRejectsMissingCurrentPlatform(t *testing.T) {
	_, err := NewLLMProvider(&config.Config{CurrentPlatform: "missing"})
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("NewLLMProvider() error = %v, want missing platform", err)
	}
}

func TestNewLLMProviderRejectsNilConfig(t *testing.T) {
	_, err := NewLLMProvider(nil)
	if err == nil || !strings.Contains(err.Error(), "配置不能为空") {
		t.Fatalf("NewLLMProvider() error = %v, want nil config error", err)
	}
}
