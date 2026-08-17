package observability

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	logsdk "github.com/PycMono/go-logger-sdk"
	"github.com/PycMono/go-reagent/pi/ai"
	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
)

// Pricing is an immutable USD-per-million-token price snapshot.
type Pricing struct {
	InputUSDPerMillionTokens  float64
	OutputUSDPerMillionTokens float64
}

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
	case invalidPrice(pricing.InputUSDPerMillionTokens):
		return nil, errors.New("model cost tracker: input price is outside the supported range")
	case invalidPrice(pricing.OutputUSDPerMillionTokens):
		return nil, errors.New("model cost tracker: output price is outside the supported range")
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
}

func (s *trackingStream) Next() bool {
	if !s.next.Next() {
		return false
	}
	s.current = s.next.Current()
	return true
}

func (s *trackingStream) Current() ai.StreamEvent { return s.current }

func (s *trackingStream) Result() (*ai.Message, error) {
	if s.resolved {
		return s.response, s.err
	}
	s.resolved = true
	response, err := s.next.Result()
	s.response, s.err = s.tracker.meter(s.ctx, s.startedAt, response, err)
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
	cost := (float64(usage.InputTokens)*t.pricing.InputUSDPerMillionTokens +
		float64(usage.OutputTokens)*t.pricing.OutputUSDPerMillionTokens) / 1_000_000
	if invalidUsageDecimal(cost) {
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

func invalidPrice(value float64) bool {
	return invalidUsageDecimal(value)
}

func invalidUsageDecimal(value float64) bool {
	return value < 0 || value >= ai.MaxUsageDecimalExclusive || math.IsNaN(value) || math.IsInf(value, 0)
}
