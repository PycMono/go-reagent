package config

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/pi/ai/providers"
)

func TestSavedModelUsesCurrentSecretButOriginalBehavior(t *testing.T) {
	cfg := &Config{CurrentPlatform: "p", Platforms: []providers.Options{{ID: "p", Protocol: providers.ProtocolOpenAI, BaseURL: "https://example.invalid/v1/", APIKey: "secret-before", Model: "model-before", Pricing: &providers.Pricing{InputUSDPerMillionTokens: 1}}}}
	snap, err := cfg.CaptureAgentSnapshot("p", "model-before", []string{"current_time"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(snap)
	if strings.Contains(string(raw), "secret-before") || strings.Contains(string(raw), "apiKey") {
		t.Fatal("credential body in snapshot")
	}
	cfg.Platforms[0].APIKey = "secret-after"
	cfg.Platforms[0].Model = "model-after"
	cfg.Platforms[0].BaseURL = "https://changed.invalid/"
	cfg.Platforms[0].Pricing.InputUSDPerMillionTokens = 99
	got, err := cfg.ResolveAgentModel(snap.Model)
	if err != nil {
		t.Fatal(err)
	}
	if got.APIKey != "secret-after" || got.Model != "model-before" || got.BaseURL != "https://example.invalid/v1/" || got.Pricing.InputUSDPerMillionTokens != 1 {
		t.Fatalf("effective behavior was not frozen: model=%s endpoint=%s price=%v", got.Model, got.BaseURL, got.Pricing)
	}
	cfg.Platforms = nil
	if _, err := cfg.ResolveAgentModel(snap.Model); err == nil {
		t.Fatal("removed provider accepted")
	}
}
