package providers

import "testing"

func TestProvidersCarryVisionCapability(t *testing.T) {
	base := Options{
		ID: "p", Protocol: ProtocolAnthropic, BaseURL: "https://example.com",
		APIKey: "k", Model: "m", Pricing: &Pricing{},
	}

	enabled := base
	enabled.Vision = true
	anthropic := NewAnthropic(enabled)
	impl, ok := anthropic.(*AnthropicImpl)
	if !ok || !impl.vision {
		t.Fatalf("NewAnthropic vision = %v (ok=%v), want true", impl.vision, ok)
	}
	openai := NewOpenAi(enabled)
	openaiImpl, ok := openai.(*OpenAIImpl)
	if !ok || !openaiImpl.vision {
		t.Fatalf("NewOpenAi vision = %v (ok=%v), want true", openaiImpl.vision, ok)
	}

	disabled := base
	if disabledImpl, ok := NewAnthropic(disabled).(*AnthropicImpl); !ok || disabledImpl.vision {
		t.Fatalf("default vision must be false, got %v", disabledImpl.vision)
	}
	if disabledImpl, ok := NewOpenAi(disabled).(*OpenAIImpl); !ok || disabledImpl.vision {
		t.Fatalf("default vision must be false, got %v", disabledImpl.vision)
	}
}
