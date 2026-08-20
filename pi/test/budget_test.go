package test

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/harness"
	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
)

func budgetUsage(input, output int64) *ai.Usage {
	return &ai.Usage{
		PlatformID:                    "test",
		Model:                         "model",
		InputTokens:                   input,
		OutputTokens:                  output,
		InputPriceUSDPerMillionTokens: 1,
		OutputPriceUSDPerMillionTokens: 1,
		CostUSD:                       float64(input+output) / 1_000_000,
	}
}

func budgetAssistant(text string, usage *ai.Usage) *ai.Message {
	return &ai.Message{Role: ai.RoleAssistant, Content: blocks(text), Usage: usage}
}

func budgetToolCall(id, name, text string, usage *ai.Usage) *ai.Message {
	return &ai.Message{
		Role:    ai.RoleAssistant,
		Content: blocks(text),
		ToolCalls: []ai.ToolCall{{
			ID:        id,
			Name:      name,
			Arguments: json.RawMessage(`{"path":"a.txt"}`),
		}},
		Usage: usage,
	}
}

func budgetEventTypes(reporter *recordingReporter) []pi.AgentEventType {
	events := reporter.Events()
	types := make([]pi.AgentEventType, len(events))
	for index, event := range events {
		types[index] = event.Type
	}
	return types
}

func TestRunRejectsInvalidLimitsBeforeContextConstruction(t *testing.T) {
	// 工作区路径指向一个文件而不是目录：只要进入 Context 构造就会失败。
	notAWorkspace := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(notAWorkspace, []byte("not a dir"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		limits pi.RunLimits
	}{
		{name: "negative turns", limits: pi.RunLimits{MaxTurns: -1}},
		{name: "negative tokens", limits: pi.RunLimits{MaxTotalTokens: -1}},
		{name: "negative cost", limits: pi.RunLimits{MaxCostUSD: -0.5}},
		{name: "NaN cost", limits: pi.RunLimits{MaxCostUSD: math.NaN()}},
		{name: "infinite cost", limits: pi.RunLimits{MaxCostUSD: math.Inf(1)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &fakeProvider{}
			toolRuntime := &fakeToolRuntime{}
			builder := harness.NewContextBuilder(harness.NewPromptComposer(notAWorkspace), notAWorkspace)
			agent := pi.New(builder, pi.NewLoop(provider, pi.NewScheduler(toolRuntime, 1), false), toolRuntime)

			request := validAgentRequest("hello")
			request.Limits = tt.limits
			result, err := agent.Run(context.Background(), request, nil)
			if !errors.Is(err, pierrors.ErrRequestInvalid) {
				t.Fatalf("Run() error = %v, want request invalid", err)
			}
			if pierrors.ErrorCodeOf(err) != pierrors.ErrorCodeRequestInvalid {
				t.Fatalf("error code = %q, want %q", pierrors.ErrorCodeOf(err), pierrors.ErrorCodeRequestInvalid)
			}
			if len(provider.requests) != 0 {
				t.Fatalf("provider calls = %d, want 0", len(provider.requests))
			}
			if result.Termination.Reason != pi.RunTerminationError || result.Termination.Limit != "" ||
				result.Termination.Totals != (pi.RunTotals{}) {
				t.Fatalf("Termination = %#v, want error reason with zero totals", result.Termination)
			}
		})
	}
}

func TestRunLeavesCallerLimitsUnmodified(t *testing.T) {
	provider := &fakeProvider{responses: []*ai.Message{budgetAssistant("done", budgetUsage(1, 1))}}
	runtime := newPublicAgent(t, provider, &fakeToolRuntime{})
	request := validAgentRequest("hello")
	request.Limits = pi.RunLimits{MaxTurns: 3, MaxCostUSD: 0.5, MaxTotalTokens: 100}

	if _, err := runtime.Run(context.Background(), request, nil); err != nil {
		t.Fatal(err)
	}
	if request.Limits != (pi.RunLimits{MaxTurns: 3, MaxCostUSD: 0.5, MaxTotalTokens: 100}) {
		t.Fatalf("caller limits mutated: %#v", request.Limits)
	}
}

func TestRunMaxTurnsDirectAnswerCompletes(t *testing.T) {
	provider := &fakeProvider{responses: []*ai.Message{budgetAssistant("done", budgetUsage(2, 3))}}
	runtime := newPublicAgent(t, provider, &fakeToolRuntime{})
	request := validAgentRequest("hello")
	request.Limits.MaxTurns = 1

	result, err := runtime.Run(context.Background(), request, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Termination.Reason != pi.RunTerminationCompleted || result.Termination.Totals.Turns != 1 {
		t.Fatalf("Termination = %#v, want completed after one turn", result.Termination)
	}
}

func TestRunMaxTurnsAllowsFinalToolBatch(t *testing.T) {
	call := ai.ToolCall{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"a.txt"}`)}
	provider := &fakeProvider{responses: []*ai.Message{
		budgetToolCall("call-1", "read", "reading", budgetUsage(1, 1)),
		budgetAssistant("unreachable", budgetUsage(1, 1)),
	}}
	toolRuntime := &fakeToolRuntime{
		definitions: []ai.ToolDefinition{{Name: "read"}},
		results:     map[string]pi.ToolResult{"read": toolResult(call, "file A", false)},
	}
	runtime := newPublicAgent(t, provider, toolRuntime)
	request := validAgentRequest("read a")
	request.Limits.MaxTurns = 1

	result, err := runtime.Run(context.Background(), request, nil)
	if !errors.Is(err, pierrors.ErrRunLimitExceeded) {
		t.Fatalf("Run() error = %v, want run limit exceeded", err)
	}
	if pierrors.ErrorCodeOf(err) != pierrors.ErrorCodeRunLimitExceeded {
		t.Fatalf("error code = %q, want %q", pierrors.ErrorCodeOf(err), pierrors.ErrorCodeRunLimitExceeded)
	}
	if result.Termination.Reason != pi.RunTerminationMaxTurns || result.Termination.Limit != pi.RunLimitTurns {
		t.Fatalf("Termination = %#v, want max_turns", result.Termination)
	}
	if calls := toolRuntime.Calls(); len(calls) != 1 {
		t.Fatalf("tool calls = %d, want the final batch executed", len(calls))
	}
	if len(provider.requests) != 1 {
		t.Fatalf("provider calls = %d, want no second turn", len(provider.requests))
	}
	if len(result.NewMessages) != 2 || result.NewMessages[0].Role != ai.RoleAssistant ||
		result.NewMessages[1].Role != ai.RoleTool {
		t.Fatalf("NewMessages = %#v, want complete action and tool result", result.NewMessages)
	}
	if result.Termination.Totals.Turns != 1 || result.Termination.Totals.Invocations != 1 {
		t.Fatalf("Totals = %#v", result.Termination.Totals)
	}
}

func TestRunMaxTurnsCountsOuterTurnsRegardlessOfThinking(t *testing.T) {
	call := ai.ToolCall{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"a.txt"}`)}
	for _, thinking := range []bool{false, true} {
		responses := []*ai.Message{budgetToolCall("call-1", "read", "reading", budgetUsage(1, 1))}
		if thinking {
			responses = []*ai.Message{
				budgetAssistant("plan", budgetUsage(1, 1)),
				budgetToolCall("call-1", "read", "reading", budgetUsage(1, 1)),
			}
		}
		provider := &fakeProvider{responses: responses}
		toolRuntime := &fakeToolRuntime{
			definitions: []ai.ToolDefinition{{Name: "read"}},
			results:     map[string]pi.ToolResult{"read": toolResult(call, "file A", false)},
		}
		runtime := newPublicAgentWithThinking(t, provider, toolRuntime, thinking)
		request := validAgentRequest("read a")
		request.Limits.MaxTurns = 1

		result, err := runtime.Run(context.Background(), request, nil)
		if !errors.Is(err, pierrors.ErrRunLimitExceeded) || result.Termination.Totals.Turns != 1 {
			t.Fatalf("thinking=%v error/Turns = %v/%d, want run limit after one outer turn",
				thinking, err, result.Termination.Totals.Turns)
		}
	}
}

func TestRunThinkingBudgetStopsBeforeAction(t *testing.T) {
	tests := []struct {
		name       string
		limits     pi.RunLimits
		wantReason pi.RunTerminationReason
	}{
		{name: "cost", limits: pi.RunLimits{MaxCostUSD: 0.001}, wantReason: pi.RunTerminationMaxCost},
		{name: "tokens", limits: pi.RunLimits{MaxTotalTokens: 1000}, wantReason: pi.RunTerminationMaxTotalTokens},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &fakeProvider{responses: []*ai.Message{
				budgetAssistant("plan", budgetUsage(1000, 1000)),
				budgetAssistant("unreachable", budgetUsage(1, 1)),
			}}
			runtime := newPublicAgentWithThinking(t, provider, &fakeToolRuntime{}, true)
			request := validAgentRequest("hello")
			request.Limits = tt.limits

			result, err := runtime.Run(context.Background(), request, nil)
			if !errors.Is(err, pierrors.ErrRunLimitExceeded) {
				t.Fatalf("Run() error = %v, want run limit exceeded", err)
			}
			if result.Termination.Reason != tt.wantReason {
				t.Fatalf("reason = %q, want %q", result.Termination.Reason, tt.wantReason)
			}
			if len(provider.requests) != 1 {
				t.Fatalf("provider calls = %d, want Action never started", len(provider.requests))
			}
			if len(result.NewMessages) != 0 {
				t.Fatalf("NewMessages = %#v, want none", result.NewMessages)
			}
			if len(result.Invocations) != 1 || result.Invocations[0].Phase != pi.ModelInvocationPhaseThinking {
				t.Fatalf("Invocations = %#v, want the thinking invocation kept", result.Invocations)
			}
			if result.Termination.Totals.CostUSD != 0.002 || result.Termination.Totals.TotalTokens != 2000 {
				t.Fatalf("Totals = %#v, want actual overshoot values", result.Termination.Totals)
			}
		})
	}
}

func TestRunFinalActionBudgetKeepsAssistantMessage(t *testing.T) {
	reporter := &recordingReporter{}
	provider := &fakeProvider{responses: []*ai.Message{
		budgetAssistant("final answer", budgetUsage(1000, 1000)),
	}}
	runtime := newPublicAgent(t, provider, &fakeToolRuntime{})
	request := validAgentRequest("hello")
	request.Limits.MaxCostUSD = 0.001

	result, err := runtime.Run(context.Background(), request, reporter)
	if !errors.Is(err, pierrors.ErrRunLimitExceeded) || result.Termination.Reason != pi.RunTerminationMaxCost {
		t.Fatalf("error/reason = %v/%q", err, result.Termination.Reason)
	}
	if len(result.NewMessages) != 1 || result.NewMessages[0].Role != ai.RoleAssistant {
		t.Fatalf("NewMessages = %#v, want the complete final assistant", result.NewMessages)
	}
	if len(result.Invocations) != 1 {
		t.Fatalf("Invocations = %#v", result.Invocations)
	}
	wantEvents := []pi.AgentEventType{pi.AgentEventMessageStart, pi.AgentEventMessageUpdate, pi.AgentEventMessageEnd}
	if got := budgetEventTypes(reporter); !reflectEventTypes(got, wantEvents) {
		t.Fatalf("events = %v, want %v", got, wantEvents)
	}
}

func TestRunToolActionBudgetSkipsToolsMessageEndAndNewMessages(t *testing.T) {
	call := ai.ToolCall{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"a.txt"}`)}
	reporter := &recordingReporter{}
	provider := &fakeProvider{responses: []*ai.Message{
		budgetToolCall("call-1", "read", "reading", budgetUsage(1000, 1000)),
	}}
	toolRuntime := &fakeToolRuntime{
		definitions: []ai.ToolDefinition{{Name: "read"}},
		results:     map[string]pi.ToolResult{"read": toolResult(call, "file A", false)},
	}
	runtime := newPublicAgent(t, provider, toolRuntime)
	request := validAgentRequest("read a")
	request.Limits.MaxCostUSD = 0.001

	result, err := runtime.Run(context.Background(), request, reporter)
	if !errors.Is(err, pierrors.ErrRunLimitExceeded) || result.Termination.Reason != pi.RunTerminationMaxCost {
		t.Fatalf("error/reason = %v/%q", err, result.Termination.Reason)
	}
	if calls := toolRuntime.Calls(); len(calls) != 0 {
		t.Fatalf("tool calls = %d, want none after budget", len(calls))
	}
	if len(result.NewMessages) != 0 {
		t.Fatalf("NewMessages = %#v, want no incomplete action", result.NewMessages)
	}
	if len(result.Invocations) != 1 || result.Invocations[0].Phase != pi.ModelInvocationPhaseAction {
		t.Fatalf("Invocations = %#v, want the over-budget action kept", result.Invocations)
	}
	for _, eventType := range budgetEventTypes(reporter) {
		if eventType == pi.AgentEventMessageEnd || eventType == pi.AgentEventToolStart || eventType == pi.AgentEventToolEnd {
			t.Fatalf("events = %v, want no message_end or tool events", budgetEventTypes(reporter))
		}
	}
	wantEvents := []pi.AgentEventType{pi.AgentEventMessageStart, pi.AgentEventMessageUpdate}
	if got := budgetEventTypes(reporter); !reflectEventTypes(got, wantEvents) {
		t.Fatalf("events = %v, want %v", got, wantEvents)
	}
}

func TestRunCostAndTokenSimultaneousPrefersCost(t *testing.T) {
	provider := &fakeProvider{responses: []*ai.Message{
		budgetAssistant("done", budgetUsage(1000, 1000)),
	}}
	runtime := newPublicAgent(t, provider, &fakeToolRuntime{})
	request := validAgentRequest("hello")
	request.Limits = pi.RunLimits{MaxCostUSD: 0.001, MaxTotalTokens: 1000}

	result, err := runtime.Run(context.Background(), request, nil)
	if !errors.Is(err, pierrors.ErrRunLimitExceeded) {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Termination.Reason != pi.RunTerminationMaxCost || result.Termination.Limit != pi.RunLimitCostUSD {
		t.Fatalf("Termination = %#v, want max_cost priority", result.Termination)
	}
	if result.Termination.Totals.TotalTokens != 2000 || result.Termination.Totals.CostUSD != 0.002 {
		t.Fatalf("Totals = %#v, want both dimensions accumulated", result.Termination.Totals)
	}
}

func TestRunCompactionBudgetStopsBeforeRetry(t *testing.T) {
	provider := &scriptedProvider{steps: []providerStep{
		{err: pierrors.Wrap(pierrors.ErrorCodeAIContextOverflow, "test", errors.New("too long"))},
		{response: budgetAssistant("old work summarized", budgetUsage(1000, 1000))},
		{response: budgetAssistant("unreachable", budgetUsage(1, 1))},
	}}
	runtime := newPublicAgent(t, provider, &fakeToolRuntime{})
	request := recoveryAgentRequest()
	request.Limits.MaxCostUSD = 0.001

	result, err := runtime.Run(context.Background(), request, nil)
	if !errors.Is(err, pierrors.ErrRunLimitExceeded) || result.Termination.Reason != pi.RunTerminationMaxCost {
		t.Fatalf("error/reason = %v/%q", err, result.Termination.Reason)
	}
	if provider.callCount() != 2 {
		t.Fatalf("provider calls = %d, want no retry after over-budget compaction", provider.callCount())
	}
	if len(result.Invocations) != 1 || result.Invocations[0].Phase != pi.ModelInvocationPhaseCompaction {
		t.Fatalf("Invocations = %#v, want the compaction invocation kept", result.Invocations)
	}
	if len(result.NewMessages) != 0 {
		t.Fatalf("NewMessages = %#v, want none", result.NewMessages)
	}
}

func TestRunCompactionRetryStaysUnderSameGovernor(t *testing.T) {
	provider := &scriptedProvider{steps: []providerStep{
		{err: pierrors.Wrap(pierrors.ErrorCodeAIContextOverflow, "test", errors.New("too long"))},
		{response: budgetAssistant("old work summarized", budgetUsage(500, 500))},
		{response: budgetAssistant("done", budgetUsage(1000, 1000))},
	}}
	runtime := newPublicAgent(t, provider, &fakeToolRuntime{})
	request := recoveryAgentRequest()
	request.Limits.MaxTotalTokens = 2500

	result, err := runtime.Run(context.Background(), request, nil)
	if !errors.Is(err, pierrors.ErrRunLimitExceeded) || result.Termination.Reason != pi.RunTerminationMaxTotalTokens {
		t.Fatalf("error/reason = %v/%q", err, result.Termination.Reason)
	}
	if provider.callCount() != 3 {
		t.Fatalf("provider calls = %d, want compaction and retried action", provider.callCount())
	}
	if len(result.Invocations) != 2 ||
		result.Invocations[0].Phase != pi.ModelInvocationPhaseCompaction ||
		result.Invocations[1].Phase != pi.ModelInvocationPhaseAction {
		t.Fatalf("Invocations = %#v", result.Invocations)
	}
	if result.Termination.Totals.TotalTokens != 3000 {
		t.Fatalf("Totals = %#v, want compaction plus retried action", result.Termination.Totals)
	}
	if len(result.NewMessages) != 1 {
		t.Fatalf("NewMessages = %#v, want the complete final assistant kept", result.NewMessages)
	}
}

func TestRunCompactionInvocationSurvivesRetryFailure(t *testing.T) {
	provider := &scriptedProvider{steps: []providerStep{
		{err: pierrors.Wrap(pierrors.ErrorCodeAIContextOverflow, "test", errors.New("too long"))},
		{response: budgetAssistant("old work summarized", budgetUsage(500, 500))},
		{err: pierrors.Wrap(pierrors.ErrorCodeAIUnauthorized, "test", errors.New("unauthorized"))},
	}}
	runtime := newPublicAgent(t, provider, &fakeToolRuntime{})
	request := recoveryAgentRequest()
	request.Limits.MaxCostUSD = 1

	result, err := runtime.Run(context.Background(), request, nil)
	if pierrors.ErrorCodeOf(err) != pierrors.ErrorCodeAIUnauthorized {
		t.Fatalf("Run() error = %v, want unauthorized", err)
	}
	if result.Termination.Reason != pi.RunTerminationError {
		t.Fatalf("reason = %q, want error", result.Termination.Reason)
	}
	if len(result.Invocations) != 1 || result.Invocations[0].Phase != pi.ModelInvocationPhaseCompaction ||
		result.Termination.Totals.TotalTokens != 1000 {
		t.Fatalf("Invocations/Totals = %#v/%#v", result.Invocations, result.Termination.Totals)
	}
}

func TestRunThinkingCompactionBudgetChecked(t *testing.T) {
	provider := &scriptedProvider{steps: []providerStep{
		{err: pierrors.Wrap(pierrors.ErrorCodeAIContextOverflow, "test", errors.New("too long"))},
		{response: budgetAssistant("old work summarized", budgetUsage(1000, 1000))},
		{response: budgetAssistant("unreachable", budgetUsage(1, 1))},
	}}
	runtime := newPublicAgentWithThinking(t, provider, &fakeToolRuntime{}, true)
	request := recoveryAgentRequest()
	request.Limits.MaxTotalTokens = 1000

	result, err := runtime.Run(context.Background(), request, nil)
	if !errors.Is(err, pierrors.ErrRunLimitExceeded) || result.Termination.Reason != pi.RunTerminationMaxTotalTokens {
		t.Fatalf("error/reason = %v/%q", err, result.Termination.Reason)
	}
	if provider.callCount() != 2 {
		t.Fatalf("provider calls = %d, want no retry and no action", provider.callCount())
	}
	if len(result.Invocations) != 1 || result.Invocations[0].Phase != pi.ModelInvocationPhaseCompaction {
		t.Fatalf("Invocations = %#v", result.Invocations)
	}
}

func TestRunTotalsMatchInvocationDetails(t *testing.T) {
	call := ai.ToolCall{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"a.txt"}`)}
	provider := &fakeProvider{responses: []*ai.Message{
		budgetAssistant("plan", budgetUsage(10, 5)),
		budgetToolCall("call-1", "read", "reading", budgetUsage(20, 10)),
		budgetAssistant("plan two", budgetUsage(30, 15)),
		budgetAssistant("done", budgetUsage(40, 20)),
	}}
	toolRuntime := &fakeToolRuntime{
		definitions: []ai.ToolDefinition{{Name: "read"}},
		results:     map[string]pi.ToolResult{"read": toolResult(call, "file A", false)},
	}
	runtime := newPublicAgentWithThinking(t, provider, toolRuntime, true)

	result, err := runtime.Run(context.Background(), validAgentRequest("read then answer"), nil)
	if err != nil {
		t.Fatal(err)
	}
	totals := result.Termination.Totals
	if totals.Invocations != uint32(len(result.Invocations)) || totals.Invocations != 4 {
		t.Fatalf("Totals.Invocations = %d, want %d", totals.Invocations, len(result.Invocations))
	}
	var input, output int64
	var cost float64
	for index, invocation := range result.Invocations {
		if invocation.Sequence != uint32(index+1) {
			t.Fatalf("invocation %d sequence = %d", index, invocation.Sequence)
		}
		input += invocation.Usage.InputTokens
		output += invocation.Usage.OutputTokens
		cost += invocation.Usage.CostUSD
	}
	if totals.InputTokens != input || totals.OutputTokens != output ||
		totals.TotalTokens != totals.InputTokens+totals.OutputTokens {
		t.Fatalf("Totals = %#v, want sums %d/%d", totals, input, output)
	}
	if math.Abs(totals.CostUSD-cost) > 1e-12 {
		t.Fatalf("Totals.CostUSD = %v, want %v", totals.CostUSD, cost)
	}
	if result.Termination.Reason != pi.RunTerminationCompleted || totals.Turns != 2 {
		t.Fatalf("Termination = %#v", result.Termination)
	}
}

func TestRunCancellationBeforeLoopReportsZeroTermination(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	provider := &fakeProvider{responses: []*ai.Message{budgetAssistant("done", budgetUsage(1, 1))}}
	runtime := newPublicAgent(t, provider, &fakeToolRuntime{})

	result, err := runtime.Run(ctx, validAgentRequest("hello"), nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want canceled", err)
	}
	if result.Termination.Reason != pi.RunTerminationCanceled || result.Termination.Totals != (pi.RunTotals{}) {
		t.Fatalf("Termination = %#v, want canceled with zero totals", result.Termination)
	}
	if len(provider.requests) != 0 {
		t.Fatalf("provider calls = %d, want 0", len(provider.requests))
	}
}

func TestRunConcurrentGovernorIsolation(t *testing.T) {
	runtime := newPublicAgent(t, &echoAgentProvider{}, &fakeToolRuntime{})

	const runs = 8
	results := make([]pi.RunResult, runs)
	runErrors := make([]error, runs)
	var wait sync.WaitGroup
	for index := range runs {
		wait.Add(1)
		go func() {
			defer wait.Done()
			request := validAgentRequest("hello")
			request.Limits = pi.RunLimits{MaxTurns: 2, MaxCostUSD: 0.5, MaxTotalTokens: 100}
			results[index], runErrors[index] = runtime.Run(context.Background(), request, nil)
		}()
	}
	wait.Wait()
	for index := range runs {
		if runErrors[index] != nil {
			t.Fatalf("Run(%d) error = %v", index, runErrors[index])
		}
		totals := results[index].Termination.Totals
		if results[index].Termination.Reason != pi.RunTerminationCompleted ||
			totals.Invocations != 1 || totals.Turns != 1 {
			t.Fatalf("Run(%d) termination = %#v, want isolated single-call totals", index, results[index].Termination)
		}
	}
}

func reflectEventTypes(got, want []pi.AgentEventType) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}
