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
	// TTFTMS 是首个非空 Text Delta 的延迟毫秒数；nil 表示未观测到
	// Text Delta（如纯 Tool Call 响应），0 表示已观测但不足 1ms（设计 §9.1）。
	TTFTMS *int64 `json:"ttft_ms,omitempty"`
	// CostQuality 表示成本可信度（§9.1）：exact 表示分项足以按配置价格
	// 重算；estimated 不能混入精确成本报表。缺省空值按 estimated 处理。
	CostQuality CostQuality `json:"cost_quality,omitempty"`

	// 阶段 4（§9.1）：缓存与推理分项。口径：Input 是总输入，Cache Read/Write
	// 是其子集；Output 是总输出，Reasoning 是其子集。
	// CacheReadTokens 是缓存读取令牌数。
	CacheReadTokens int64 `json:"cache_read_tokens,omitempty"`
	// CacheWriteTokens 是缓存写入令牌数。
	CacheWriteTokens int64 `json:"cache_write_tokens,omitempty"`
	// ReasoningTokens 是推理令牌数。
	ReasoningTokens int64 `json:"reasoning_tokens,omitempty"`
	// CacheReadPriceUSDPerMillionTokens 是每百万缓存读取令牌的美元价格。
	CacheReadPriceUSDPerMillionTokens float64 `json:"cache_read_price_usd_per_million_tokens,omitempty"`
	// CacheWritePriceUSDPerMillionTokens 是每百万缓存写入令牌的美元价格。
	CacheWritePriceUSDPerMillionTokens float64 `json:"cache_write_price_usd_per_million_tokens,omitempty"`
}

// CostQuality 是成本可信度枚举（设计 §9.1）。
type CostQuality string

const (
	// CostQualityExact 表示 Provider 分项足以按配置价格重算成本。
	CostQualityExact CostQuality = "exact"
	// CostQualityEstimated 表示成本只能估算。
	CostQualityEstimated CostQuality = "estimated"
)

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
	// §9.1 校验规则：Token 非负、子集口径、价格有限且落在账本范围内。
	if usage.InputTokens < 0 || usage.OutputTokens < 0 ||
		usage.CacheReadTokens < 0 || usage.CacheWriteTokens < 0 || usage.ReasoningTokens < 0 {
		return errors.New("usage tokens must be non-negative")
	}
	if usage.CacheReadTokens+usage.CacheWriteTokens > usage.InputTokens {
		return errors.New("usage cache tokens must be a subset of input tokens")
	}
	if usage.ReasoningTokens > usage.OutputTokens {
		return errors.New("usage reasoning tokens must be a subset of output tokens")
	}
	if usage.LatencyMS < 0 {
		return errors.New("usage latency must be non-negative")
	}

	if invalidUsageDecimal(usage.InputPriceUSDPerMillionTokens) ||
		invalidUsageDecimal(usage.OutputPriceUSDPerMillionTokens) ||
		invalidUsageDecimal(usage.CacheReadPriceUSDPerMillionTokens) ||
		invalidUsageDecimal(usage.CacheWritePriceUSDPerMillionTokens) ||
		invalidUsageDecimal(usage.CostUSD) {
		return errors.New("usage prices and cost are outside the supported range")
	}

	expectedCost := ExpectedCostUSD(*usage)
	if math.Abs(usage.CostUSD-expectedCost) > 1e-12 {
		return errors.New("usage cost does not match token prices")
	}

	return nil
}

// ExpectedCostUSD 按 §9.1 的成本公式重算：
//
//	normal_input = input - cache_read - cache_write
//	cost = (normal_input×input_price + cache_read×cache_read_price
//		+ cache_write×cache_write_price + output×output_price) / 1e6
func ExpectedCostUSD(usage Usage) float64 {
	normalInput := usage.InputTokens - usage.CacheReadTokens - usage.CacheWriteTokens
	return (float64(normalInput)*usage.InputPriceUSDPerMillionTokens +
		float64(usage.CacheReadTokens)*usage.CacheReadPriceUSDPerMillionTokens +
		float64(usage.CacheWriteTokens)*usage.CacheWritePriceUSDPerMillionTokens +
		float64(usage.OutputTokens)*usage.OutputPriceUSDPerMillionTokens) / 1_000_000
}

// ValidUsageDecimal 判断价格或单次调用成本是否落在账本支持的数值范围内。
func ValidUsageDecimal(value float64) bool {
	return !invalidUsageDecimal(value)
}

func invalidUsageDecimal(value float64) bool {
	return value < 0 || value >= MaxUsageDecimalExclusive || math.IsNaN(value) || math.IsInf(value, 0)
}
