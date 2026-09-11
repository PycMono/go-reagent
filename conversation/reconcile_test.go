package conversation

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

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

// TestRunTotalsLedgerReconcile 验证实际模型用量与持久化账本一致，并可关联 Provider Span。
func TestRunTotalsLedgerReconcile(t *testing.T) {
	exporter := installSpanRecorder(t)

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

	totals := result.Termination.Totals
	if totals.Invocations != 1 || totals.InputTokens != 100 || totals.OutputTokens != 50 {
		t.Fatalf("unexpected totals: %+v", totals)
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
