package conversation

import (
	"context"
	"errors"
	"testing"

	conversationentity "github.com/PycMono/go-reagent/domain/entity/conversation"
	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
	piobservability "github.com/PycMono/go-reagent/pi/harness/observability"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// installSpanRecorder 安装 In-Memory Exporter 并返回读取器，测试结束恢复
// 之前的全局 Provider。
func installSpanRecorder(t *testing.T) *tracetest.InMemoryExporter {
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

func spanStubsByName(exporter *tracetest.InMemoryExporter) map[string]tracetest.SpanStub {
	spans := make(map[string]tracetest.SpanStub)
	for _, span := range exporter.GetSpans() {
		spans[span.Name] = span
	}
	return spans
}

// TestRunnerEmitsConversationSpans 验证设计 §3：Conversation Runner 在
// conversation.run 父 Span 下产生 load_history 与 persist_turn 子 Span。
func TestRunnerEmitsConversationSpans(t *testing.T) {
	exporter := installSpanRecorder(t)
	store := &runnerStoreFake{conversation: conversationentity.Conversation{
		ID: "pk-1", ConversationID: "conversation", UserID: "user", Version: 1,
	}}
	runtime := &runnerRuntimeFake{}
	runner := NewRunner(runtime, store, 100, pi.RunLimits{})

	tracer := otel.Tracer("test")
	ctx, parent := tracer.Start(context.Background(), piobservability.SpanNameConversationRun)
	_, err := runner.Run(ctx, validConversationRunRequest(), nil)
	parent.End()
	if err != nil {
		t.Fatal(err)
	}

	spans := spanStubsByName(exporter)
	for _, name := range []string{
		piobservability.SpanNameConversationRun,
		piobservability.SpanNameConversationLoadHistory,
		piobservability.SpanNameConversationPersistTurn,
	} {
		span, ok := spans[name]
		if !ok {
			t.Fatalf("缺少 Span %q；实际: %v", name, spans)
		}
		if span.Status.Code == codes.Error {
			t.Fatalf("Span %q 不应为 Error", name)
		}
	}
	parentContext := spans[piobservability.SpanNameConversationRun].SpanContext
	if !spans[piobservability.SpanNameConversationLoadHistory].Parent.Equal(parentContext) ||
		!spans[piobservability.SpanNameConversationPersistTurn].Parent.Equal(parentContext) {
		t.Fatal("load_history/persist_turn 必须是 conversation.run 的直接子 Span")
	}
}

// TestRunnerPersistSpanRecordsError 验证 §4.9：持久化失败时 persist_turn
// Span 为 Error，描述是稳定错误码而非错误正文。
func TestRunnerPersistSpanRecordsError(t *testing.T) {
	exporter := installSpanRecorder(t)
	store := &runnerStoreFake{
		conversation: conversationentity.Conversation{ID: "pk-1", ConversationID: "conversation", UserID: "user", Version: 1},
		appendErr:    errors.New("deadlock detected in innodb"),
	}
	runtime := &runnerRuntimeFake{}
	runner := NewRunner(runtime, store, 100, pi.RunLimits{})

	_, err := runner.Run(context.Background(), validConversationRunRequest(), nil)
	if err == nil {
		t.Fatal("期望持久化错误")
	}
	persistSpan, ok := spanStubsByName(exporter)[piobservability.SpanNameConversationPersistTurn]
	if !ok {
		t.Fatal("缺少 persist_turn Span")
	}
	if persistSpan.Status.Code != codes.Error {
		t.Fatalf("persist_turn Status = %v，期望 Error", persistSpan.Status.Code)
	}
	if persistSpan.Status.Description == "" || persistSpan.Status.Description == "deadlock detected in innodb" {
		t.Fatalf("错误描述必须是稳定错误码而非正文: %q", persistSpan.Status.Description)
	}
}

// TestRunnerWritesTraceIDToLedger 验证 §10.1：conversation.run 的 SpanContext
// 有效时台账写入 TraceID；Telemetry 关闭（无有效 SpanContext）时写 NULL。
func TestRunnerWritesTraceIDToLedger(t *testing.T) {
	exporter := installSpanRecorder(t)
	_ = exporter
	store := &runnerStoreFake{conversation: conversationentity.Conversation{
		ID: "pk-1", ConversationID: "conversation", UserID: "user", Version: 1,
	}}
	runtime := &runnerRuntimeFake{result: pi.RunResult{
		Invocations: []pi.ModelInvocation{{
			Sequence: 1, Phase: pi.ModelInvocationPhaseAction,
			Outcome: pi.ModelInvocationAccepted, ProviderRequestIndex: 1,
			Usage: ai.Usage{PlatformID: "test", Model: "model"},
		}},
	}}
	runner := NewRunner(runtime, store, 100, pi.RunLimits{})

	tracer := otel.Tracer("test")
	ctx, parent := tracer.Start(context.Background(), piobservability.SpanNameConversationRun)
	_, err := runner.Run(ctx, validConversationRunRequest(), nil)
	parent.End()
	if err != nil {
		t.Fatal(err)
	}
	wantTraceID := parent.SpanContext().TraceID().String()
	got := store.appendedInvocations[0]
	if got.TraceID == nil || *got.TraceID != wantTraceID {
		t.Fatalf("trace_id = %v, want %q", got.TraceID, wantTraceID)
	}
	if got.Outcome != conversationentity.InvocationOutcomeAccepted ||
		got.ProviderRequestIndex == nil || *got.ProviderRequestIndex != 1 {
		t.Fatalf("outcome/request_index = %v/%v", got.Outcome, got.ProviderRequestIndex)
	}
	if got.TTFTMS != nil {
		t.Fatal("未观测 TTFT 必须为 NULL")
	}

	// 无 SpanContext：写 NULL。
	storeNoTrace := &runnerStoreFake{conversation: conversationentity.Conversation{
		ID: "pk-2", ConversationID: "conversation", UserID: "user", Version: 1,
	}}
	if _, err := NewRunner(runtime, storeNoTrace, 100, pi.RunLimits{}).Run(
		context.Background(), validConversationRunRequest(), nil); err != nil {
		t.Fatal(err)
	}
	if storeNoTrace.appendedInvocations[0].TraceID != nil {
		t.Fatal("无 SpanContext 时 trace_id 必须为 NULL")
	}
}

// cancelDuringRunFake 在 Run 中途取消 Context，模拟生产中的取消时机。
type cancelDuringRunFake struct {
	cancel context.CancelFunc
	result pi.RunResult
}

func (f cancelDuringRunFake) Run(ctx context.Context, _ pi.RunRequest, _ pi.Reporter) (pi.RunResult, error) {
	f.cancel()
	return f.result, ctx.Err()
}

// TestRunnerPersistsTerminalStateWithDetachedContext 验证 §10.2：Run 被取消
// 后，已有 NewMessages/Invocations 使用脱离取消的 Context 完成终态持久化。
func TestRunnerPersistsTerminalStateWithDetachedContext(t *testing.T) {
	store := &runnerStoreFake{conversation: conversationentity.Conversation{
		ID: "pk-1", ConversationID: "conversation", UserID: "user", Version: 1,
	}}
	ctx, cancel := context.WithCancel(context.Background())
	runtime := cancelDuringRunFake{
		cancel: cancel,
		result: pi.RunResult{NewMessages: []ai.Message{{
			Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("partial")},
		}}},
	}
	runner := NewRunner(runtime, store, 100, pi.RunLimits{})

	_, err := runner.Run(ctx, validConversationRunRequest(), nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v", err)
	}
	if store.appendCalls != 1 {
		t.Fatalf("取消后仍必须保存终态，appendCalls = %d", store.appendCalls)
	}
	if store.appendCtxErr != nil {
		t.Fatalf("终态持久化 Context 必须脱离取消，ctx.Err() = %v", store.appendCtxErr)
	}
}
