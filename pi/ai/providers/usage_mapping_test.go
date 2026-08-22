package providers

import (
	"testing"

	anthropicsdk "github.com/anthropics/anthropic-sdk-go"
	openaisdk "github.com/openai/openai-go/v3"
)

// TestMapOpenAIUsage 覆盖 §9.2 OpenAI 映射：总量、缓存/推理分项。
func TestMapOpenAIUsage(t *testing.T) {
	var usage openaisdk.CompletionUsage
	if err := usage.UnmarshalJSON([]byte(`{
		"prompt_tokens": 1100, "completion_tokens": 220,
		"prompt_tokens_details": {"cached_tokens": 1000},
		"completion_tokens_details": {"reasoning_tokens": 120}
	}`)); err != nil {
		t.Fatal(err)
	}
	mapped := mapOpenAIUsage(usage)
	if mapped.InputTokens != 1100 || mapped.OutputTokens != 220 ||
		mapped.CacheReadTokens != 1000 || mapped.ReasoningTokens != 120 {
		t.Fatalf("mapOpenAIUsage = %+v", mapped)
	}

	// DeepSeek 非标字段：prompt_cache_hit_tokens → CacheReadTokens，
	// prompt_tokens 本身即 hit + miss 总量。
	var deepseek openaisdk.CompletionUsage
	if err := deepseek.UnmarshalJSON([]byte(`{
		"prompt_tokens": 1100, "completion_tokens": 220,
		"prompt_cache_hit_tokens": 900, "prompt_cache_miss_tokens": 200
	}`)); err != nil {
		t.Fatal(err)
	}
	mapped = mapOpenAIUsage(deepseek)
	if mapped.InputTokens != 1100 || mapped.CacheReadTokens != 900 {
		t.Fatalf("deepseek mapping = %+v", mapped)
	}

	// 字段缺失：分项为 0。
	var bare openaisdk.CompletionUsage
	if err := bare.UnmarshalJSON([]byte(`{"prompt_tokens": 10, "completion_tokens": 5}`)); err != nil {
		t.Fatal(err)
	}
	if mapped = mapOpenAIUsage(bare); mapped.CacheReadTokens != 0 || mapped.ReasoningTokens != 0 {
		t.Fatalf("bare mapping = %+v", mapped)
	}
}

// TestMapAnthropicUsage 覆盖 §9.2 Anthropic 映射：Input = input + read +
// creation 的总输入口径。
func TestMapAnthropicUsage(t *testing.T) {
	mapped := mapAnthropicUsage(anthropicsdk.Usage{
		InputTokens:              100,
		OutputTokens:             50,
		CacheReadInputTokens:     800,
		CacheCreationInputTokens: 200,
	})
	if mapped.InputTokens != 1100 || mapped.OutputTokens != 50 ||
		mapped.CacheReadTokens != 800 || mapped.CacheWriteTokens != 200 {
		t.Fatalf("mapAnthropicUsage = %+v", mapped)
	}
	if mapped.CacheReadTokens+mapped.CacheWriteTokens > mapped.InputTokens {
		t.Fatal("违反 §9.1 子集口径")
	}
}
