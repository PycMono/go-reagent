package pi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/PycMono/go-reagent/pi/ai"
	pierrors "github.com/PycMono/go-reagent/pi/errors"
	"github.com/PycMono/go-reagent/pi/governor"
	"github.com/PycMono/go-reagent/pi/harness"
	"github.com/PycMono/go-reagent/pi/harness/observability"
	"github.com/PycMono/go-reagent/pi/loopdetect"
	"github.com/PycMono/go-reagent/pi/middleware"
	"github.com/PycMono/go-reagent/pi/toolexec"
)

// loopScriptProvider 永远请求同一个工具调用（模拟无进展循环）；finalAfter
// 大于 0 时，在收到指定数量的工具结果后改为返回最终文本回答。
type loopScriptProvider struct {
	mu         sync.Mutex
	requests   [][]ai.Message
	callName   string
	callArgs   string
	finalAfter int
}

func (p *loopScriptProvider) usage() ai.Usage {
	return ai.Usage{
		PlatformID: "test", Model: "test-model",
		InputTokens: 100, OutputTokens: 50, CostUSD: 0.01,
		InputPriceUSDPerMillionTokens: 0.01 * 1e6 / 100,
	}
}

func (p *loopScriptProvider) Stream(_ context.Context, messages []ai.Message, _ []ai.ToolDefinition) ai.Stream {
	p.mu.Lock()
	p.requests = append(p.requests, append([]ai.Message(nil), messages...))
	requests := len(p.requests)
	p.mu.Unlock()

	toolResults := 0
	for _, message := range messages {
		if message.Role == ai.RoleTool {
			toolResults++
		}
	}

	message := &ai.Message{
		Role:    ai.RoleAssistant,
		Content: []ai.ContentBlock{ai.TextBlock("思考中")},
		Usage:   usagePtr(p.usage()),
	}
	if p.finalAfter > 0 && toolResults >= p.finalAfter {
		message.Content = []ai.ContentBlock{ai.TextBlock("最终回答")}
		return &registerTestStream{message: message}
	}
	message.ToolCalls = []ai.ToolCall{{
		ID:        fmt.Sprintf("call-%d", requests),
		Name:      p.callName,
		Arguments: json.RawMessage(p.callArgs),
	}}
	return &registerTestStream{message: message}
}

// requestText 拼接一次物理请求中全部消息的文本，用于 ephemeral 断言。
func requestText(messages []ai.Message) string {
	var builder strings.Builder
	for _, message := range messages {
		text, _ := ai.TextContent(message.Content)
		builder.WriteString(text)
	}
	return builder.String()
}

type loopCaptureListener struct {
	mu     sync.Mutex
	events []AgentEvent
}

func (l *loopCaptureListener) OnEvent(_ context.Context, event AgentEvent) {
	l.mu.Lock()
	l.events = append(l.events, event)
	l.mu.Unlock()
}

func (l *loopCaptureListener) count(eventType AgentEventType) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	count := 0
	for _, event := range l.events {
		if event.Type == eventType {
			count++
		}
	}
	return count
}

// newLoopDetectFixture 构造单工具（search）的 Registry 与 Loop。
func newLoopDetectFixture(
	t *testing.T,
	provider ai.Provider,
	execute func(ctx context.Context, args json.RawMessage) (ai.ToolOutput, error),
	loopConfig loopdetect.Config,
) (*Loop, *atomic.Int32) {
	t.Helper()
	var executions atomic.Int32
	registry, err := toolexec.NewRegistry([]ai.Tool{
		&stubTool{name: "search", execute: func(ctx context.Context, args json.RawMessage) (ai.ToolOutput, error) {
			executions.Add(1)
			if execute != nil {
				return execute(ctx, args)
			}
			return ai.ToolOutput{Content: []ai.ContentBlock{ai.TextBlock("相同的结果")}}, nil
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	registry.Freeze()
	toolRuntime := toolexec.NewExecutorFromRegistry(registry, middleware.Defaults())
	loop := NewLoop(provider, toolexec.NewScheduler(toolRuntime, defaultMaxParallelTools),
		WithLoopDetection(loopConfig))
	return loop, &executions
}

// Recover→Terminate 主路径：5 次相同结果后 recover（合成结果、不执行工具），
// 再次请求即 terminate；提醒只出现在第 4 次 Provider 请求，不进入消息历史。
func TestLoopDetectionRecoverThenTerminate(t *testing.T) {
	provider := &loopScriptProvider{callName: "search", callArgs: `{"q":"x"}`}
	loop, executions := newLoopDetectFixture(t, provider, nil, loopdetect.Config{})
	runContextTools := ai.ToolDefinitions{{Name: "search", InputSchema: map[string]any{"type": "object"}}}
	listener := &loopCaptureListener{}

	runContext := harness.Context{
		Messages: []ai.Message{
			{Role: ai.RoleSystem, Content: []ai.ContentBlock{ai.TextBlock("You are a test Agent.")}},
			{Role: ai.RoleUser, Content: []ai.ContentBlock{ai.TextBlock("开始任务")}},
		},
		Tools:             runContextTools,
		CurrentInputIndex: 1,
	}
	gov := governor.New(governor.Limits{})
	result, err := loop.run(context.Background(), runContext, listener, gov)
	if err == nil {
		t.Fatal("run must end with loop detection error")
	}
	var loopErr *loopdetect.Error
	if !errors.As(err, &loopErr) {
		t.Fatalf("error must unwrap to *loopdetect.Error, got %v", err)
	}
	if pierrors.ErrorCodeOf(err) != pierrors.ErrorCodeRunLoopDetected {
		t.Fatalf("error code = %s, want run_loop_detected", pierrors.ErrorCodeOf(err))
	}
	if termination := gov.Termination(err); termination.Reason != governor.TerminationLoopDetected {
		t.Fatalf("termination reason = %s, want loop_detected", termination.Reason)
	}

	// 工具只执行了前 5 次（5 个相同结果确认）；第 6 次被 recover 阻止，
	// 第 7 次请求触发 terminate——Scheduler 在 critical 批次零调用。
	if got := executions.Load(); got != 5 {
		t.Fatalf("tool executions = %d, want 5（critical 批次不得产生副作用）", got)
	}

	// 提醒只出现在第 4 次物理请求（第 3 次调用 warn 后的下一次 Action）。
	if len(provider.requests) != 7 {
		t.Fatalf("provider requests = %d, want 7", len(provider.requests))
	}
	for index, request := range provider.requests {
		hasReminder := strings.Contains(requestText(request), "循环护栏提醒")
		if index == 3 && !hasReminder {
			t.Fatal("4th request must carry the ephemeral reminder")
		}
		if index != 3 && hasReminder {
			t.Fatalf("request %d must not carry the reminder", index)
		}
	}

	// 提醒不进入消息历史；协议组完整：recover 的 Assistant 与合成结果都在，
	// terminate 的 Assistant 不在。
	for _, message := range result.newMessages {
		text, _ := ai.TextContent(message.Content)
		if strings.Contains(text, "循环护栏提醒") {
			t.Fatal("reminder must never enter newMessages")
		}
	}
	syntheticResults := 0
	for _, message := range result.newMessages {
		if message.Role == ai.RoleTool && message.IsError {
			text, _ := ai.TextContent(message.Content)
			if strings.Contains(text, "工具循环护栏阻止了本批次执行") {
				syntheticResults++
			}
		}
	}
	if syntheticResults != 1 {
		t.Fatalf("recover synthetic results = %d, want 1", syntheticResults)
	}
	// 最后一个 assistant 消息必须带齐工具结果（terminate 不留孤立 Assistant）。
	for index, message := range result.newMessages {
		if message.Role != ai.RoleAssistant || len(message.ToolCalls) == 0 {
			continue
		}
		for _, call := range message.ToolCalls {
			found := false
			for _, follow := range result.newMessages[index+1:] {
				if follow.Role == ai.RoleTool && follow.ToolCallID == call.ID {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("assistant tool call %s has no matching tool result", call.ID)
			}
		}
	}
	// recover 的合成结果发送完整 start/end 事件。
	if got := listener.count(AgentEventToolEnd); got != 6 {
		t.Fatalf("tool_end events = %d, want 6（5 次真实 + 1 次合成）", got)
	}
}

// Disabled 时不干预：相同循环由 MaxTurns 兜底。
func TestLoopDetectionDisabledFallsBackToBudget(t *testing.T) {
	provider := &loopScriptProvider{callName: "search", callArgs: `{"q":"x"}`}
	loop, executions := newLoopDetectFixture(t, provider, nil, loopdetect.Config{Disabled: true})
	_, gov, err := runLoopDetectWithTools(t, loop, governor.Limits{MaxTurns: 4})
	if err == nil {
		t.Fatal("run must end with max_turns")
	}
	if termination := gov.Termination(err); termination.Reason != governor.TerminationMaxTurns {
		t.Fatalf("reason = %s, want max_turns", termination.Reason)
	}
	if got := executions.Load(); got != 4 {
		t.Fatalf("executions = %d, want 4", got)
	}
}

// excluded_tools 不计数：被排除工具的循环不触发护栏。
func TestLoopDetectionExcludedTool(t *testing.T) {
	provider := &loopScriptProvider{callName: "search", callArgs: `{"q":"x"}`}
	loop, executions := newLoopDetectFixture(t, provider, nil,
		loopdetect.Config{ExcludedTools: []string{"search"}})
	_, gov, err := runLoopDetectWithTools(t, loop, governor.Limits{MaxTurns: 4})
	if err == nil {
		t.Fatal("run must end with max_turns")
	}
	if termination := gov.Termination(err); termination.Reason != governor.TerminationMaxTurns {
		t.Fatalf("reason = %s, want max_turns", termination.Reason)
	}
	if got := executions.Load(); got != 4 {
		t.Fatalf("executions = %d, want 4", got)
	}
}

// 结果持续变化代表进展：只提醒、不 recover/terminate，Run 正常完成。
func TestLoopDetectionChangingOutcomesProgress(t *testing.T) {
	provider := &loopScriptProvider{callName: "search", callArgs: `{"q":"x"}`, finalAfter: 6}
	var counter int
	loop, _ := newLoopDetectFixture(t, provider,
		func(_ context.Context, _ json.RawMessage) (ai.ToolOutput, error) {
			counter++
			return ai.ToolOutput{Content: []ai.ContentBlock{
				ai.TextBlock(fmt.Sprintf("新状态 %d", counter))}}, nil
		}, loopdetect.Config{})

	result, gov, err := runLoopDetectWithTools(t, loop, governor.Limits{})
	if err != nil {
		t.Fatalf("run must complete, got %v", err)
	}
	if termination := gov.Termination(err); termination.Reason != governor.TerminationCompleted {
		t.Fatalf("reason = %s, want completed", termination.Reason)
	}
	// 结果变化后允许重新提醒，但任何请求都不含 recover/terminate 的干预。
	for _, message := range result.newMessages {
		if message.Role == ai.RoleTool && message.IsError {
			t.Fatal("no synthetic error results expected when progress continues")
		}
	}
}

func runLoopDetectWithTools(t *testing.T, loop *Loop, limits governor.Limits) (loopResult, *governor.Governor, error) {
	t.Helper()
	runContext := harness.Context{
		Messages: []ai.Message{
			{Role: ai.RoleSystem, Content: []ai.ContentBlock{ai.TextBlock("You are a test Agent.")}},
			{Role: ai.RoleUser, Content: []ai.ContentBlock{ai.TextBlock("开始任务")}},
		},
		Tools:             ai.ToolDefinitions{{Name: "search", InputSchema: map[string]any{"type": "object"}}},
		CurrentInputIndex: 1,
	}
	gov := governor.New(limits)
	result, err := loop.run(context.Background(), runContext, nopListener{}, gov)
	return result, gov, err
}

// 结果对齐判定：长度、ToolCallID、工具名任一不一致即失败。
func TestToolResultsAligned(t *testing.T) {
	calls := ai.ToolCalls{
		{ID: "1", Name: "a", Arguments: json.RawMessage(`{}`)},
		{ID: "2", Name: "b", Arguments: json.RawMessage(`{}`)},
	}
	aligned := []toolexec.Result{{ToolCallID: "1", ToolName: "a"}, {ToolCallID: "2", ToolName: "b"}}
	if !toolResultsAligned(calls, aligned) {
		t.Fatal("aligned results must pass")
	}
	if toolResultsAligned(calls, aligned[:1]) {
		t.Fatal("length mismatch must fail")
	}
	wrong := []toolexec.Result{{ToolCallID: "1", ToolName: "a"}, {ToolCallID: "2", ToolName: "c"}}
	if toolResultsAligned(calls, wrong) {
		t.Fatal("tool name mismatch must fail")
	}
	wrong = []toolexec.Result{{ToolCallID: "1", ToolName: "a"}, {ToolCallID: "3", ToolName: "b"}}
	if toolResultsAligned(calls, wrong) {
		t.Fatal("tool call ID mismatch must fail")
	}
}

// ---------- Warning ephemeral context ----------

func ephemeralReminder() []ai.Message {
	return []ai.Message{{Role: ai.RoleSystem, Content: []ai.ContentBlock{
		ai.TextBlock("循环护栏提醒：检测到重复的工具调用")}}}
}

func newEphemeralTestLoop(provider ai.Provider, cfg harness.CompactionConfig) (*Loop, *compactionRuntime) {
	loop := NewLoopWithCompaction(provider, nil, cfg)
	return loop, newCompactionRuntime(loop.compaction, 1, governor.NewSequencer())
}

// 1. 普通成功路径：Provider 能看到 warning，generationResult.context 不包含。
func TestEphemeralReminderNormalPath(t *testing.T) {
	provider := &fakeCompactionProvider{steps: []compactionStep{
		{message: compactSummaryResponse(t, "done")},
	}}
	loop, rt := newEphemeralTestLoop(provider, harness.CompactionConfig{})
	durable := compactTestMessages(2)

	result, err := loop.generate(context.Background(),
		&generateState{phase: observability.GenerationPhaseAction, rt: rt},
		durable, ephemeralReminder(), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !requestsContain(provider.requests[0], "循环护栏提醒") {
		t.Fatal("provider must see the reminder")
	}
	if requestsContain(result.context, "循环护栏提醒") {
		t.Fatal("result.context must not contain the reminder")
	}
	// durable 切片不被合并修改。
	if requestsContain(durable, "循环护栏提醒") {
		t.Fatal("durable history must not be mutated")
	}
}

// 2. transient retry 的每次物理请求都看到同一 warning；返回 context 不含。
func TestEphemeralReminderRetryPath(t *testing.T) {
	provider := &fakeCompactionProvider{steps: []compactionStep{
		{err: pierrors.Wrap(pierrors.ErrorCodeAITransient, "test", errors.New("boom"))},
		{message: compactSummaryResponse(t, "done")},
	}}
	loop, rt := newEphemeralTestLoop(provider, harness.CompactionConfig{})

	result, err := loop.generate(context.Background(),
		&generateState{phase: observability.GenerationPhaseAction, rt: rt},
		compactTestMessages(2), ephemeralReminder(), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if provider.calls != 2 {
		t.Fatalf("calls = %d, want 2 (transient retry)", provider.calls)
	}
	for index, request := range provider.requests {
		if !requestsContain(request, "循环护栏提醒") {
			t.Fatalf("request %d must see the reminder", index)
		}
	}
	if requestsContain(result.context, "循环护栏提醒") {
		t.Fatal("result.context must not contain the reminder")
	}
}

// 3. reactive L2：被摘要消息与摘要请求本身都不含 warning；压缩后的 Provider
// retry 仍看到 warning。
func TestEphemeralReminderReactiveCompaction(t *testing.T) {
	provider := &fakeCompactionProvider{steps: []compactionStep{
		{err: contextOverflowErr()},
		{message: compactSummaryResponse(t, "反应式摘要")},
		{message: compactSummaryResponse(t, "done")},
	}}
	loop, rt := newEphemeralTestLoop(provider,
		harness.CompactionConfig{ContextWindowTokens: 0, EnablePrune: false})

	result, err := loop.generate(context.Background(),
		&generateState{phase: observability.GenerationPhaseAction, rt: rt},
		compactTestMessages(6), ephemeralReminder(), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if provider.calls != 3 {
		t.Fatalf("calls = %d, want 3 (overflow, summary, retry)", provider.calls)
	}
	if !requestsContain(provider.requests[0], "循环护栏提醒") {
		t.Fatal("original request must see the reminder")
	}
	if requestsContain(provider.requests[1], "循环护栏提醒") {
		t.Fatal("summary request itself must never carry the reminder")
	}
	if !requestsContain(provider.requests[2], "循环护栏提醒") {
		t.Fatal("post-compaction retry must still see the reminder")
	}
	if requestsContain(result.context, "循环护栏提醒") {
		t.Fatal("compacted durable context must not contain the reminder")
	}
}

// 与 Governor 的优先级：Action 已耗尽 Cost 预算且包含循环 Tool Calls 时，
// 预算终止获胜，Detector 不参与（无提醒、无合成结果）。
func TestLoopDetectionBudgetTerminatesFirst(t *testing.T) {
	provider := &loopScriptProvider{callName: "search", callArgs: `{"q":"x"}`}
	loop, executions := newLoopDetectFixture(t, provider, nil, loopdetect.Config{})
	_, gov, err := runLoopDetectWithTools(t, loop, governor.Limits{MaxCostUSD: 0.005})
	if err == nil {
		t.Fatal("run must end with budget termination")
	}
	if termination := gov.Termination(err); termination.Reason != governor.TerminationMaxCost {
		t.Fatalf("reason = %s, want max_cost", termination.Reason)
	}
	if got := executions.Load(); got != 0 {
		t.Fatalf("executions = %d, want 0（预算在工具执行前终止）", got)
	}
	if len(provider.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(provider.requests))
	}
}
