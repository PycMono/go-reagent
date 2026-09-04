package conversation

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	sdkmetrics "github.com/PycMono/go-observability-sdk/metrics"
	conversationentity "github.com/PycMono/go-reagent/domain/entity/conversation"
	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai/providers"
	"github.com/PycMono/go-reagent/pi/governor"
	piobservability "github.com/PycMono/go-reagent/pi/harness/observability"
	"go.opentelemetry.io/otel"
)

// fakeOpenAIServer 返回一条 OpenAI 兼容的流式响应：文本增量 + stop 结束 +
// 完整 Usage（input 100 / output 50），供对账断言与 Pricing 相乘。
func fakeOpenAIServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		chunks := []string{
			`{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1700000000,"model":"fake","choices":[{"index":0,"delta":{"content":"对账"}}]}`,
			`{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1700000000,"model":"fake","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
			`{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1700000000,"model":"fake","choices":[],"usage":{"prompt_tokens":100,"completion_tokens":50,"total_tokens":150}}`,
		}
		for _, chunk := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", chunk)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
}

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
	server := fakeOpenAIServer(t)
	defer server.Close()
	agent, err := pi.New(pi.Options{
		WorkDir: workDir,
		Platform: providers.Options{
			ID: "test", Protocol: providers.ProtocolOpenAI, BaseURL: server.URL,
			APIKey: "k", Model: "fake",
			Pricing: &providers.Pricing{InputUSDPerMillionTokens: 1, OutputUSDPerMillionTokens: 2},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	store := &runnerStoreFake{conversation: conversationentity.Conversation{ID: "pk-1", ConversationID: "conversation", UserID: "user", Version: 1}}
	runner := NewRunner(agent, store, 100, governor.Limits{})

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
