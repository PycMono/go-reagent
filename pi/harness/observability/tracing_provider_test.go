package observability

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/PycMono/go-reagent/pi/ai"
	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// ---------- 测试装置 ----------

type fakeRawStream struct {
	events      []ai.StreamEvent
	result      *ai.Message
	err         error
	index       int
	closed      int
	resultCalls int
}

func (s *fakeRawStream) Next() bool {
	if s.index >= len(s.events) {
		return false
	}
	s.index++
	return true
}
func (s *fakeRawStream) Current() ai.StreamEvent { return s.events[s.index-1] }
func (s *fakeRawStream) Result() (*ai.Message, error) {
	s.resultCalls++
	return s.result, s.err
}
func (s *fakeRawStream) Close() error { s.closed++; return nil }

type fakeRawProvider struct{ stream *fakeRawStream }

func (p *fakeRawProvider) Stream(context.Context, []ai.Message, []ai.ToolDefinition) ai.Stream {
	return p.stream
}

func meteredMessage(text string) *ai.Message {
	return &ai.Message{
		Role:         ai.RoleAssistant,
		Content:      []ai.ContentBlock{ai.TextBlock(text)},
		FinishReason: ai.FinishReasonStop,
		Usage:        &ai.Usage{InputTokens: 10, OutputTokens: 5},
	}
}

func installTracer(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		_ = provider.Shutdown(t.Context())
	})
	return exporter
}

func onlySpan(t *testing.T, exporter *tracetest.InMemoryExporter) tracetest.SpanStub {
	t.Helper()
	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("spans = %d, want 1: %v", len(spans), spans)
	}
	return spans[0]
}

func spanAttr(span tracetest.SpanStub, key string) any {
	for _, attr := range span.Attributes {
		if string(attr.Key) == key {
			return attr.Value.AsInterface()
		}
	}
	return nil
}

func newTestChain(stream *fakeRawStream) (*TracingProvider, *CostTracker) {
	tracker, err := NewCostTracker(&fakeRawProvider{stream: stream}, "test", "test-model", Pricing{})
	if err != nil {
		panic(err)
	}
	return NewTracingProvider(tracker, "openai", "test", "test-model"), tracker
}

func hintedContext() context.Context {
	return WithGenerationHint(context.Background(), GenerationHint{
		Phase: string(GenerationPhaseAction), Attempt: 2, RequestIndex: 3,
	})
}

// ---------- 生命周期（§5） ----------

func TestTracingProviderSuccessSpan(t *testing.T) {
	exporter := installTracer(t)
	stream := &fakeRawStream{
		events: []ai.StreamEvent{
			{Type: ai.StreamEventStart},
			{Type: ai.StreamEventTextDelta, TextDelta: "你"},
			{Type: ai.StreamEventTextDelta, TextDelta: "好"},
			{Type: ai.StreamEventDone},
		},
		result: meteredMessage("你好"),
	}
	provider, _ := newTestChain(stream)

	s := provider.Stream(hintedContext(), nil, nil)
	for s.Next() {
	}
	message, err := s.Result()
	if err != nil || message == nil {
		t.Fatalf("Result() = (%v, %v)", message, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	span := onlySpan(t, exporter)
	if span.Name != "chat test-model" || span.SpanKind != trace.SpanKindClient {
		t.Fatalf("span = %q/%v", span.Name, span.SpanKind)
	}
	for key, want := range map[string]any{
		AttrGenAIOperationName:     "chat",
		AttrGenAIProviderName:      "openai",
		AttrGenAIRequestModel:      "test-model",
		AttrGenerationPhase:        "action",
		AttrProviderAttempt:        int64(2),
		AttrProviderRequestIndex:   int64(3),
		AttrGenAIUsageInputTokens:  int64(10),
		AttrGenAIUsageOutputTokens: int64(5),
		AttrStreamChunkCount:       int64(4),
		AttrGenAIResponseFinishRsn: []string{"stop"},
	} {
		got := spanAttr(span, key)
		if got == nil {
			t.Errorf("缺少属性 %q", key)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("属性 %q = %v (%T)，期望 %v (%T)", key, got, got, want, want)
		}
	}
	// TTFT 与 CostTracker Snapshot 同源：首个非空 Text Delta 后写入整数毫秒。
	if spanAttr(span, AttrStreamTTFTMS) == nil {
		t.Error("缺少 TTFT 属性")
	}
	if spanAttr(span, AttrInvocationCostUSD) == nil {
		t.Error("可信 Usage 必须写成本")
	}
	if span.Status.Code == codes.Error {
		t.Error("成功 Span 不应为 Error")
	}
}

func TestTracingProviderFailureSpanKeepsNoUsage(t *testing.T) {
	exporter := installTracer(t)
	stream := &fakeRawStream{err: pierrors.Wrap(pierrors.ErrorCodeAITransient, "test", errors.New("boom"))}
	provider, _ := newTestChain(stream)

	s := provider.Stream(hintedContext(), nil, nil)
	for s.Next() {
	}
	if _, err := s.Result(); err == nil {
		t.Fatal("期望错误")
	}
	s.Close()

	span := onlySpan(t, exporter)
	if span.Status.Code != codes.Error || span.Status.Description != string(pierrors.ErrorCodeAITransient) {
		t.Fatalf("status = %v/%q", span.Status.Code, span.Status.Description)
	}
	if spanAttr(span, AttrErrorType) != string(pierrors.ErrorCodeAITransient) ||
		spanAttr(span, AttrReagentErrorCode) != string(pierrors.ErrorCodeAITransient) {
		t.Fatalf("错误分类属性缺失: %v", span.Attributes)
	}
	if spanAttr(span, AttrGenAIUsageInputTokens) != nil || spanAttr(span, AttrInvocationCostUSD) != nil {
		t.Fatal("失败请求不得写 Token/成本")
	}
}

func TestTracingProviderCloseBeforeResultEndsOnce(t *testing.T) {
	exporter := installTracer(t)
	stream := &fakeRawStream{events: []ai.StreamEvent{{Type: ai.StreamEventStart}}, result: meteredMessage("x")}
	provider, _ := newTestChain(stream)

	s := provider.Stream(context.Background(), nil, nil)
	s.Next()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	// 提前 Close 后再 Result：Span 只结束一次（§5 sync.Once）。
	s.Result()
	s.Close()

	span := onlySpan(t, exporter)
	if span.Status.Code != codes.Error || span.Status.Description != "unknown" {
		// abandoned 映射为 error/unknown（errStreamAbandoned 无项目错误码）。
		t.Fatalf("abandoned status = %v/%q", span.Status.Code, span.Status.Description)
	}
	if stream.closed != 2 || stream.resultCalls != 1 {
		t.Fatalf("下层 Close/Result 调用 = %d/%d", stream.closed, stream.resultCalls)
	}
}

func TestTracingProviderMissingHintUsesDefaults(t *testing.T) {
	exporter := installTracer(t)
	stream := &fakeRawStream{result: meteredMessage("x")}
	provider, _ := newTestChain(stream)

	s := provider.Stream(context.Background(), nil, nil)
	s.Result()
	s.Close()

	span := onlySpan(t, exporter)
	if spanAttr(span, AttrGenerationPhase) != string(GenerationPhaseUnknown) ||
		spanAttr(span, AttrProviderAttempt) != int64(1) {
		t.Fatalf("hint 缺失兜底错误: %v", span.Attributes)
	}
	if spanAttr(span, AttrProviderRequestIndex) != nil {
		t.Fatal("hint 缺失时必须省略 Request Index")
	}
}

func TestTracingProviderPureToolCallOmitsTTFT(t *testing.T) {
	exporter := installTracer(t)
	stream := &fakeRawStream{
		events: []ai.StreamEvent{{Type: ai.StreamEventToolCallDelta}, {Type: ai.StreamEventDone}},
		result: meteredMessage(""),
	}
	provider, _ := newTestChain(stream)

	s := provider.Stream(hintedContext(), nil, nil)
	for s.Next() {
	}
	s.Result()
	s.Close()

	span := onlySpan(t, exporter)
	if spanAttr(span, AttrStreamTTFTMS) != nil {
		t.Fatal("纯 Tool Call 响应必须省略 TTFT")
	}
}
