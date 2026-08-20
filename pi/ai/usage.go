package ai

import (
	"errors"
	"math"
	"strings"
)

// MaxUsageDecimalExclusive 是以 DECIMAL(20,12) 存储价格和单次调用成本时的上限，不包含该值本身。
const MaxUsageDecimalExclusive = 100_000_000

// Usage 保存一次模型响应的标准化令牌用量、价格、成本和延迟数据。
type Usage struct {
	// InputTokens 是输入令牌数。
	InputTokens int64 `json:"input_tokens"`
	// OutputTokens 是输出令牌数。
	OutputTokens int64 `json:"output_tokens"`
	// InputPriceUSDPerMillionTokens 是每百万输入令牌的美元价格。
	InputPriceUSDPerMillionTokens float64 `json:"input_price_usd_per_million_tokens"`
	// OutputPriceUSDPerMillionTokens 是每百万输出令牌的美元价格。
	OutputPriceUSDPerMillionTokens float64 `json:"output_price_usd_per_million_tokens"`
	// CostUSD 是本次响应的美元成本。
	CostUSD float64 `json:"cost_usd"`
	// LatencyMS 是本次响应的延迟，单位为毫秒。
	LatencyMS int64 `json:"latency_ms"`
	// PlatformID 是提供模型服务的平台标识。
	PlatformID string `json:"platform_id"`
	// Model 是生成响应的模型名称。
	Model string `json:"model"`
}

// ValidateMetered 校验一次已完成且可计量的模型调用所需的完整台账数据：
// 归属（平台、模型）、非负数值、价格在账本范围内，以及成本与 token 单价一致。
func (usage *Usage) ValidateMetered() error {
	if usage == nil {
		return errors.New("usage is required")
	}
	if strings.TrimSpace(usage.PlatformID) == "" {
		return errors.New("usage platform ID is required")
	}
	if strings.TrimSpace(usage.Model) == "" {
		return errors.New("usage model is required")
	}
	if usage.InputTokens < 0 || usage.OutputTokens < 0 {
		return errors.New("usage tokens must be non-negative")
	}
	if usage.LatencyMS < 0 {
		return errors.New("usage latency must be non-negative")
	}

	if invalidUsageDecimal(usage.InputPriceUSDPerMillionTokens) ||
		invalidUsageDecimal(usage.OutputPriceUSDPerMillionTokens) ||
		invalidUsageDecimal(usage.CostUSD) {
		return errors.New("usage prices and cost are outside the supported range")
	}

	expectedCost := (float64(usage.InputTokens)*usage.InputPriceUSDPerMillionTokens +
		float64(usage.OutputTokens)*usage.OutputPriceUSDPerMillionTokens) / 1_000_000
	if math.Abs(usage.CostUSD-expectedCost) > 1e-12 {
		return errors.New("usage cost does not match token prices")
	}

	return nil
}

// ValidUsageDecimal 判断价格或单次调用成本是否落在账本支持的数值范围内。
func ValidUsageDecimal(value float64) bool {
	return !invalidUsageDecimal(value)
}

func invalidUsageDecimal(value float64) bool {
	return value < 0 || value >= MaxUsageDecimalExclusive || math.IsNaN(value) || math.IsInf(value, 0)
}
