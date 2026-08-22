package pi

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/harness"
	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
	"github.com/PycMono/go-reagent/pi/harness/observability"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// ---------- 装置 ----------

func installRunTracer(t *testing.T) *tracetest.InMemoryExporter {
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

// scriptedStream 按脚本返回事件序列；err 在 Result 时返回。
type scriptedStream struct {
	events []ai.StreamEvent
	result *ai.Message
	err    error
	index  int
	closed int
}

func (s *scriptedStream) Next() bool {
	if s.index >= len(s.events) {
		return false
	}
	s.index++
	return true
}
func (s *scriptedStream) Current() ai.StreamEvent { return s.events[s.index-1] }
func (s *scriptedStream) Result() (*ai.Message, error) {
	return s.result, s.err
}
func (s *scriptedStream) Close() error { s.closed++; return nil }

// scriptedProvider 按调用次序返回脚本化的 Stream。
type scriptedProvider struct {
	streams []*scriptedStream
	calls   int
}

func (p *scriptedProvider) Stream(context.Context, []ai.Message, []ai.ToolDefinition) ai.Stream {
	p.calls++
	if p.calls-1 < len(p.streams) {
		return p.streams[p.calls-1]
	}
	return &scriptedStream{err: errors.New("unexpected provider call")}
}

func actionMessage(text string, toolCalls ...ai.ToolCall) *ai.Message {
	message := &ai.Message{
		Role:      ai.RoleAssistant,
		Content:   []ai.ContentBlock{ai.TextBlock(text)},
		ToolCalls: toolCalls,
		Usage: &ai.Usage{
			PlatformID: "test", Model: "fake",
			InputTokens: 1, OutputTokens: 1,
			InputPriceUSDPerMillionTokens: 1, OutputPriceUSDPerMillionTokens: 1,
			CostUSD: 2.0 / 1e6,
		},
	}
	if len(toolCalls) > 0 {
		message.FinishReason = ai.FinishReasonToolUse
	} else {
		message.FinishReason = ai.FinishReasonStop
	}
	return message
}

func textDeltaStream(message *ai.Message) *scriptedStream {
	text, _ := ai.TextContent(message.Content)
	return &scriptedStream{
		events: []ai.StreamEvent{
			{Type: ai.StreamEventStart},
			{Type: ai.StreamEventTextDelta, TextDelta: text},
			{Type: ai.StreamEventDone},
		},
		result: message,
	}
}

type echoTool struct{}

func (echoTool) Definition() ai.ToolDefinition {
	return ai.ToolDefinition{
		Name: "echo", Description: "echo back", ParallelSafe: true,
		InputSchema: map[string]any{"type": "object"},
	}
}
func (echoTool) Execute(_ context.Context, arguments json.RawMessage, _ ai.UpdateEmitter) (ai.ToolOutput, error) {
	return ai.ToolOutput{Content: []ai.ContentBlock{ai.TextBlock(string(arguments))}}, nil
}

type nopRunReporter struct{}

func (nopRunReporter) Report(context.Context, AgentEvent) {}

func newTracedAgent(t *testing.T, provider ai.Provider, tools ...ai.Tool) *Agent {
	t.Helper()
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "AGENTS.md"), []byte("You are a test Agent."), 0o600); err != nil {
		t.Fatal(err)
	}
	toolRuntime, err := NewToolRuntime(ToolRuntimeOptions{Tools: tools, Middlewares: DefaultMiddlewareRegistrations()})
	if err != nil {
		t.Fatal(err)
	}
	builder := harness.NewContextBuilder(harness.NewPromptComposer(workDir), workDir)
	traced := observability.NewTracingProvider(provider, "openai", "test", "fake")
	loop := NewLoop(traced, NewScheduler(toolRuntime, 2), false, WithLoopProviderIdentity("test", "fake"))
	return New(builder, loop, toolRuntime)
}

func runInput() RunRequest {
	return RunRequest{Input: Message{ContentType: "text", Content: "你好", SenderType: "customer"}}
}

func spansByName(exporter *tracetest.InMemoryExporter) map[string][]tracetest.SpanStub {
	out := make(map[string][]tracetest.SpanStub)
	for _, span := range exporter.GetSpans() {
		out[span.Name] = append(out[span.Name], span)
	}
	return out
}

func attrOf(span tracetest.SpanStub, key string) any {
	for _, attr := range span.Attributes {
		if string(attr.Key) == key {
			return attr.Value.AsInterface()
		}
	}
	return nil
}

func sortByRequestIndex(spans []tracetest.SpanStub) {
	slices.SortFunc(spans, func(a, b tracetest.SpanStub) int {
		ai, _ := attrOf(a, observability.AttrProviderRequestIndex).(int64)
		bi, _ := attrOf(b, observability.AttrProviderRequestIndex).(int64)
		return cmp.Compare(ai, bi)
	})
}

// ---------- Span 树（§4.1、OBS-001） ----------

func TestRunBuildsFullSpanTree(t *testing.T) {
	exporter := installRunTracer(t)
	provider := &scriptedProvider{streams: []*scriptedStream{
		textDeltaStream(actionMessage("", ai.ToolCall{ID: "c1", Name: "echo", Arguments: json.RawMessage(`{"a":1}`)})),
		textDeltaStream(actionMessage("完成")),
	}}
	agent := newTracedAgent(t, provider, echoTool{})

	result, err := agent.Run(context.Background(), runInput(), nopRunReporter{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Termination.Reason != RunTerminationCompleted {
		t.Fatalf("termination = %v", result.Termination)
	}

	spans := spansByName(exporter)
	agentSpans := spans[observability.AgentSpanName(observability.AgentName)]
	if len(agentSpans) != 1 {
		t.Fatalf("invoke_agent spans = %d", len(agentSpans))
	}
	root := agentSpans[0]
	if attrOf(root, observability.AttrGenAIOperationName) != "invoke_agent" ||
		attrOf(root, observability.AttrTerminationReason) != string(RunTerminationCompleted) {
		t.Fatalf("invoke_agent 属性错误: %v", root.Attributes)
	}

	turns := spans[observability.SpanNameTurn]
	if len(turns) != 2 {
		t.Fatalf("turn spans = %d, want 2", len(turns))
	}
	for _, turn := range turns {
		if !turn.Parent.Equal(root.SpanContext) {
			t.Fatal("turn 必须是 invoke_agent 的子 Span")
		}
	}
	var turnOne tracetest.SpanStub
	for _, turn := range turns {
		if attrOf(turn, observability.AttrTurnIndex) == int64(1) {
			turnOne = turn
		}
	}
	if attrOf(turnOne, observability.AttrToolsRequestedCount) != int64(1) ||
		attrOf(turnOne, observability.AttrToolsExecutionMode) != "serial" {
		t.Fatalf("turn1 工具属性错误: %v", turnOne.Attributes)
	}

	generates := spans[observability.SpanNameGenerate]
	if len(generates) != 2 {
		t.Fatalf("generate spans = %d, want 2", len(generates))
	}
	for _, generate := range generates {
		if attrOf(generate, observability.AttrGenerationPhase) != "action" ||
			attrOf(generate, observability.AttrGenerationOutcome) != "succeeded" {
			t.Fatalf("generate 属性错误: %v", generate.Attributes)
		}
	}

	chats := spans[observability.ChatSpanName("fake")]
	if len(chats) != 2 {
		t.Fatalf("chat spans = %d, want 2", len(chats))
	}
	// In-Memory Exporter 按结束顺序返回，按 request_index 重排。
	sortByRequestIndex(chats)
	for index, chat := range chats {
		if attrOf(chat, observability.AttrProviderAttempt) != int64(1) ||
			attrOf(chat, observability.AttrProviderRequestIndex) != int64(index+1) {
			t.Fatalf("chat[%d] attempt/request_index 错误: %v", index, chat.Attributes)
		}
		// TTFT 只在有非空 Text Delta 时写入（§5）：第一次响应为纯 Tool Call，
		// 第二次（request_index=2）有正文，必须有 TTFT。
		hasTTFT := attrOf(chat, observability.AttrStreamTTFTMS) != nil
		if wantTTFT := attrOf(chat, observability.AttrProviderRequestIndex) == int64(2); hasTTFT != wantTTFT {
			t.Fatalf("chat[%d] TTFT 存在性 = %v, want %v", index, hasTTFT, wantTTFT)
		}
		parentOK := false
		for _, generate := range generates {
			if chat.Parent.Equal(generate.SpanContext) {
				parentOK = true
			}
		}
		if !parentOK {
			t.Fatal("chat 必须是 generate 的子 Span")
		}
	}

	tools := spans[observability.ToolSpanName("echo")]
	if len(tools) != 1 {
		t.Fatalf("tool spans = %d, want 1", len(tools))
	}
	toolSpan := tools[0]
	if !toolSpan.Parent.Equal(turnOne.SpanContext) {
		t.Fatal("execute_tool 必须是发起它的 turn 的子 Span")
	}
	if attrOf(toolSpan, observability.AttrGenAIToolCallID) != "c1" ||
		attrOf(toolSpan, observability.AttrToolParallelSafe) != true ||
		attrOf(toolSpan, observability.AttrToolIsError) != false {
		t.Fatalf("tool 属性错误: %v", toolSpan.Attributes)
	}
}

// TestRunRetryEvents 验证 §4.8：首次失败后在 Generate Span 上记录
// scheduled/completed，Attempt 与下一 Provider Span 对齐，Counter 只增一次。
func TestRunRetryEvents(t *testing.T) {
	exporter := installRunTracer(t)
	provider := &scriptedProvider{streams: []*scriptedStream{
		{err: pierrors.Wrap(pierrors.ErrorCodeAITransient, "test", errors.New("flush"))},
		textDeltaStream(actionMessage("恢复")),
	}}
	agent := newTracedAgent(t, provider)

	if _, err := agent.Run(context.Background(), runInput(), nopRunReporter{}); err != nil {
		t.Fatal(err)
	}

	spans := spansByName(exporter)
	generates := spans[observability.SpanNameGenerate]
	if len(generates) != 1 {
		t.Fatalf("generate spans = %d", len(generates))
	}
	generate := generates[0]
	var scheduled, completed, canceled int
	for _, event := range generate.Events {
		switch event.Name {
		case observability.EventRetryScheduled:
			scheduled++
			for _, attr := range event.Attributes {
				if string(attr.Key) == observability.AttrRetryNextAttempt && attr.Value.AsInt64() != 2 {
					t.Fatalf("scheduled next_attempt = %d, want 2", attr.Value.AsInt64())
				}
				if string(attr.Key) == observability.AttrRetryReason && attr.Value.AsString() != string(pierrors.ErrorCodeAITransient) {
					t.Fatalf("scheduled reason = %q", attr.Value.AsString())
				}
			}
		case observability.EventRetryCompleted:
			completed++
		case observability.EventRetryCanceled:
			canceled++
		}
	}
	if scheduled != 1 || completed != 1 || canceled != 0 {
		t.Fatalf("retry events scheduled/completed/canceled = %d/%d/%d", scheduled, completed, canceled)
	}
	if attrOf(generate, observability.AttrGenerationAttempts) != int64(2) ||
		attrOf(generate, observability.AttrGenerationOutcome) != "succeeded" {
		t.Fatalf("generate attempts/outcome 错误: %v", generate.Attributes)
	}

	chats := spans[observability.ChatSpanName("fake")]
	if len(chats) != 2 {
		t.Fatalf("chat spans = %d, want 2", len(chats))
	}
	var failed, succeeded tracetest.SpanStub
	for _, chat := range chats {
		if attrOf(chat, observability.AttrProviderAttempt) == int64(1) {
			failed = chat
		} else {
			succeeded = chat
		}
	}
	// 失败 Provider Span 保持 Error，父 Generate 按最终 Outcome 成功（§4.4/§4.9）。
	if failed.Status.Code != codes.Error || !failed.Parent.Equal(generate.SpanContext) {
		t.Fatalf("失败 chat Span 状态错误: %v", failed.Status)
	}
	if succeeded.Status.Code == codes.Error ||
		attrOf(succeeded, observability.AttrProviderRequestIndex) != int64(2) {
		t.Fatalf("重试 chat Span 错误: %v", succeeded.Attributes)
	}
}

// TestRunCanceledMarksCanceled 验证取消与 deadline 不归入普通 error（§4.9）。
func TestRunCanceledMarksCanceled(t *testing.T) {
	exporter := installRunTracer(t)
	provider := &scriptedProvider{streams: []*scriptedStream{
		{err: context.Canceled},
	}}
	agent := newTracedAgent(t, provider)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := agent.Run(ctx, runInput(), nopRunReporter{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v", err)
	}
	spans := spansByName(exporter)
	root := spans[observability.AgentSpanName(observability.AgentName)][0]
	if attrOf(root, observability.AttrTerminationReason) != string(RunTerminationCanceled) {
		t.Fatalf("termination = %v", attrOf(root, observability.AttrTerminationReason))
	}
	if fmt.Sprint(attrOf(root, observability.AttrErrorType)) != string(pierrors.ErrorCodeCanceled) {
		t.Fatalf("error.type = %v", attrOf(root, observability.AttrErrorType))
	}
}

// ---------- 恢复（§4.4、§4.7、§4.9） ----------

// TestGenerateOverflowRecoverySpans 验证 Overflow→Compaction→重试成功：
// 失败 Provider Span 保持 Error，Generate 最终 succeeded 且
// compaction.triggered=true，compact_context 是 Generate 的子 Span。
func TestGenerateOverflowRecoverySpans(t *testing.T) {
	exporter := installRunTracer(t)
	provider := &scriptedProvider{streams: []*scriptedStream{
		{err: contextOverflowErr()},
		textDeltaStream(compactSummaryResponse(t, "反应式摘要")),
		textDeltaStream(actionMessage("恢复")),
	}}
	loop := NewLoopWithCompaction(
		observability.NewTracingProvider(provider, "openai", "test", "fake"),
		nil, false,
		harness.CompactionConfig{ContextWindowTokens: 0, EnablePrune: false},
		WithLoopProviderIdentity("test", "fake"),
	)
	rt := newCompactionRuntime(loop.compaction, 1)
	var observed []ai.Usage

	result, err := loop.generateWithSpan(context.Background(), observability.GenerationPhaseAction,
		compactTestMessages(6), nil, nil,
		invocationObserver(func(usage ai.Usage, _ uint32, _ string) (func(error), error) {
			observed = append(observed, usage)
			return func(error) {}, nil
		}), rt)
	if err != nil {
		t.Fatal(err)
	}
	if result.message == nil || len(observed) != 1 {
		t.Fatalf("result/observed = %v/%d", result.message, len(observed))
	}

	spans := spansByName(exporter)
	generate := spans[observability.SpanNameGenerate][0]
	if generate.Status.Code == codes.Error {
		t.Fatal("恢复成功后 Generate 必须为 succeeded")
	}
	if attrOf(generate, observability.AttrGenerationOutcome) != "succeeded" ||
		attrOf(generate, observability.AttrGenerationAttempts) != int64(2) ||
		attrOf(generate, observability.AttrCompactionTriggered) != true {
		t.Fatalf("generate 属性错误: %v", generate.Attributes)
	}

	compaction := spans[observability.SpanNameCompaction]
	if len(compaction) != 1 || !compaction[0].Parent.Equal(generate.SpanContext) {
		t.Fatalf("compact_context 必须是 generate 的子 Span: %v", compaction)
	}
	if attrOf(compaction[0], observability.AttrCompactionReason) != "overflow" ||
		attrOf(compaction[0], observability.AttrCompactionSummaryTokens) == nil {
		t.Fatalf("compaction 属性错误: %v", compaction[0].Attributes)
	}

	chats := spans[observability.ChatSpanName("fake")]
	if len(chats) != 3 {
		t.Fatalf("chat spans = %d, want 3（overflow、summary、retry）", len(chats))
	}
	sortByRequestIndex(chats)
	// overflow 失败：attempt=1，保持 Error。
	if attrOf(chats[0], observability.AttrProviderAttempt) != int64(1) || chats[0].Status.Code != codes.Error {
		t.Fatalf("overflow chat 错误: %v %v", chats[0].Attributes, chats[0].Status)
	}
	// compaction 摘要：phase=compaction，attempt 独立从 1 开始。
	if attrOf(chats[1], observability.AttrGenerationPhase) != "compaction" ||
		attrOf(chats[1], observability.AttrProviderAttempt) != int64(1) ||
		!chats[1].Parent.Equal(compaction[0].SpanContext) {
		t.Fatalf("compaction chat 错误: %v", chats[1].Attributes)
	}
	// 恢复重试：attempt 连续为 2，Generate 直接子 Span。
	if attrOf(chats[2], observability.AttrProviderAttempt) != int64(2) ||
		attrOf(chats[2], observability.AttrGenerationPhase) != "action" ||
		!chats[2].Parent.Equal(generate.SpanContext) {
		t.Fatalf("retry chat 错误: %v", chats[2].Attributes)
	}
	// Request Index 全程单调 1→2→3。
	for index, chat := range chats {
		if attrOf(chat, observability.AttrProviderRequestIndex) != int64(index+1) {
			t.Fatalf("chat[%d] request_index = %v", index, attrOf(chat, observability.AttrProviderRequestIndex))
		}
	}
}

// ---------- 并发（§4.1 并行 Tool 平行子 Span） ----------

type sleepTool struct {
	name  string
	delay time.Duration
}

func (t sleepTool) Definition() ai.ToolDefinition {
	return ai.ToolDefinition{
		Name: t.name, Description: "sleep", ParallelSafe: true,
		InputSchema: map[string]any{"type": "object"},
	}
}
func (t sleepTool) Execute(ctx context.Context, _ json.RawMessage, _ ai.UpdateEmitter) (ai.ToolOutput, error) {
	select {
	case <-time.After(t.delay):
		return ai.ToolOutput{Content: []ai.ContentBlock{ai.TextBlock(t.name)}}, nil
	case <-ctx.Done():
		return ai.ToolOutput{}, ctx.Err()
	}
}

func TestParallelToolsShareTurnParentAndOverlap(t *testing.T) {
	exporter := installRunTracer(t)
	provider := &scriptedProvider{streams: []*scriptedStream{
		textDeltaStream(actionMessage("",
			ai.ToolCall{ID: "c1", Name: "slow-a", Arguments: json.RawMessage(`{}`)},
			ai.ToolCall{ID: "c2", Name: "slow-b", Arguments: json.RawMessage(`{}`)})),
		textDeltaStream(actionMessage("完成")),
	}}
	agent := newTracedAgent(t, provider, sleepTool{name: "slow-a", delay: 80 * time.Millisecond}, sleepTool{name: "slow-b", delay: 80 * time.Millisecond})

	if _, err := agent.Run(context.Background(), runInput(), nopRunReporter{}); err != nil {
		t.Fatal(err)
	}
	spans := spansByName(exporter)
	toolA := spans[observability.ToolSpanName("slow-a")]
	toolB := spans[observability.ToolSpanName("slow-b")]
	if len(toolA) != 1 || len(toolB) != 1 {
		t.Fatalf("tool spans = %d/%d", len(toolA), len(toolB))
	}
	if !toolA[0].Parent.Equal(toolB[0].Parent) {
		t.Fatal("并行 Tool 必须同父（同一 Turn）")
	}
	// 时间区间可重叠：B 开始早于 A 结束（串行执行 2×80ms 不会重叠）。
	if !toolB[0].StartTime.Before(toolA[0].EndTime) || !toolA[0].StartTime.Before(toolB[0].EndTime) {
		t.Fatalf("并行 Tool Span 时间未重叠: %v-%v vs %v-%v",
			toolA[0].StartTime, toolA[0].EndTime, toolB[0].StartTime, toolB[0].EndTime)
	}
}

// ---------- Ledger 正确性（§9.3、OBS-003） ----------

// TestContractInvalidStillRecorded 验证：已取得可信 Usage 的调用即使契约
// 校验失败也必须进入 Invocations 与 RunTotals，Outcome 标记为
// contract_invalid，且返回契约错误。
func TestContractInvalidStillRecorded(t *testing.T) {
	installRunTracer(t)
	// FinishReason=length 且携带 ToolCalls 违反 Action 契约。
	invalid := actionMessage("截断", ai.ToolCall{ID: "c1", Name: "echo", Arguments: json.RawMessage(`{}`)})
	invalid.FinishReason = ai.FinishReasonLength
	provider := &scriptedProvider{streams: []*scriptedStream{textDeltaStream(invalid)}}
	agent := newTracedAgent(t, provider, echoTool{})

	result, err := agent.Run(context.Background(), runInput(), nopRunReporter{})
	if err == nil {
		t.Fatal("契约非法必须返回错误")
	}
	if len(result.Invocations) != 1 {
		t.Fatalf("契约非法仍必须入账，invocations = %d", len(result.Invocations))
	}
	invocation := result.Invocations[0]
	if invocation.Outcome != ModelInvocationContractInvalid {
		t.Fatalf("outcome = %q, want contract_invalid", invocation.Outcome)
	}
	if invocation.ProviderRequestIndex != 1 {
		t.Fatalf("request index = %d, want 1", invocation.ProviderRequestIndex)
	}
	if result.Termination.Totals.Invocations != 1 || result.Termination.Totals.TotalTokens != 2 {
		t.Fatalf("Totals 必须包含本次调用: %+v", result.Termination.Totals)
	}
	// 契约非法的消息不进入 NewMessages。
	if len(result.NewMessages) != 0 {
		t.Fatalf("契约非法消息不得写入 NewMessages: %d", len(result.NewMessages))
	}
}

// TestLedgerRequestIndexSeparatesFromSequence 验证物理请求序号与可信
// Invocation Sequence 分离（§7）：首次失败不产生 Invocation，重试成功的
// Invocation 携带 RequestIndex=2、Sequence=1。
func TestLedgerRequestIndexSeparatesFromSequence(t *testing.T) {
	installRunTracer(t)
	provider := &scriptedProvider{streams: []*scriptedStream{
		{err: pierrors.Wrap(pierrors.ErrorCodeAITransient, "test", errors.New("flush"))},
		textDeltaStream(actionMessage("恢复")),
	}}
	agent := newTracedAgent(t, provider)

	result, err := agent.Run(context.Background(), runInput(), nopRunReporter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Invocations) != 1 {
		t.Fatalf("invocations = %d", len(result.Invocations))
	}
	if result.Invocations[0].Sequence != 1 || result.Invocations[0].ProviderRequestIndex != 2 {
		t.Fatalf("sequence/request_index = %d/%d, want 1/2",
			result.Invocations[0].Sequence, result.Invocations[0].ProviderRequestIndex)
	}
	if result.Invocations[0].Outcome != ModelInvocationAccepted {
		t.Fatalf("outcome = %q", result.Invocations[0].Outcome)
	}
}

// ---------- 性能（§19） ----------

// TestTelemetryOverheadWithinBudget 在同一进程、无网络 Collector 下对比
// Noop 与 Enabled（NeverSample SDK Provider，Span 不导出）的 Run 时延：
// 各预热后串行运行 1,000 次，P95 额外时延不超过 5%（允许 2ms 调度抖动余量）。
func TestTelemetryOverheadWithinBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	const runs = 1000

	measure := func() time.Duration {
		provider := &scriptedProvider{streams: nil}
		provider.streams = nil
		// 每次 Run 复用同一脚本流工厂。
		durations := make([]time.Duration, 0, runs)
		agent := newTracedAgent(t, provider)
		run := func() time.Duration {
			provider.streams = []*scriptedStream{textDeltaStream(actionMessage("完成"))}
			provider.calls = 0
			start := time.Now()
			if _, err := agent.Run(context.Background(), runInput(), nopRunReporter{}); err != nil {
				t.Fatal(err)
			}
			return time.Since(start)
		}
		for range 100 {
			run() // 预热
		}
		for range runs {
			durations = append(durations, run())
		}
		slices.Sort(durations)
		return durations[int(float64(runs)*0.95)]
	}

	noopP95 := measure()

	provider := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.NeverSample()))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	enabledP95 := measure()
	otel.SetTracerProvider(previous)
	_ = provider.Shutdown(t.Context())

	t.Logf("noop P95 = %v, enabled P95 = %v", noopP95, enabledP95)
	budget := time.Duration(float64(noopP95)*1.05) + 2*time.Millisecond
	if enabledP95 > budget {
		t.Fatalf("Enabled P95 %v 超出预算 %v（Noop P95 %v）", enabledP95, budget, noopP95)
	}
}
