package ai

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
