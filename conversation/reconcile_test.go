package conversation

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"

	sdkmetrics "github.com/PycMono/go-observability-sdk/metrics"
	conversationentity "github.com/PycMono/go-reagent/domain/entity/conversation"
	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/harness"
	piobservability "github.com/PycMono/go-reagent/pi/harness/observability"
	"go.opentelemetry.io/otel"
)

// reconProvider 返回一条完整计量的 Action 响应。
type reconProvider struct{}

func (reconProvider) Stream(context.Context, []ai.Message, []ai.ToolDefinition) ai.Stream {
	return &scriptedReconStream{}
}

type scriptedReconStream struct{ step int }

func (s *scriptedReconStream) Next() bool { s.step++; return s.step <= 2 }
func (s *scriptedReconStream) Current() ai.StreamEvent {
	if s.step == 1 {
		return ai.StreamEvent{Type: ai.StreamEventTextDelta, TextDelta: "对账"}
	}
	return ai.StreamEvent{Type: ai.StreamEventDone}
}
func (s *scriptedReconStream) Result() (*ai.Message, error) {
	return &ai.Message{
		Role:         ai.RoleAssistant,
		Content:      []ai.ContentBlock{ai.TextBlock("对账")},
		FinishReason: ai.FinishReasonStop,
		Usage: &ai.Usage{
			PlatformID: "test", Model: "fake",
			InputTokens: 100, OutputTokens: 50,
			InputPriceUSDPerMillionTokens: 1, OutputPriceUSDPerMillionTokens: 2,
			CostUSD: (100.0*1 + 50.0*2) / 1e6,
		},
	}, nil
}
func (s *scriptedReconStream) Close() error { return nil }

// reconMetrics 记录领域指标用于对账。
type reconMetrics struct {
	requests, invocations int
	costUSD               float64
	inputTokens           int64
	outputTokens          int64
}

func (m *reconMetrics) Counter(_ context.Context, name string, value float64, labels ...sdkmetrics.Label) {
	switch name {
	case piobservability.MetricModelRequests:
		m.requests++
	case piobservability.MetricModelInvocations:
		m.invocations++
	case piobservability.MetricModelCost:
		m.costUSD += value
	case piobservability.MetricModelTokens:
		for _, label := range labels {
			if label.Key == piobservability.LabelTokenType {
				if label.Value.AsString() == string(piobservability.TokenTypeInputTotal) {
					m.inputTokens += int64(value)
				} else if label.Value.AsString() == string(piobservability.TokenTypeOutputTotal) {
					m.outputTokens += int64(value)
				}
			}
		}
	}
}
func (m *reconMetrics) UpDownCounter(context.Context, string, float64, ...sdkmetrics.Label) {}
func (m *reconMetrics) Histogram(context.Context, string, float64, ...sdkmetrics.Label)     {}
func (m *reconMetrics) Timer(context.Context, string, float64, ...sdkmetrics.Label)         {}
func (m *reconMetrics) Value(context.Context, string, float64, ...sdkmetrics.Label)         {}

// TestMetricsRunTotalsLedgerReconcile 是阶段 3 的三方对账验收（§19、§20）：
// 同一 Fixture 下 Metrics、RunTotals 与 MySQL Ledger 必须一致，且 Ledger 的
// trace_id + provider_request_index 指向唯一 Provider Span。
func TestMetricsRunTotalsLedgerReconcile(t *testing.T) {
	exporter := installSpanRecorder(t)
	metrics := &reconMetrics{}
	sdkmetrics.SetDefault(sdkmetrics.NewManager(metrics))
	t.Cleanup(func() { sdkmetrics.SetDefault(nil) })

	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "AGENTS.md"), []byte("You are a test Agent."), 0o600); err != nil {
		t.Fatal(err)
	}
	toolRuntime, err := pi.NewToolRuntime(pi.ToolRuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	traced := piobservability.NewTracingProvider(reconProvider{}, "openai", "test", "fake")
	loop := pi.NewLoop(traced, pi.NewScheduler(toolRuntime, 1), false, pi.WithLoopProviderIdentity("test", "fake"))
	builder := harness.NewContextBuilder(harness.NewPromptComposer(workDir), workDir)
	agent := pi.New(builder, loop, toolRuntime)

	store := &runnerStoreFake{conversation: conversationentity.Conversation{ID: "pk-1", ConversationID: "conversation", UserID: "user", Version: 1}}
	runner := NewRunner(agent, store, 100, pi.RunLimits{})

	// Sampling 1.0：conversation.run SpanContext 有效。
	tracer := otel.Tracer("test")
	ctx, runSpan := tracer.Start(context.Background(), piobservability.SpanNameConversationRun)
	result, err := runner.Run(ctx, validConversationRunRequest(), nil)
	runSpan.End()
	if err != nil {
		t.Fatal(err)
	}

	// RunTotals 与 Metrics 对账。
	totals := result.Termination.Totals
	if metrics.requests != 1 || metrics.invocations != 1 || totals.Invocations != 1 {
		t.Fatalf("requests/invocations/totals = %d/%d/%d", metrics.requests, metrics.invocations, totals.Invocations)
	}
	if metrics.inputTokens != totals.InputTokens || metrics.outputTokens != totals.OutputTokens {
		t.Fatalf("tokens 对账失败: metrics %d/%d, totals %d/%d",
			metrics.inputTokens, metrics.outputTokens, totals.InputTokens, totals.OutputTokens)
	}
	if math.Abs(metrics.costUSD-totals.CostUSD) > 1e-12 {
		t.Fatalf("cost 对账失败: metrics %v, totals %v", metrics.costUSD, totals.CostUSD)
	}

	// Ledger 与 RunTotals 对账。
	if store.appendCalls != 1 || len(store.appendedInvocations) != 1 {
		t.Fatalf("ledger append = %d/%v", store.appendCalls, store.appendedInvocations)
	}
	ledger := store.appendedInvocations[0]
	if ledger.CostUSD != totals.CostUSD || ledger.InputTokens != totals.InputTokens || ledger.OutputTokens != totals.OutputTokens {
		t.Fatalf("ledger 与 totals 不一致: %+v vs %+v", ledger, totals)
	}
	if ledger.TraceID == nil || *ledger.TraceID != runSpan.SpanContext().TraceID().String() {
		t.Fatalf("ledger trace_id 错误: %v", ledger.TraceID)
	}
	if ledger.ProviderRequestIndex == nil || *ledger.ProviderRequestIndex != 1 {
		t.Fatalf("ledger provider_request_index 错误: %v", ledger.ProviderRequestIndex)
	}
	if ledger.Outcome != conversationentity.InvocationOutcomeAccepted {
		t.Fatalf("ledger outcome = %q", ledger.Outcome)
	}

	// trace_id + provider_request_index 唯一定位 Provider Span（§10.1）。
	var providerSpans int
	for _, span := range exporter.GetSpans() {
		if span.Name != piobservability.ChatSpanName("fake") {
			continue
		}
		providerSpans++
		if span.SpanContext.TraceID() != runSpan.SpanContext().TraceID() {
			t.Fatal("Provider Span 必须与 conversation.run 同 Trace")
		}
	}
	if providerSpans != 1 {
		t.Fatalf("provider spans = %d, want 1", providerSpans)
	}
}
