package pi

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/PycMono/go-reagent/pi/ai"
	pierrors "github.com/PycMono/go-reagent/pi/errors"
	"github.com/PycMono/go-reagent/pi/governor"
	"github.com/PycMono/go-reagent/pi/harness"
	"github.com/PycMono/go-reagent/pi/middleware"
	"github.com/PycMono/go-reagent/pi/toolexec"
)

// stubTool 是测试用的最小 ai.Tool 实现。
type stubTool struct {
	name         string
	parallelSafe bool
	execute      func(ctx context.Context, args json.RawMessage) (ai.ToolOutput, error)
}

func (t *stubTool) Definition() ai.ToolDefinition {
	return ai.ToolDefinition{
		Name:         t.name,
		ParallelSafe: t.parallelSafe,
		InputSchema:  map[string]any{"type": "object"},
	}
}

func (t *stubTool) Execute(ctx context.Context, args json.RawMessage, _ ai.UpdateEmitter) (ai.ToolOutput, error) {
	if t.execute != nil {
		return t.execute(ctx, args)
	}
	return ai.ToolOutput{Content: []ai.ContentBlock{ai.TextBlock("ok")}}, nil
}

// subagentScriptProvider 按消息形态区分父子运行：工具列表含
// subagent_research 的是父运行；含工具结果消息的是后续轮次。
type subagentScriptProvider struct {
	mu                  sync.Mutex
	parentCalls         int
	childCalls          int
	costUSD             float64
	parentCallCount     int           // 父首次 Action 返回的子代理调用数
	childFirstTool      string        // 子首次 Action 调用的工具名（默认 web_search_exa）
	sawNotAvailableTool bool          // 子运行收到了"not available"工具结果
	secondChildBlock    chan struct{} // 非 nil 时第二个子 Stream 调用阻塞直至关闭
}

func (p *subagentScriptProvider) usage() ai.Usage {
	return ai.Usage{
		PlatformID: "test", Model: "test-model",
		InputTokens: 100, OutputTokens: 50, CostUSD: p.costUSD,
		// 价格与成本必须满足成本公式（ExpectedCostUSD）：全部成本归到输入价格。
		InputPriceUSDPerMillionTokens: p.costUSD * 1e6 / 100,
	}
}

func (p *subagentScriptProvider) Stream(_ context.Context, messages []ai.Message, tools []ai.ToolDefinition) ai.Stream {
	isParent := ai.ToolDefinitions(tools).Has(subagentToolPrefix + "research")
	p.mu.Lock()
	if isParent {
		p.parentCalls++
	} else {
		p.childCalls++
	}
	childOrdinal := p.childCalls
	p.mu.Unlock()
	if !isParent && p.secondChildBlock != nil && childOrdinal == 2 {
		<-p.secondChildBlock
	}

	hasToolResult := false
	for _, message := range messages {
		if message.Role == ai.RoleTool {
			hasToolResult = true
			text, _ := ai.TextContent(message.Content)
			if strings.Contains(text, "is not available in this run") {
				p.mu.Lock()
				p.sawNotAvailableTool = true
				p.mu.Unlock()
			}
		}
	}

	var message *ai.Message
	switch {
	case isParent && !hasToolResult:
		count := p.parentCallCount
		if count == 0 {
			count = 1
		}
		calls := make([]ai.ToolCall, 0, count)
		for index := 0; index < count; index++ {
			calls = append(calls, ai.ToolCall{
				ID:        "call-" + string(rune('a'+index)),
				Name:      subagentToolPrefix + "research",
				Arguments: json.RawMessage(`{"task":"调研竞品"}`),
			})
		}
		message = &ai.Message{
			Role:      ai.RoleAssistant,
			Content:   []ai.ContentBlock{ai.TextBlock("派出查证子代理")},
			ToolCalls: calls,
			Usage:     usagePtr(p.usage()),
		}
	case isParent:
		message = &ai.Message{
			Role:    ai.RoleAssistant,
			Content: []ai.ContentBlock{ai.TextBlock("最终回答")},
			Usage:   usagePtr(p.usage()),
		}
	case !hasToolResult:
		childTool := p.childFirstTool
		if childTool == "" {
			childTool = "web_search_exa"
		}
		message = &ai.Message{
			Role:    ai.RoleAssistant,
			Content: []ai.ContentBlock{ai.TextBlock("检索中")},
			ToolCalls: []ai.ToolCall{{
				ID:        "search-1",
				Name:      childTool,
				Arguments: json.RawMessage(`{"query":"竞品"}`),
			}},
			Usage: usagePtr(p.usage()),
		}
	default:
		message = &ai.Message{
			Role:    ai.RoleAssistant,
			Content: []ai.ContentBlock{ai.TextBlock("竞品调研报告：A 优于 B")},
			Usage:   usagePtr(p.usage()),
		}
	}
	return &registerTestStream{message: message}
}

func usagePtr(usage ai.Usage) *ai.Usage { return &usage }

// newSubagentFixture 构造手工冻结的 Registry（含 stub web 工具与已绑定的
// 子代理工具），返回子代理工具与 Provider。overrides 按名字替换默认的
// stub web 工具（如需让工具执行有副作用，例如触发取消）。
func newSubagentFixture(t *testing.T, provider ai.Provider, tool *SubagentTool, overrides ...ai.Tool) (*toolexec.Registry, *SubagentTool) {
	t.Helper()
	tools := []ai.Tool{
		&stubTool{name: "web_search_exa"},
		&stubTool{name: "web_fetch_exa"},
		tool,
	}
	for _, override := range overrides {
		for index, registered := range tools {
			if registered.Definition().Name == override.Definition().Name {
				tools[index] = override
			}
		}
	}
	registry, err := toolexec.NewRegistry(tools)
	if err != nil {
		t.Fatal(err)
	}
	registry.Freeze()

	defs := make(ai.ToolDefinitions, 0, len(tool.tools))
	for _, name := range tool.tools {
		definition, _, ok := registry.Lookup(name)
		if !ok {
			t.Fatalf("tool %q missing", name)
		}
		defs = append(defs, definition)
	}
	toolRuntime := toolexec.NewExecutorFromRegistry(registry, middleware.Defaults())
	childLoop := NewLoopWithCompaction(provider,
		toolexec.NewScheduler(toolRuntime, defaultMaxParallelTools), harness.CompactionConfig{})
	tool.bound.Store(&subagentPipeline{childLoop: childLoop, childTools: defs})
	return registry, tool
}

func runParentForTest(
	t *testing.T,
	ctx context.Context,
	provider ai.Provider,
	registry *toolexec.Registry,
	limits governor.Limits,
	listener EventListener,
) (loopResult, *governor.Governor, error) {
	t.Helper()
	toolRuntime := toolexec.NewExecutorFromRegistry(registry, middleware.Defaults())
	parentLoop := NewLoop(provider, toolexec.NewScheduler(toolRuntime, defaultMaxParallelTools))
	runContext := harness.Context{
		Messages: []ai.Message{
			{Role: ai.RoleSystem, Content: []ai.ContentBlock{ai.TextBlock("You are a test Agent.")}},
			{Role: ai.RoleUser, Content: []ai.ContentBlock{ai.TextBlock("分析一下竞品")}},
		},
		Tools:             toolRuntime.Definitions(),
		CurrentInputIndex: 1,
	}
	gov := governor.New(limits)
	if listener == nil {
		listener = nopListener{}
	}
	result, err := parentLoop.run(ctx, runContext, listener, gov)
	return result, gov, err
}

func countPhase(invocations []governor.Invocation, phase governor.InvocationPhase) int {
	count := 0
	for _, invocation := range invocations {
		if invocation.Phase == phase {
			count++
		}
	}
	return count
}

func TestSubagentEndToEnd(t *testing.T) {
	provider := &subagentScriptProvider{costUSD: 0.01}
	registry, _ := newSubagentFixture(t, provider, newResearchSubagentTool())
	listener := &registerTestRunListener{}

	result, gov, err := runParentForTest(t, context.Background(), provider, registry, governor.Limits{}, listener)
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}

	// 父账本：主运行 2 次 Action + 子运行 2 次调用（subagent 阶段）。
	if got := countPhase(result.invocations, governor.PhaseSubagent); got != 2 {
		t.Fatalf("subagent invocations = %d, want 2", got)
	}
	if len(result.invocations) != 4 {
		t.Fatalf("total invocations = %d, want 4", len(result.invocations))
	}
	// 父 Totals 合并子消耗。
	termination := gov.Termination(nil)
	if termination.Totals.Invocations != 4 || termination.Totals.InputTokens != 400 {
		t.Fatalf("totals = %+v, want 4 invocations / 400 input tokens", termination.Totals)
	}
	// 子报告经工具结果返回。
	found := false
	for _, message := range result.newMessages {
		if message.Role == ai.RoleTool && message.ToolName == subagentToolPrefix+"research" {
			text, _ := ai.TextContent(message.Content)
			if text != "竞品调研报告：A 优于 B" {
				t.Fatalf("subagent report = %q", text)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("subagent tool message not found in newMessages")
	}
	// 事件冒泡：子进度经 tool_update 到达父 listener。
	hasToolUpdate := false
	for _, event := range listener.events {
		if event == AgentEventToolUpdate {
			hasToolUpdate = true
		}
	}
	if !hasToolUpdate {
		t.Fatal("no tool_update event bubbled from child run")
	}
}

func TestSubagentSequencerUniqueAcrossRuns(t *testing.T) {
	provider := &subagentScriptProvider{costUSD: 0.01}
	registry, _ := newSubagentFixture(t, provider, newResearchSubagentTool())

	result, _, err := runParentForTest(t, context.Background(), provider, registry, governor.Limits{}, nil)
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	seen := make(map[uint32]bool)
	for _, invocation := range result.invocations {
		if invocation.ProviderRequestIndex == 0 {
			t.Fatal("ProviderRequestIndex must be non-zero")
		}
		if seen[invocation.ProviderRequestIndex] {
			t.Fatalf("duplicate ProviderRequestIndex %d", invocation.ProviderRequestIndex)
		}
		seen[invocation.ProviderRequestIndex] = true
	}
}

func TestSubagentParentBudgetTripTerminatesAsMaxCost(t *testing.T) {
	// 父预算 $0.10：父 Action $0.01 + 子首次调用 $0.30 → 触顶。
	provider := &subagentScriptProvider{costUSD: 0.01}
	registry, tool := newSubagentFixture(t, provider, newResearchSubagentTool())
	// 子调用成本单独放大：给子 Provider 包一层。
	childProvider := &subagentScriptProvider{costUSD: 0.30}
	tool.bound.Load().childLoop = NewLoopWithCompaction(childProvider,
		toolexec.NewScheduler(toolexec.NewExecutorFromRegistry(registry, middleware.Defaults()),
			defaultMaxParallelTools),
		harness.CompactionConfig{})

	result, gov, err := runParentForTest(t, context.Background(), provider, registry,
		governor.Limits{MaxCostUSD: 0.10}, nil)
	if err == nil {
		t.Fatal("run() should fail with budget error")
	}
	termination := gov.Termination(err)
	if termination.Reason != governor.TerminationMaxCost {
		t.Fatalf("termination reason = %s, want %s (不是 canceled)", termination.Reason, governor.TerminationMaxCost)
	}
	// 已计量子调用照常入账。
	if got := countPhase(result.invocations, governor.PhaseSubagent); got != 1 {
		t.Fatalf("subagent invocations = %d, want 1", got)
	}
	// 父 Totals 含子消耗。
	if termination.Totals.CostUSD < 0.30 {
		t.Fatalf("totals cost = %f, want >= 0.30", termination.Totals.CostUSD)
	}
}

func TestSubagentCancelAccounting(t *testing.T) {
	provider := &subagentScriptProvider{costUSD: 0.01}
	ctx, cancel := context.WithCancel(context.Background())
	// 子搜索工具执行时取消父 Run：在飞调用完成后取消链生效。
	registry, _ := newSubagentFixture(t, provider, newResearchSubagentTool(),
		&stubTool{name: "web_search_exa", execute: func(ctx context.Context, _ json.RawMessage) (ai.ToolOutput, error) {
			cancel()
			return ai.ToolOutput{Content: []ai.ContentBlock{ai.TextBlock("搜索结果")}}, nil
		}})

	result, _, err := runParentForTest(t, ctx, provider, registry, governor.Limits{}, nil)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("run() error = %v, want context.Canceled", err)
	}
	// 取消路径照常入账：子 Action 调用已进入父账本。
	if got := countPhase(result.invocations, governor.PhaseSubagent); got != 1 {
		t.Fatalf("subagent invocations after cancel = %d, want 1", got)
	}
}

func TestSubagentBatchCapRejectsExcess(t *testing.T) {
	provider := &subagentScriptProvider{costUSD: 0.001, parentCallCount: maxSubagentCallsPerBatch + 2}
	registry, _ := newSubagentFixture(t, provider, newResearchSubagentTool())

	result, gov, err := runParentForTest(t, context.Background(), provider, registry, governor.Limits{}, nil)
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	// 只有前 8 个子代理真正执行。
	provider.mu.Lock()
	childCalls := provider.childCalls
	provider.mu.Unlock()
	if childCalls != maxSubagentCallsPerBatch*2 {
		t.Fatalf("child provider calls = %d, want %d（8 个子运行 × 2 次调用）", childCalls, maxSubagentCallsPerBatch*2)
	}
	// 10 个工具消息齐全，第 9/10 个为合成的 IsError 结果。
	toolResults := 0
	rejectedResults := 0
	for _, message := range result.newMessages {
		if message.Role != ai.RoleTool {
			continue
		}
		toolResults++
		if message.IsError {
			rejectedResults++
			text, _ := ai.TextContent(message.Content)
			if text == "" {
				t.Fatal("rejected result must carry an explanation")
			}
		}
	}
	if toolResults != maxSubagentCallsPerBatch+2 {
		t.Fatalf("tool messages = %d, want %d", toolResults, maxSubagentCallsPerBatch+2)
	}
	if rejectedResults != 2 {
		t.Fatalf("rejected results = %d, want 2", rejectedResults)
	}
	if termination := gov.Termination(nil); termination.Reason != governor.TerminationCompleted {
		t.Fatalf("termination = %s, want completed（超限不中断运行）", termination.Reason)
	}
}

func TestSubagentChildBudgetIndependent(t *testing.T) {
	tool := newResearchSubagentTool()
	tool.limits = governor.Limits{MaxTurns: 1, MaxCostUSD: 0.3}
	provider := &subagentScriptProvider{costUSD: 0.01}
	registry, _ := newSubagentFixture(t, provider, tool)

	result, gov, err := runParentForTest(t, context.Background(), provider, registry, governor.Limits{}, nil)
	if err != nil {
		t.Fatalf("run() error = %v（子预算触顶不应中断父运行）", err)
	}
	// 子 MaxTurns=1：首轮工具调用后即触顶，工具返回 IsError 结果，父运行继续。
	foundChildLimitError := false
	for _, message := range result.newMessages {
		if message.Role == ai.RoleTool && message.ToolName == subagentToolPrefix+"research" && message.IsError {
			foundChildLimitError = true
		}
	}
	if !foundChildLimitError {
		t.Fatal("child budget exhaustion must surface as IsError tool result")
	}
	// 父 debit 不因子触顶而跳过：父 Totals 含子已消耗。
	if termination := gov.Termination(nil); termination.Totals.Invocations < 3 {
		t.Fatalf("totals invocations = %d, want >= 3（父 2 + 子 1）", termination.Totals.Invocations)
	}
}

func TestSubagentDepthGuard(t *testing.T) {
	provider := &subagentScriptProvider{costUSD: 0.01}
	_, tool := newSubagentFixture(t, provider, newResearchSubagentTool())

	_, err := tool.Execute(governor.WithSubagentDepth(context.Background(), governor.MaxSubagentDepth),
		json.RawMessage(`{"task":"x"}`), nil)
	if err == nil {
		t.Fatal("nested subagent call must be rejected")
	}
}

func TestSubagentUnboundRejected(t *testing.T) {
	tool := newResearchSubagentTool()
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"task":"x"}`), nil)
	if err == nil {
		t.Fatal("unbound subagent tool must fail")
	}
}

// captureToolListener 记录完整工具事件（用于断言合成拒绝结果的 ErrorCode 与 SSE 顺序）。
type captureToolListener struct {
	mu       sync.Mutex
	toolEnds []toolexec.Event
}

func (l *captureToolListener) OnEvent(_ context.Context, event AgentEvent) {
	if event.Type != AgentEventToolEnd || event.Tool == nil {
		return
	}
	l.mu.Lock()
	l.toolEnds = append(l.toolEnds, *event.Tool)
	l.mu.Unlock()
}

func TestSubagentRejectsNonVisibleToolCall(t *testing.T) {
	var readExecuted atomic.Int32
	tool := newResearchSubagentTool()
	registry, err := toolexec.NewRegistry([]ai.Tool{
		&stubTool{name: "read", execute: func(_ context.Context, _ json.RawMessage) (ai.ToolOutput, error) {
			readExecuted.Add(1)
			return ai.ToolOutput{Content: []ai.ContentBlock{ai.TextBlock("本地文件内容")}}, nil
		}},
		&stubTool{name: "web_search_exa"},
		&stubTool{name: "web_fetch_exa"},
		tool,
	})
	if err != nil {
		t.Fatal(err)
	}
	registry.Freeze()

	// 子模型发起白名单外的 read 调用（幻觉或注入诱导）。
	provider := &subagentScriptProvider{costUSD: 0.01, childFirstTool: "read"}
	toolRuntime := toolexec.NewExecutorFromRegistry(registry, middleware.Defaults())
	defs := ai.ToolDefinitions{}
	for _, name := range newResearchSubagentTool().tools {
		definition, _, _ := registry.Lookup(name)
		defs = append(defs, definition)
	}
	tool.bound.Store(&subagentPipeline{
		childLoop: NewLoopWithCompaction(provider,
			toolexec.NewScheduler(toolRuntime, defaultMaxParallelTools), harness.CompactionConfig{}),
		childTools: defs,
	})

	_, _, err = runParentForTest(t, context.Background(), provider, registry, governor.Limits{}, nil)
	if err != nil {
		t.Fatalf("run() error = %v（可见性拒绝不应中断运行）", err)
	}
	// read 绝不执行（Loop 可见性校验在调度前拒绝）。
	if readExecuted.Load() != 0 {
		t.Fatal("non-visible tool must NEVER execute")
	}
	// 子运行内部收到了"not available"工具结果，并继续产出最终报告。
	if !provider.sawNotAvailableTool {
		t.Fatal("child must receive an IsError result for the non-visible call")
	}
}

// mixedScriptProvider 首次 Action 返回混排批次：普通工具穿插在子代理调用中。
type mixedScriptProvider struct {
	subagentScriptProvider
}

func (p *mixedScriptProvider) Stream(ctx context.Context, messages []ai.Message, tools []ai.ToolDefinition) ai.Stream {
	isParent := ai.ToolDefinitions(tools).Has(subagentToolPrefix + "research")
	hasToolResult := false
	for _, message := range messages {
		if message.Role == ai.RoleTool {
			hasToolResult = true
			text, _ := ai.TextContent(message.Content)
			if strings.Contains(text, "is not available in this run") {
				p.mu.Lock()
				p.sawNotAvailableTool = true
				p.mu.Unlock()
			}
		}
	}
	if isParent && !hasToolResult {
		p.mu.Lock()
		p.parentCalls++
		p.mu.Unlock()
		calls := []ai.ToolCall{{ID: "call-normal", Name: "get_current_time", Arguments: json.RawMessage(`{}`)}}
		for index := 0; index < maxSubagentCallsPerBatch+1; index++ {
			calls = append(calls, ai.ToolCall{
				ID:        "call-" + string(rune('a'+index)),
				Name:      subagentToolPrefix + "research",
				Arguments: json.RawMessage(`{"task":"调研竞品"}`),
			})
		}
		return &registerTestStream{message: &ai.Message{
			Role:      ai.RoleAssistant,
			Content:   []ai.ContentBlock{ai.TextBlock("混排批次")},
			ToolCalls: calls,
			Usage:     usagePtr(p.usage()),
		}}
	}
	return p.subagentScriptProvider.Stream(ctx, messages, tools)
}

func TestSubagentMixedBatchOrderAndRejectedErrorCode(t *testing.T) {
	provider := &mixedScriptProvider{subagentScriptProvider: subagentScriptProvider{costUSD: 0.001}}
	tool := newResearchSubagentTool()
	registry, err := toolexec.NewRegistry([]ai.Tool{
		&stubTool{name: "web_search_exa"},
		&stubTool{name: "web_fetch_exa"},
		&stubTool{name: "get_current_time", parallelSafe: true},
		tool,
	})
	if err != nil {
		t.Fatal(err)
	}
	registry.Freeze()
	defs := ai.ToolDefinitions{}
	for _, name := range newResearchSubagentTool().tools {
		definition, _, _ := registry.Lookup(name)
		defs = append(defs, definition)
	}
	toolRuntime := toolexec.NewExecutorFromRegistry(registry, middleware.Defaults())
	tool.bound.Store(&subagentPipeline{
		childLoop:  NewLoopWithCompaction(provider, toolexec.NewScheduler(toolRuntime, defaultMaxParallelTools), harness.CompactionConfig{}),
		childTools: defs,
	})

	listener := &captureToolListener{}
	result, _, err := runParentForTest(t, context.Background(), provider, registry, governor.Limits{}, listener)
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}

	// 原序合并：工具消息顺序与原始调用顺序一致（normal 在首位）。
	var toolMessageIDs []string
	for _, message := range result.newMessages {
		if message.Role == ai.RoleTool {
			toolMessageIDs = append(toolMessageIDs, message.ToolCallID)
		}
	}
	wantCount := 1 + maxSubagentCallsPerBatch + 1
	if len(toolMessageIDs) != wantCount {
		t.Fatalf("tool messages = %d, want %d", len(toolMessageIDs), wantCount)
	}
	if toolMessageIDs[0] != "call-normal" {
		t.Fatalf("first tool message = %q, want call-normal（保持原始相对顺序）", toolMessageIDs[0])
	}
	// 普通工具不受子代理配额限制：正常执行且非错误。
	normalResult := result.newMessages[1] // assistant 之后第一个 tool 消息
	if normalResult.IsError {
		t.Fatal("normal tool must execute normally in a mixed batch")
	}

	// 合成拒绝结果：稳定 ErrorCode + 补发的 SSE 事件。
	var rejectedEvents int
	listener.mu.Lock()
	for _, event := range listener.toolEnds {
		if event.Result != nil && event.Result.ErrorCode == pierrors.ErrorCodeRunLimitExceeded {
			rejectedEvents++
		}
	}
	listener.mu.Unlock()
	if rejectedEvents != 1 {
		t.Fatalf("rejected tool_end events with run_limit_exceeded = %d, want 1", rejectedEvents)
	}
}

func TestBatchBudgetDebitAfterExhaustedStillAccumulates(t *testing.T) {
	gov := governor.New(governor.Limits{MaxCostUSD: 0.10})
	_, cancel := context.WithCancelCause(context.Background())
	account := governor.NewBatchBudget(gov, cancel)
	invocation := func(cost float64) governor.Invocation {
		return governor.Invocation{Usage: ai.Usage{InputTokens: 100, OutputTokens: 50, CostUSD: cost}}
	}

	firstErr := account.Debit(invocation(0.15)) // 触顶
	if firstErr == nil {
		t.Fatal("first debit must trip the parent budget")
	}
	secondErr := account.Debit(invocation(0.15)) // 触顶后的在飞调用
	if secondErr == nil {
		t.Fatal("subsequent debit must still report the first budget error")
	}
	// 在飞调用照常累加 Totals（账本与 Totals 一致）。
	termination := gov.Termination(secondErr)
	if termination.Totals.Invocations != 2 || termination.Totals.CostUSD < 0.29 {
		t.Fatalf("totals = %+v, want 2 invocations and cost ~0.30", termination.Totals)
	}
	_ = cancel
}

func TestFinalAssistantTextSkipsToolCallMessages(t *testing.T) {
	messages := []ai.Message{
		{Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("检索中")},
			ToolCalls: []ai.ToolCall{{ID: "c1", Name: "web_search_exa", Arguments: json.RawMessage(`{}`)}}},
	}
	// 子运行中途终止：中间状态不得误作最终报告。
	if got := finalAssistantText(messages); got != "" {
		t.Fatalf("finalAssistantText = %q, want empty（带 ToolCalls 的消息必须跳过）", got)
	}
	messages = append(messages, ai.Message{
		Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("最终报告")},
	})
	if got := finalAssistantText(messages); got != "最终报告" {
		t.Fatalf("finalAssistantText = %q, want 最终报告", got)
	}
}

func TestSubagentConcurrentMCPCallsBounded(t *testing.T) {
	provider := &subagentScriptProvider{costUSD: 0.001, parentCallCount: maxSubagentCallsPerBatch}
	var inFlight, peak atomic.Int32
	blocking := func(_ context.Context, _ json.RawMessage) (ai.ToolOutput, error) {
		current := inFlight.Add(1)
		for {
			seen := peak.Load()
			if current <= seen || peak.CompareAndSwap(seen, current) {
				break
			}
		}
		// 不 sleep：调度本身的交错已足够产生并发峰值
		for i := 0; i < 1000; i++ {
		}
		inFlight.Add(-1)
		return ai.ToolOutput{Content: []ai.ContentBlock{ai.TextBlock("ok")}}, nil
	}
	tool := newResearchSubagentTool()
	registry, err := toolexec.NewRegistry([]ai.Tool{
		&stubTool{name: "web_search_exa", execute: blocking},
		&stubTool{name: "web_fetch_exa", execute: blocking},
		tool,
	})
	if err != nil {
		t.Fatal(err)
	}
	registry.Freeze()
	toolRuntime := toolexec.NewExecutorFromRegistry(registry, middleware.Defaults())
	defs := ai.ToolDefinitions{}
	for _, name := range newResearchSubagentTool().tools {
		definition, _, _ := registry.Lookup(name)
		defs = append(defs, definition)
	}
	tool.bound.Store(&subagentPipeline{
		childLoop:  NewLoopWithCompaction(provider, toolexec.NewScheduler(toolRuntime, defaultMaxParallelTools), harness.CompactionConfig{}),
		childTools: defs,
	})

	if _, _, err := runParentForTest(t, context.Background(), provider, registry, governor.Limits{}, nil); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	// 跨子运行的 MCP 并发是有意行为，但并发量受 toolexec.Scheduler maxParallel 约束。
	if got := peak.Load(); got > int32(defaultMaxParallelTools) {
		t.Fatalf("concurrent MCP executions peak = %d, want <= %d", got, defaultMaxParallelTools)
	}
}

func TestSubagentLimitScopeAttribution(t *testing.T) {
	newToolCtx := func(limits governor.Limits) (context.Context, context.CancelCauseFunc) {
		ctx, cancel := context.WithCancelCause(context.Background())
		account := governor.NewBatchBudget(governor.New(limits), cancel)
		return governor.WithInvocationRecorder(governor.WithBatchBudget(ctx, account), governor.NewInvocationRecorder()), cancel
	}

	t.Run("simultaneous child and parent trip marks child", func(t *testing.T) {
		tool := newResearchSubagentTool()
		tool.limits = governor.Limits{MaxTurns: 6, MaxCostUSD: 0.10}
		provider := &subagentScriptProvider{costUSD: 0.30} // 单次调用同时越过子、父预算
		_, tool = newSubagentFixture(t, provider, tool)
		ctx, cancel := newToolCtx(governor.Limits{MaxCostUSD: 0.10})
		defer cancel(nil)

		output, _ := tool.Execute(ctx, json.RawMessage(`{"task":"调研"}`), nil)
		// observe 以子错误优先返回，runErr 是子限制 → child。
		details, _ := output.Details.(map[string]any)
		if details["limit_scope"] != "child" {
			t.Fatalf("limit_scope = %v, want child（observe 子错误优先）", details["limit_scope"])
		}
	})

	t.Run("parent-only trip marks parent", func(t *testing.T) {
		tool := newResearchSubagentTool()
		tool.limits = governor.Limits{MaxTurns: 6, MaxCostUSD: 1.0} // 子预算充足
		provider := &subagentScriptProvider{costUSD: 0.30}
		_, tool = newSubagentFixture(t, provider, tool)
		ctx, cancel := newToolCtx(governor.Limits{MaxCostUSD: 0.10})
		defer cancel(nil)

		output, _ := tool.Execute(ctx, json.RawMessage(`{"task":"调研"}`), nil)
		// 子未触顶，observe 返回父 debit 错误，runErr 即父首错 → parent。
		details, _ := output.Details.(map[string]any)
		if details["limit_scope"] != "parent" {
			t.Fatalf("limit_scope = %v, want parent", details["limit_scope"])
		}
	})

	t.Run("cascaded cancel marks parent", func(t *testing.T) {
		provider := &subagentScriptProvider{costUSD: 0.01}
		_, tool := newSubagentFixture(t, provider, newResearchSubagentTool())
		ctx, cancel := newToolCtx(governor.Limits{MaxCostUSD: 100})
		cancel(governor.ErrParentBudgetExhausted) // sibling 触发父预算，本运行被级联取消

		output, _ := tool.Execute(ctx, json.RawMessage(`{"task":"调研"}`), nil)
		details, _ := output.Details.(map[string]any)
		if details["limit_scope"] != "parent" {
			t.Fatalf("limit_scope = %v, want parent（级联取消）", details["limit_scope"])
		}
	})
}

func TestSubagentInflightSettledAfterBudgetTrip(t *testing.T) {
	// 父预算只够一次子调用：并发两个子代理，一个触顶后另一个的在飞调用
	// 仍须完成并入账（账本与 Totals 一致是硬不变量）。
	provider := &subagentScriptProvider{costUSD: 0.06, parentCallCount: 2}
	tool := newResearchSubagentTool()
	registry, err := toolexec.NewRegistry([]ai.Tool{
		&stubTool{name: "web_search_exa"},
		&stubTool{name: "web_fetch_exa"},
		tool,
	})
	if err != nil {
		t.Fatal(err)
	}
	registry.Freeze()
	toolRuntime := toolexec.NewExecutorFromRegistry(registry, middleware.Defaults())
	defs := ai.ToolDefinitions{}
	for _, name := range newResearchSubagentTool().tools {
		definition, _, _ := registry.Lookup(name)
		defs = append(defs, definition)
	}
	tool.bound.Store(&subagentPipeline{
		childLoop:  NewLoopWithCompaction(provider, toolexec.NewScheduler(toolRuntime, defaultMaxParallelTools), harness.CompactionConfig{}),
		childTools: defs,
	})

	result, gov, err := runParentForTest(t, context.Background(), provider, registry,
		governor.Limits{MaxCostUSD: 0.10}, nil)
	if err == nil {
		t.Fatal("run() should fail with parent budget error")
	}
	termination := gov.Termination(err)
	if termination.Reason != governor.TerminationMaxCost {
		t.Fatalf("reason = %s, want max_cost", termination.Reason)
	}
	// 硬不变量：账本条目数 == Totals.Invocations（触顶后在飞调用照常入账）。
	if int(termination.Totals.Invocations) != len(result.invocations) {
		t.Fatalf("totals.invocations = %d, ledger = %d：触顶后在飞调用漏账",
			termination.Totals.Invocations, len(result.invocations))
	}
}

func TestSubagentCancelVsBudgetRacePrefersParentCancel(t *testing.T) {
	// 真实竞态：两个并发子代理，首个 debit（0.06 < 0.10）的子代理继续执行
	// 带 cancel 钩子的搜索工具；第二个 debit（0.12 ≥ 0.10）触顶父预算。
	// 结算时父 ctx 已取消且预算已触顶——父取消必须胜出（switch 顺序保证）。
	gate := make(chan struct{})
	provider := &subagentScriptProvider{costUSD: 0.06, parentCallCount: 2, secondChildBlock: gate}
	// 预算 0.15：父 action（0.06）+ 首个子 debit（0.12）不触顶，
	// 第二个子 debit（0.18）触顶；阻塞门保证首个子代理的 cancel 钩子
	// 先于第二个子代理的 debit 执行，构造确定的"取消 × 触顶"竞态。
	ctx, cancel := context.WithCancel(context.Background())
	tool := newResearchSubagentTool()
	registry, err := toolexec.NewRegistry([]ai.Tool{
		&stubTool{name: "web_search_exa", execute: func(ctx context.Context, _ json.RawMessage) (ai.ToolOutput, error) {
			cancel()
			close(gate)
			return ai.ToolOutput{Content: []ai.ContentBlock{ai.TextBlock("搜索结果")}}, nil
		}},
		&stubTool{name: "web_fetch_exa"},
		tool,
	})
	if err != nil {
		t.Fatal(err)
	}
	registry.Freeze()
	toolRuntime := toolexec.NewExecutorFromRegistry(registry, middleware.Defaults())
	defs := ai.ToolDefinitions{}
	for _, name := range newResearchSubagentTool().tools {
		definition, _, _ := registry.Lookup(name)
		defs = append(defs, definition)
	}
	tool.bound.Store(&subagentPipeline{
		childLoop:  NewLoopWithCompaction(provider, toolexec.NewScheduler(toolRuntime, defaultMaxParallelTools), harness.CompactionConfig{}),
		childTools: defs,
	})

	_, gov, err := runParentForTest(t, ctx, provider, registry,
		governor.Limits{MaxCostUSD: 0.15}, nil)
	if err == nil {
		t.Fatal("run() should fail")
	}
	termination := gov.Termination(err)
	if termination.Reason != governor.TerminationCanceled {
		t.Fatalf("reason = %s, want canceled（父 ctx 取消优先于预算触顶）", termination.Reason)
	}
	// 全部已计量调用入账：父 action + 两个子 action（触顶与取消都不漏账）。
	if termination.Totals.Invocations != 3 {
		t.Fatalf("totals.invocations = %d, want 3", termination.Totals.Invocations)
	}
}
