package pi

import (
	"math"
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/pi/ai"
)

func TestPlatformPricingValidation(t *testing.T) {
	tests := []struct {
		name    string
		pricing *ai.PricingConfig
	}{
		{name: "missing"},
		{name: "negative input", pricing: &ai.PricingConfig{InputUSDPerMillionTokens: -1}},
		{name: "negative output", pricing: &ai.PricingConfig{OutputUSDPerMillionTokens: -1}},
		{name: "NaN input", pricing: &ai.PricingConfig{InputUSDPerMillionTokens: math.NaN()}},
		{name: "infinite output", pricing: &ai.PricingConfig{OutputUSDPerMillionTokens: math.Inf(1)}},
		{name: "input outside ledger range", pricing: &ai.PricingConfig{InputUSDPerMillionTokens: 100_000_000}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			platform := ai.PlatformConfig{ID: "x", Protocol: ai.ProtocolOpenAI, BaseURL: "https://x.test/", APIKey: "k", Model: "m", Pricing: test.pricing}
			if err := validatePlatform(&platform, 0); err == nil || !strings.Contains(err.Error(), "pricing") {
				t.Fatalf("validatePlatform() error = %v", err)
			}
		})
	}
}

func TestPlatformPricingAllowsFreeModel(t *testing.T) {
	platform := ai.PlatformConfig{ID: "x", Protocol: ai.ProtocolOpenAI, BaseURL: "https://x.test/", APIKey: "k", Model: "m", Pricing: &ai.PricingConfig{}}
	if err := validatePlatform(&platform, 0); err != nil {
		t.Fatalf("validatePlatform() error = %v", err)
	}
}

func TestCloneConfigCopiesPricing(t *testing.T) {
	input := &Config{Platforms: []PlatformConfig{{Pricing: &ai.PricingConfig{InputUSDPerMillionTokens: 0.15}}}}
	cloned := cloneConfig(input)
	input.Platforms[0].Pricing.InputUSDPerMillionTokens = 9
	if cloned.Platforms[0].Pricing.InputUSDPerMillionTokens != 0.15 {
		t.Fatalf("cloned pricing mutated: %#v", cloned.Platforms[0].Pricing)
	}
}
