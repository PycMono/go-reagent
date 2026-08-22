package ai

import (
	"strings"
	"testing"
)

// TestUsageValidateMeteredSubsetRules 覆盖 §9.1 校验规则：非负、子集口径、
// 含缓存分项的成本公式与 1e-12 重算差值。
func TestUsageValidateMeteredSubsetRules(t *testing.T) {
	valid := Usage{
		PlatformID: "test", Model: "m",
		InputTokens: 1100, OutputTokens: 220,
		CacheReadTokens: 800, CacheWriteTokens: 200, ReasoningTokens: 120,
		InputPriceUSDPerMillionTokens:      1,
		OutputPriceUSDPerMillionTokens:     2,
		CacheReadPriceUSDPerMillionTokens:  0.1,
		CacheWritePriceUSDPerMillionTokens: 1.5,
		LatencyMS:                          10,
	}
	valid.CostUSD = ExpectedCostUSD(valid)
	if err := valid.ValidateMetered(); err != nil {
		t.Fatalf("valid usage rejected: %v", err)
	}
	// normal_input = 1100-800-200 = 100；cost = (100×1 + 800×0.1 + 200×1.5 + 220×2)/1e6
	want := (100.0*1 + 800.0*0.1 + 200.0*1.5 + 220.0*2) / 1e6
	if valid.CostUSD != want {
		t.Fatalf("cost = %v, want %v", valid.CostUSD, want)
	}

	tests := []struct {
		name   string
		mutate func(*Usage)
		want   string
	}{
		{"cache exceeds input", func(u *Usage) { u.CacheWriteTokens = 400 }, "subset of input"},
		{"reasoning exceeds output", func(u *Usage) { u.ReasoningTokens = 300 }, "subset of output"},
		{"negative cache", func(u *Usage) { u.CacheReadTokens = -1 }, "non-negative"},
		{"cache price out of range", func(u *Usage) { u.CacheReadPriceUSDPerMillionTokens = -0.1 }, "supported range"},
		{"cost mismatch beyond tolerance", func(u *Usage) { u.CostUSD += 1e-6 }, "does not match"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			usage := valid
			test.mutate(&usage)
			if err := usage.ValidateMetered(); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

// TestExpectedCostUSDDegeneratesWithoutCache 无缓存分项时公式退化为阶段 0–3
// 的 input×price + output×price。
func TestExpectedCostUSDDegeneratesWithoutCache(t *testing.T) {
	usage := Usage{
		InputTokens: 1000, OutputTokens: 500,
		InputPriceUSDPerMillionTokens: 0.15, OutputPriceUSDPerMillionTokens: 0.6,
	}
	want := (1000.0*0.15 + 500.0*0.6) / 1e6
	if got := ExpectedCostUSD(usage); got != want {
		t.Fatalf("ExpectedCostUSD = %v, want %v", got, want)
	}
}
