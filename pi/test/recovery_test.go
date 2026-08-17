package test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/harness"
	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
)

type providerStep struct {
	response *ai.Message
	err      error
}

type scriptedProvider struct {
	mu        sync.Mutex
	steps     []providerStep
	requests  [][]ai.Message
	tools     [][]ai.ToolDefinition
	afterCall func(int)
}

func (p *scriptedProvider) Stream(
	_ context.Context,
	messages []ai.Message,
	tools []ai.ToolDefinition,
) ai.Stream {
	p.mu.Lock()
	call := len(p.requests) + 1
	p.requests = append(p.requests, append([]ai.Message(nil), messages...))
	p.tools = append(p.tools, append([]ai.ToolDefinition(nil), tools...))
	step := p.steps[call-1]
	p.mu.Unlock()

	if p.afterCall != nil {
		p.afterCall(call)
	}
	return newTestStream(withTestUsage(step.response), step.err)
}

func (p *scriptedProvider) snapshots() ([][]ai.Message, [][]ai.ToolDefinition) {
	p.mu.Lock()
	defer p.mu.Unlock()
	requests := make([][]ai.Message, len(p.requests))
	for index := range p.requests {
		requests[index] = append([]ai.Message(nil), p.requests[index]...)
	}
	tools := make([][]ai.ToolDefinition, len(p.tools))
	for index := range p.tools {
		tools[index] = append([]ai.ToolDefinition(nil), p.tools[index]...)
	}
	return requests, tools
}

func (p *scriptedProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.requests)
}

func TestLoopRetriesTransientGenerationTwice(t *testing.T) {
	provider := &scriptedProvider{steps: []providerStep{
		{err: pierrors.Wrap(pierrors.ErrorCodeAITransient, "test", errors.New("first"))},
		{err: pierrors.Wrap(pierrors.ErrorCodeAIRateLimited, "test", errors.New("second"))},
		{response: &ai.Message{Role: ai.RoleAssistant, Content: blocks("done")}},
	}}
	loop := pi.NewLoop(provider, pi.NewScheduler(&fakeToolRuntime{}, 1), false)
	started := time.Now()
	messages, err := loop.Run(context.Background(), harness.Context{Messages: []ai.Message{{
		Role: ai.RoleUser, Content: blocks("run"),
	}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if provider.callCount() != 3 || len(messages) != 1 {
		t.Fatalf("calls/messages = %d/%#v, want 3/one", provider.callCount(), messages)
	}
	if elapsed := time.Since(started); elapsed < 1400*time.Millisecond {
		t.Fatalf("elapsed = %s, want both retry delays", elapsed)
	}
}

func TestLoopDoesNotRetryTerminalAICode(t *testing.T) {
	provider := &scriptedProvider{steps: []providerStep{{
		err: pierrors.Wrap(pierrors.ErrorCodeAIUnauthorized, "test", errors.New("unauthorized")),
	}}}
	loop := pi.NewLoop(provider, pi.NewScheduler(&fakeToolRuntime{}, 1), false)
	_, err := loop.Run(context.Background(), harness.Context{Messages: []ai.Message{{
		Role: ai.RoleUser, Content: blocks("run"),
	}}}, nil)
	if pierrors.ErrorCodeOf(err) != pierrors.ErrorCodeAIUnauthorized || provider.callCount() != 1 {
		t.Fatalf("error/calls = %v/%d", err, provider.callCount())
	}
}

func TestLoopCancelsDuringRetryBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	provider := &scriptedProvider{
		steps: []providerStep{{
			err: pierrors.Wrap(pierrors.ErrorCodeAITransient, "test", errors.New("temporary")),
		}},
		afterCall: func(call int) {
			if call == 1 {
				cancel()
			}
		},
	}
	loop := pi.NewLoop(provider, pi.NewScheduler(&fakeToolRuntime{}, 1), false)
	started := time.Now()
	_, err := loop.Run(ctx, harness.Context{Messages: []ai.Message{{
		Role: ai.RoleUser, Content: blocks("run"),
	}}}, nil)
	if !errors.Is(err, context.Canceled) || provider.callCount() != 1 {
		t.Fatalf("error/calls = %v/%d", err, provider.callCount())
	}
	if elapsed := time.Since(started); elapsed >= 500*time.Millisecond {
		t.Fatalf("elapsed = %s, retry backoff ignored cancellation", elapsed)
	}
}

func TestAgentCompactsOnceAfterContextOverflow(t *testing.T) {
	provider := &scriptedProvider{steps: []providerStep{
		{err: pierrors.Wrap(pierrors.ErrorCodeAIContextOverflow, "test", errors.New("too long"))},
		{response: &ai.Message{Role: ai.RoleAssistant, Content: blocks("old work summarized"), Usage: costedUsage(1)}},
		{response: &ai.Message{Role: ai.RoleAssistant, Content: blocks("done"), Usage: costedUsage(2)}},
	}}
	runtime := newPublicAgent(t, provider, &agentToolRuntimeFake{})
	result, err := runtime.Run(context.Background(), recoveryAgentRequest(), nil)
	if err != nil {
		t.Fatal(err)
	}
	requests, tools := provider.snapshots()
	if len(requests) != 3 {
		t.Fatalf("provider calls = %d, want 3", len(requests))
	}
	if len(tools[1]) != 0 || !messagesContain(requests[1], "请总结所提供的早期对话") {
		t.Fatalf("summary request/tools = %#v/%#v", requests[1], tools[1])
	}
	if !messagesContain(requests[2], "# Earlier conversation summary\nold work summarized") ||
		!messagesContain(requests[2], "current question") {
		t.Fatalf("retried request = %#v", requests[2])
	}
	if len(result.Invocations) != 2 ||
		result.Invocations[0].Sequence != 1 || result.Invocations[0].Phase != pi.ModelInvocationPhaseCompaction ||
		result.Invocations[1].Sequence != 2 || result.Invocations[1].Phase != pi.ModelInvocationPhaseAction {
		t.Fatalf("Invocations = %#v", result.Invocations)
	}
	if len(result.NewMessages) != 1 || messagesContain(result.NewMessages, "old work summarized") {
		t.Fatalf("NewMessages = %#v", result.NewMessages)
	}
}

func TestAgentReturnsSummaryFailureWithoutFallback(t *testing.T) {
	provider := &scriptedProvider{steps: []providerStep{
		{err: pierrors.Wrap(pierrors.ErrorCodeAIContextOverflow, "test", errors.New("too long"))},
		{err: pierrors.Wrap(pierrors.ErrorCodeAIUnauthorized, "test", errors.New("summary failed"))},
	}}
	runtime := newPublicAgent(t, provider, &agentToolRuntimeFake{})
	result, err := runtime.Run(context.Background(), recoveryAgentRequest(), nil)
	if pierrors.ErrorCodeOf(err) != pierrors.ErrorCodeAIUnauthorized || provider.callCount() != 2 {
		t.Fatalf("error/calls = %v/%d", err, provider.callCount())
	}
	if len(result.NewMessages) != 0 || len(result.Invocations) != 0 {
		t.Fatalf("result = %#v", result)
	}
}

func TestAgentDoesNotCompactAgainWhenRetriedRequestOverflows(t *testing.T) {
	provider := &scriptedProvider{steps: []providerStep{
		{err: pierrors.Wrap(pierrors.ErrorCodeAIContextOverflow, "test", errors.New("too long"))},
		{response: &ai.Message{Role: ai.RoleAssistant, Content: blocks("old work summarized"), Usage: costedUsage(1)}},
		{err: pierrors.Wrap(pierrors.ErrorCodeAIContextOverflow, "test", errors.New("still too long"))},
	}}
	runtime := newPublicAgent(t, provider, &agentToolRuntimeFake{})
	result, err := runtime.Run(context.Background(), recoveryAgentRequest(), nil)
	if pierrors.ErrorCodeOf(err) != pierrors.ErrorCodeAIContextOverflow || provider.callCount() != 3 {
		t.Fatalf("error/calls = %v/%d", err, provider.callCount())
	}
	if len(result.NewMessages) != 0 || len(result.Invocations) != 1 ||
		result.Invocations[0].Sequence != 1 || result.Invocations[0].Phase != pi.ModelInvocationPhaseCompaction {
		t.Fatalf("result = %#v", result)
	}
}

func TestAgentReturnsOriginalOverflowWithoutOldHistory(t *testing.T) {
	provider := &scriptedProvider{steps: []providerStep{{
		err: pierrors.Wrap(pierrors.ErrorCodeAIContextOverflow, "test", errors.New("current input is too long")),
	}}}
	runtime := newPublicAgent(t, provider, &agentToolRuntimeFake{})
	result, err := runtime.Run(context.Background(), validAgentRequest("current question"), nil)
	if pierrors.ErrorCodeOf(err) != pierrors.ErrorCodeAIContextOverflow || provider.callCount() != 1 {
		t.Fatalf("error/calls = %v/%d", err, provider.callCount())
	}
	if len(result.NewMessages) != 0 || len(result.Invocations) != 0 {
		t.Fatalf("result = %#v", result)
	}
}

func recoveryAgentRequest() pi.RunRequest {
	request := validAgentRequest("current question")
	request.History = []pi.Message{
		{ContentType: "text", Content: "old question", SenderType: "customer"},
		{ContentType: "text", Content: "old answer", SenderType: "ai"},
	}
	return request
}

func messagesContain(messages []ai.Message, fragment string) bool {
	for _, message := range messages {
		text, _ := ai.TextContent(message.Content)
		if strings.Contains(text, fragment) {
			return true
		}
	}
	return false
}

func TestLoopInjectsToolRecoveryHintOnlyIntoProviderContext(t *testing.T) {
	call := ai.ToolCall{ID: "call-1", Name: "edit", Arguments: json.RawMessage(`{"path":"a"}`)}
	provider := &fakeProvider{responses: []*ai.Message{
		{Role: ai.RoleAssistant, ToolCalls: []ai.ToolCall{call}},
		{Role: ai.RoleAssistant, Content: blocks("done")},
	}}
	toolRuntime := &fakeToolRuntime{
		definitions: []ai.ToolDefinition{{Name: "edit"}},
		results: map[string]pi.ToolResult{"edit": {
			ToolCallID: "call-1",
			ToolName:   "edit",
			Content:    blocks("在文件中未找到 oldText"),
			IsError:    true,
			ErrorCode:  pierrors.ErrorCodeToolEditNoMatch,
		}},
	}
	reporter := &recordingReporter{}
	loop := pi.NewLoop(provider, pi.NewScheduler(toolRuntime, 1), false)
	newMessages, err := loop.Run(context.Background(), harness.Context{
		Messages: []ai.Message{{Role: ai.RoleUser, Content: blocks("edit file")}},
		Tools:    toolRuntime.Definitions(),
	}, reporter)
	if err != nil {
		t.Fatal(err)
	}
	if len(newMessages) != 3 || messageText(t, newMessages[1]) != "在文件中未找到 oldText" ||
		strings.Contains(messageText(t, newMessages[1]), "Recovery Hint") {
		t.Fatalf("NewMessages = %#v", newMessages)
	}
	observation := provider.requests[1][len(provider.requests[1])-1]
	if !strings.Contains(messageText(t, observation), "在文件中未找到 oldText") ||
		!strings.Contains(messageText(t, observation), "先使用 read") {
		t.Fatalf("provider observation = %#v", observation)
	}
	var toolEnd *pi.ToolResult
	for _, event := range reporter.Events() {
		if event.Type == pi.AgentEventToolEnd {
			toolEnd = event.Tool.Result
		}
	}
	if toolEnd == nil || toolResultText(t, *toolEnd) != "在文件中未找到 oldText" ||
		strings.Contains(toolResultText(t, *toolEnd), "Recovery Hint") {
		t.Fatalf("ToolEnd result = %#v", toolEnd)
	}
	if len(toolRuntime.Calls()) != 1 {
		t.Fatalf("tool calls = %d", len(toolRuntime.Calls()))
	}
}

func TestLoopInjectsToolRecoveryHintSkipsUnknownCode(t *testing.T) {
	call := ai.ToolCall{ID: "call-1", Name: "custom", Arguments: json.RawMessage(`{}`)}
	provider := &fakeProvider{responses: []*ai.Message{
		{Role: ai.RoleAssistant, ToolCalls: []ai.ToolCall{call}},
		{Role: ai.RoleAssistant, Content: blocks("done")},
	}}
	toolRuntime := &fakeToolRuntime{
		definitions: []ai.ToolDefinition{{Name: "custom"}},
		results: map[string]pi.ToolResult{"custom": {
			ToolCallID: "call-1",
			ToolName:   "custom",
			Content:    blocks("custom failed"),
			IsError:    true,
			ErrorCode:  pierrors.ErrorCodeToolRuntime,
		}},
	}
	loop := pi.NewLoop(provider, pi.NewScheduler(toolRuntime, 1), false)
	_, err := loop.Run(context.Background(), harness.Context{
		Messages: []ai.Message{{Role: ai.RoleUser, Content: blocks("run")}},
		Tools:    toolRuntime.Definitions(),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	observation := provider.requests[1][len(provider.requests[1])-1]
	if messageText(t, observation) != "custom failed" {
		t.Fatalf("provider observation = %#v", observation)
	}
}
