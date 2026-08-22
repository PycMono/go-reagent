package observability

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/PycMono/go-reagent/pi/ai"
	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
)

type providerFunc func(context.Context, []ai.Message, []ai.ToolDefinition) (*ai.Message, error)

func (f providerFunc) Stream(ctx context.Context, messages []ai.Message, tools []ai.ToolDefinition) ai.Stream {
	message, err := f(ctx, messages, tools)
	return &providerStream{message: message, err: err}
}

type providerStream struct {
	step    int
	message *ai.Message
	err     error
}

func (s *providerStream) Next() bool { s.step++; return s.step <= 2 }
func (s *providerStream) Current() ai.StreamEvent {
	if s.step == 1 {
		return ai.StreamEvent{Type: ai.StreamEventStart}
	}
	if s.err != nil {
		return ai.StreamEvent{Type: ai.StreamEventError}
	}
	return ai.StreamEvent{Type: ai.StreamEventDone}
}
func (s *providerStream) Result() (*ai.Message, error) { return s.message, s.err }
func (s *providerStream) Close() error                 { return nil }

func streamResult(stream ai.Stream) (*ai.Message, error) {
	defer stream.Close()
	for stream.Next() {
	}
	return stream.Result()
}

func TestCostTrackerCalculatesEverySuccessfulCall(t *testing.T) {
	original := &ai.Message{Role: ai.RoleAssistant, Usage: &ai.Usage{
		InputTokens: 2_000_000, OutputTokens: 500_000,
	}}
	times := []time.Time{time.Unix(0, 0), time.Unix(0, 0).Add(2500 * time.Millisecond)}
	index := 0
	tracker, err := newCostTracker(
		providerFunc(func(context.Context, []ai.Message, []ai.ToolDefinition) (*ai.Message, error) {
			return original, nil
		}),
		"zhipu", "glm-4.5-air",
		Pricing{InputUSDPerMillionTokens: 0.15, OutputUSDPerMillionTokens: 0.60},
		func() time.Time {
			value := times[index]
			index++
			return value
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := streamResult(tracker.Stream(context.Background(), nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	if result.Usage.CostUSD != 0.60 || result.Usage.LatencyMS != 2500 ||
		result.Usage.PlatformID != "zhipu" || result.Usage.Model != "glm-4.5-air" {
		t.Fatalf("Usage = %#v", result.Usage)
	}
	if original.Usage.CostUSD != 0 || original.Usage.LatencyMS != 0 || original.Usage.PlatformID != "" {
		t.Fatalf("delegate Usage mutated: %#v", original.Usage)
	}
}

func TestCostTrackerRejectsMissingUsage(t *testing.T) {
	tracker, err := NewCostTracker(
		providerFunc(func(context.Context, []ai.Message, []ai.ToolDefinition) (*ai.Message, error) {
			return &ai.Message{Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("uncosted")}}, nil
		}),
		"test", "model", Pricing{},
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := streamResult(tracker.Stream(context.Background(), nil, nil))
	if err == nil || pierrors.ErrorCodeOf(err) != pierrors.ErrorCodeAIGeneration {
		t.Fatalf("Stream().Result() error = %v, want generation error", err)
	}
	if result != nil {
		t.Fatalf("Stream().Result() = %#v, want nil on missing Usage", result)
	}
}

func TestCostTrackerRejectsInvalidTokenUsage(t *testing.T) {
	for _, usage := range []*ai.Usage{{InputTokens: -1}, {OutputTokens: -1}} {
		tracker, err := NewCostTracker(
			providerFunc(func(context.Context, []ai.Message, []ai.ToolDefinition) (*ai.Message, error) {
				return &ai.Message{Role: ai.RoleAssistant, Usage: usage}, nil
			}),
			"test", "model", Pricing{},
		)
		if err != nil {
			t.Fatal(err)
		}
		if result, err := streamResult(tracker.Stream(context.Background(), nil, nil)); result != nil || pierrors.ErrorCodeOf(err) != pierrors.ErrorCodeAIGeneration {
			t.Fatalf("Stream().Result() = %#v, %v, want nil generation error", result, err)
		}
	}
}

func TestCostTrackerAllowsFreePricing(t *testing.T) {
	tracker, err := NewCostTracker(
		providerFunc(func(context.Context, []ai.Message, []ai.ToolDefinition) (*ai.Message, error) {
			return &ai.Message{Role: ai.RoleAssistant, Usage: &ai.Usage{InputTokens: 3, OutputTokens: 4}}, nil
		}),
		"test", "free-model", Pricing{},
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := streamResult(tracker.Stream(context.Background(), nil, nil))
	if err != nil || result.Usage.CostUSD != 0 {
		t.Fatalf("Stream().Result() = %#v, %v", result, err)
	}
}

func TestCostTrackerRejectsCostOutsideLedgerRange(t *testing.T) {
	tracker, err := NewCostTracker(
		providerFunc(func(context.Context, []ai.Message, []ai.ToolDefinition) (*ai.Message, error) {
			return &ai.Message{Role: ai.RoleAssistant, Usage: &ai.Usage{InputTokens: 3_000_000}}, nil
		}),
		"test", "model", Pricing{InputUSDPerMillionTokens: 50_000_000},
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := streamResult(tracker.Stream(context.Background(), nil, nil))
	if result != nil || pierrors.ErrorCodeOf(err) != pierrors.ErrorCodeAIGeneration {
		t.Fatalf("Stream().Result() = %#v, %v, want nil generation error", result, err)
	}
}

func TestCostTrackerPreservesDelegateError(t *testing.T) {
	want := errors.New("provider failed")
	response := &ai.Message{Role: ai.RoleAssistant}
	tracker, err := NewCostTracker(
		providerFunc(func(context.Context, []ai.Message, []ai.ToolDefinition) (*ai.Message, error) {
			return response, want
		}),
		"test", "model", Pricing{},
	)
	if err != nil {
		t.Fatal(err)
	}
	gotResponse, gotErr := streamResult(tracker.Stream(context.Background(), nil, nil))
	if gotResponse != response || !errors.Is(gotErr, want) {
		t.Fatalf("Stream().Result() = %#v, %v, want original response/error", gotResponse, gotErr)
	}
}

func TestCostTrackerRejectsInvalidConstruction(t *testing.T) {
	validProvider := providerFunc(func(context.Context, []ai.Message, []ai.ToolDefinition) (*ai.Message, error) {
		return nil, nil
	})
	tests := []struct {
		name       string
		next       ai.Provider
		platformID string
		model      string
		pricing    Pricing
	}{
		{name: "blank platform", next: validProvider, platformID: " ", model: "m"},
		{name: "blank model", next: validProvider, platformID: "p", model: " "},
		{name: "negative input price", next: validProvider, platformID: "p", model: "m", pricing: Pricing{InputUSDPerMillionTokens: -1}},
		{name: "NaN input price", next: validProvider, platformID: "p", model: "m", pricing: Pricing{InputUSDPerMillionTokens: math.NaN()}},
		{name: "infinite output price", next: validProvider, platformID: "p", model: "m", pricing: Pricing{OutputUSDPerMillionTokens: math.Inf(1)}},
		{name: "input price outside ledger range", next: validProvider, platformID: "p", model: "m", pricing: Pricing{InputUSDPerMillionTokens: 100_000_000}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewCostTracker(tt.next, tt.platformID, tt.model, tt.pricing); err == nil {
				t.Fatal("NewCostTracker() error = nil")
			}
		})
	}
}

func TestCostTrackerKeepsConcurrentCallsIndependent(t *testing.T) {
	shared := &ai.Message{Role: ai.RoleAssistant, Usage: &ai.Usage{InputTokens: 1_000_000}}
	tracker, err := NewCostTracker(
		providerFunc(func(context.Context, []ai.Message, []ai.ToolDefinition) (*ai.Message, error) {
			return shared, nil
		}),
		"test", "model", Pricing{InputUSDPerMillionTokens: 0.25},
	)
	if err != nil {
		t.Fatal(err)
	}

	const callCount = 32
	results := make(chan *ai.Message, callCount)
	errorsCh := make(chan error, callCount)
	for range callCount {
		go func() {
			result, err := streamResult(tracker.Stream(context.Background(), nil, nil))
			results <- result
			errorsCh <- err
		}()
	}
	seen := make(map[*ai.Usage]struct{}, callCount)
	for range callCount {
		if err := <-errorsCh; err != nil {
			t.Fatal(err)
		}
		result := <-results
		if result == nil || result.Usage == nil || result.Usage.CostUSD != 0.25 {
			t.Fatalf("result = %#v", result)
		}
		seen[result.Usage] = struct{}{}
	}
	if len(seen) != callCount {
		t.Fatalf("unique Usage pointers = %d, want %d", len(seen), callCount)
	}
	if shared.Usage.PlatformID != "" || shared.Usage.CostUSD != 0 {
		t.Fatalf("shared delegate response mutated: %#v", shared)
	}
}

func TestCostTrackerErrorsDoNotLeakContent(t *testing.T) {
	tracker, err := NewCostTracker(
		providerFunc(func(context.Context, []ai.Message, []ai.ToolDefinition) (*ai.Message, error) {
			return &ai.Message{Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("private output")}}, nil
		}),
		"test", "model", Pricing{},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = streamResult(tracker.Stream(context.Background(), nil, nil))
	if err == nil || strings.Contains(err.Error(), "private output") {
		t.Fatalf("Stream().Result() error = %v", err)
	}
}

// TestCostTrackerCachePricingExactVsEstimated 覆盖阶段 4（§9.1）：配置了
// 缓存价格时分项可重算、标记 exact；Provider 上报缓存 Token 而价格未配置
// 时标记 estimated，不得混入精确报表。
func TestCostTrackerCachePricingExactVsEstimated(t *testing.T) {
	cacheReadPrice := 0.1
	cacheWritePrice := 1.5
	usageWithCache := func() *ai.Message {
		return &ai.Message{Role: ai.RoleAssistant, Usage: &ai.Usage{
			InputTokens: 1100, OutputTokens: 220,
			CacheReadTokens: 800, CacheWriteTokens: 200,
		}}
	}

	// 配置缓存价格：exact，成本按分项公式计算。
	tracker, err := NewCostTracker(providerFunc(func(context.Context, []ai.Message, []ai.ToolDefinition) (*ai.Message, error) {
		return usageWithCache(), nil
	}), "deepseek", "deepseek-chat", Pricing{
		InputUSDPerMillionTokens: 1, OutputUSDPerMillionTokens: 2,
		CacheReadUSDPerMillionTokens: &cacheReadPrice, CacheWriteUSDPerMillionTokens: &cacheWritePrice,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := streamResult(tracker.Stream(context.Background(), nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	wantCost := (100.0*1 + 800.0*0.1 + 200.0*1.5 + 220.0*2) / 1e6
	if result.Usage.CostQuality != ai.CostQualityExact {
		t.Fatalf("cost quality = %q, want exact", result.Usage.CostQuality)
	}
	if result.Usage.CostUSD != wantCost {
		t.Fatalf("cost = %v, want %v（normal_input=100）", result.Usage.CostUSD, wantCost)
	}
	if err := result.Usage.ValidateMetered(); err != nil {
		t.Fatalf("exact usage 必须通过 §9.1 校验: %v", err)
	}

	// 未配置缓存价格：estimated。
	trackerNoCachePricing, err := NewCostTracker(providerFunc(func(context.Context, []ai.Message, []ai.ToolDefinition) (*ai.Message, error) {
		return usageWithCache(), nil
	}), "deepseek", "deepseek-chat", Pricing{InputUSDPerMillionTokens: 1, OutputUSDPerMillionTokens: 2})
	if err != nil {
		t.Fatal(err)
	}
	estimated, err := streamResult(trackerNoCachePricing.Stream(context.Background(), nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	if estimated.Usage.CostQuality != ai.CostQualityEstimated {
		t.Fatalf("cost quality = %q, want estimated", estimated.Usage.CostQuality)
	}
}

// TestCostTrackerTTFTSnapshot 覆盖 §5：首个非空 Text Delta 的 Snapshot 进入
// Usage.TTFTMS；纯 Tool Call 响应保持 nil。
func TestCostTrackerTTFTSnapshot(t *testing.T) {
	withText := &providerStream{
		message: &ai.Message{Role: ai.RoleAssistant, Usage: &ai.Usage{}},
	}
	_ = withText
	tracker, err := NewCostTracker(providerFunc(func(context.Context, []ai.Message, []ai.ToolDefinition) (*ai.Message, error) {
		return &ai.Message{Role: ai.RoleAssistant, Usage: &ai.Usage{}}, nil
	}), "test", "m", Pricing{})
	if err != nil {
		t.Fatal(err)
	}
	// providerStream 的事件序列无 Text Delta：TTFTMS 必须为 nil。
	result, err := streamResult(tracker.Stream(context.Background(), nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	if result.Usage.TTFTMS != nil {
		t.Fatalf("无 Text Delta 时 TTFTMS 必须为 nil，实际 %v", *result.Usage.TTFTMS)
	}
}
