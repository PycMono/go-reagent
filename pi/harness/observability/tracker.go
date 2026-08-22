package observability

import (
	"context"
	"errors"
	"strings"
	"time"

	logsdk "github.com/PycMono/go-logger-sdk"
	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/ai/providers"
	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
)

// Pricing 即 providers.Pricing（类型别名）：价格结构与校验统一由
// pi/ai/providers 的 Options/Pricing 拥有，CostTracker 不再复制定义。
// 缓存价格为 0 的语义：Provider 上报缓存 Token 而对应价格为 0（未配置或
// 免费无法区分）时，该次调用保守标记 estimated，不冒充精确成本（§9.1）。
type Pricing = providers.Pricing

// CostTracker enriches every successful model response with trustworthy cost metrics.
type CostTracker struct {
	next       ai.Provider
	platformID string
	model      string
	pricing    Pricing
	now        func() time.Time
}

// NewCostTracker constructs a strict, concurrency-safe model cost decorator.
func NewCostTracker(next ai.Provider, platformID, model string, pricing Pricing) (*CostTracker, error) {
	return newCostTracker(next, platformID, model, pricing, time.Now)
}

func newCostTracker(
	next ai.Provider,
	platformID string,
	model string,
	pricing Pricing,
	now func() time.Time,
) (*CostTracker, error) {
	platformID = strings.TrimSpace(platformID)
	model = strings.TrimSpace(model)
	switch {
	case platformID == "":
		return nil, errors.New("model cost tracker: platform ID is required")
	case model == "":
		return nil, errors.New("model cost tracker: model is required")
	}
	if err := pricing.Validate(); err != nil {
		return nil, err
	}
	return &CostTracker{next: next, platformID: platformID, model: model, pricing: pricing, now: now}, nil
}

// Stream wraps one provider stream and meters its final successful response.
func (t *CostTracker) Stream(
	ctx context.Context,
	messages []ai.Message,
	tools []ai.ToolDefinition,
) ai.Stream {
	return &trackingStream{
		ctx: ctx, next: t.next.Stream(ctx, messages, tools), tracker: t, startedAt: t.now(),
	}
}

type trackingStream struct {
	ctx       context.Context
	next      ai.Stream
	tracker   *CostTracker
	startedAt time.Time
	current   ai.StreamEvent
	resolved  bool
	response  *ai.Message
	err       error

	// ttft 是首个非空 Text Delta 的 request-local Timing Snapshot（§5），
	// 经包内私有 streamTimingReader 向 TracingProvider 暴露；纯 Tool Call
	// 响应不观测。Span 整数毫秒、Histogram 秒、Ledger 共用同一 Snapshot。
	ttft    time.Duration
	hasTTFT bool
}

func (s *trackingStream) Next() bool {
	if !s.next.Next() {
		return false
	}
	s.current = s.next.Current()
	if !s.hasTTFT && s.current.Type == ai.StreamEventTextDelta && s.current.TextDelta != "" {
		s.ttft = s.tracker.now().Sub(s.startedAt)
		s.hasTTFT = true
	}
	return true
}

// StreamTTFT 实现 streamTimingReader：返回首个非空 Text Delta 的延迟
// Snapshot；未观测到时 ok=false。
func (s *trackingStream) StreamTTFT() (time.Duration, bool) {
	return s.ttft, s.hasTTFT
}

func (s *trackingStream) Current() ai.StreamEvent { return s.current }

func (s *trackingStream) Result() (*ai.Message, error) {
	if s.resolved {
		return s.response, s.err
	}
	s.resolved = true
	response, err := s.next.Result()
	s.response, s.err = s.tracker.meter(s.ctx, s.startedAt, response, err)
	// TTFT 与 Ledger 共用同一 Timing Snapshot（§5）：仅在成功且观测到
	// 非空 Text Delta 时补齐；纯 Tool Call 保持 nil，不足 1ms 为 0。
	if s.err == nil && s.response != nil && s.response.Usage != nil && s.hasTTFT {
		ttftMS := s.ttft.Milliseconds()
		s.response.Usage.TTFTMS = &ttftMS
	}
	return s.response, s.err
}

func (s *trackingStream) Close() error { return s.next.Close() }

func (t *CostTracker) meter(
	ctx context.Context,
	startedAt time.Time,
	response *ai.Message,
	err error,
) (*ai.Message, error) {
	latencyMS := t.now().Sub(startedAt).Milliseconds()
	if err != nil {
		logsdk.Error(ctx, "model invocation failed",
			logsdk.Any("component", "model_cost"),
			logsdk.Any("platform", t.platformID),
			logsdk.Any("model", t.model),
			logsdk.Any("latency_ms", latencyMS),
			logsdk.Any("error", err),
		)
		return response, err
	}
	if response == nil || response.Usage == nil {
		logsdk.Warn(ctx, "model invocation usage missing",
			logsdk.Any("component", "model_cost"),
			logsdk.Any("event", "usage_missing"),
			logsdk.Any("platform", t.platformID),
			logsdk.Any("model", t.model),
			logsdk.Any("latency_ms", latencyMS),
		)
		return nil, pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "model cost tracking", errors.New("provider response usage is required"))
	}
	if response.Usage.InputTokens < 0 || response.Usage.OutputTokens < 0 || latencyMS < 0 {
		logsdk.Warn(ctx, "model invocation usage invalid",
			logsdk.Any("component", "model_cost"),
			logsdk.Any("event", "usage_invalid"),
			logsdk.Any("platform", t.platformID),
			logsdk.Any("model", t.model),
			logsdk.Any("latency_ms", latencyMS),
		)
		return nil, pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "model cost tracking", errors.New("provider response usage must be non-negative"))
	}

	result := *response
	usage := *response.Usage
	usage.InputPriceUSDPerMillionTokens = t.pricing.InputUSDPerMillionTokens
	usage.OutputPriceUSDPerMillionTokens = t.pricing.OutputUSDPerMillionTokens
	usage.CacheReadPriceUSDPerMillionTokens = t.pricing.CacheReadUSDPerMillionTokens
	usage.CacheWritePriceUSDPerMillionTokens = t.pricing.CacheWriteUSDPerMillionTokens
	// §9.1 成本公式：normal_input = input - cache_read - cache_write。
	cost := ai.ExpectedCostUSD(usage)
	if !ai.ValidUsageDecimal(cost) {
		logsdk.Warn(ctx, "model invocation cost invalid",
			logsdk.Any("component", "model_cost"),
			logsdk.Any("event", "usage_invalid"),
			logsdk.Any("platform", t.platformID),
			logsdk.Any("model", t.model),
			logsdk.Any("latency_ms", latencyMS),
		)
		return nil, pierrors.Wrap(pierrors.ErrorCodeAIGeneration, "model cost tracking", errors.New("calculated model cost is outside the supported range"))
	}
	usage.CostUSD = cost
	usage.LatencyMS = latencyMS
	usage.PlatformID = t.platformID
	usage.Model = t.model
	// §9.1：分项足以按配置价格重算时标记 exact；Provider 上报了缓存 Token
	// 而对应价格为 0（未配置与免费无法区分）时保守降级 estimated，
	// 不得混入精确报表。
	usage.CostQuality = ai.CostQualityExact
	if (usage.CacheReadTokens > 0 && t.pricing.CacheReadUSDPerMillionTokens == 0) ||
		(usage.CacheWriteTokens > 0 && t.pricing.CacheWriteUSDPerMillionTokens == 0) {
		usage.CostQuality = ai.CostQualityEstimated
	}
	result.Usage = &usage

	logsdk.Info(ctx, "model invocation metered",
		logsdk.Any("component", "model_cost"),
		logsdk.Any("platform", usage.PlatformID),
		logsdk.Any("model", usage.Model),
		logsdk.Any("input_tokens", usage.InputTokens),
		logsdk.Any("output_tokens", usage.OutputTokens),
		logsdk.Any("input_price_usd_per_million_tokens", usage.InputPriceUSDPerMillionTokens),
		logsdk.Any("output_price_usd_per_million_tokens", usage.OutputPriceUSDPerMillionTokens),
		logsdk.Any("cost_usd", usage.CostUSD),
		logsdk.Any("latency_ms", usage.LatencyMS),
	)
	return &result, nil
}
