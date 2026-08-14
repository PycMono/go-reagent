package test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/harness"
	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
)

type agentProviderFake struct {
	mu        sync.Mutex
	responses []*ai.Message
	requests  [][]ai.Message
}

func (p *agentProviderFake) Generate(
	_ context.Context,
	messages []ai.Message,
	_ []ai.ToolDefinition,
) (*ai.Message, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requests = append(p.requests, messages)
	index := len(p.requests) - 1
	if index >= len(p.responses) {
		return nil, fmt.Errorf("unexpected provider call %d", index+1)
	}
	return withTestUsage(p.responses[index]), nil
}

type agentToolRuntimeFake struct {
	definitions []ai.ToolDefinition
}

func (r *agentToolRuntimeFake) Definitions() []ai.ToolDefinition {
	return append([]ai.ToolDefinition(nil), r.definitions...)
}

func (*agentToolRuntimeFake) Execute(_ context.Context, call ai.ToolCall, _ pi.ToolEventObserver) (pi.ToolResult, error) {
	return pi.ToolResult{
		ToolCallID: call.ID,
		ToolName:   call.Name,
		Content:    []ai.ContentBlock{ai.TextBlock("tool is not registered")},
		IsError:    true,
	}, nil
}

func newPublicAgent(t *testing.T, provider ai.Provider, toolRuntime pi.ToolRuntime) *pi.Agent {
	t.Helper()
	workDir := t.TempDir()
	writeValidAgentWorkspace(workDir)
	toolRuntime = toolRuntimeWithRequiredRead{ToolRuntime: toolRuntime}
	builder := harness.NewContextBuilder(harness.NewPromptComposer(workDir), workDir)
	loop := pi.NewLoop(provider, pi.NewScheduler(toolRuntime, 2), false)
	return pi.New(builder, loop, toolRuntime)
}

func validAgentRequest(input string) pi.RunRequest {
	return pi.RunRequest{Input: pi.Message{
		ContentType: "text",
		Content:     input,
		SenderType:  "customer",
	}}
}

func TestAgentRunReturnsEveryCostedInvocationInCallOrder(t *testing.T) {
	call := ai.ToolCall{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"AGENTS.md"}`)}
	provider := &agentProviderFake{responses: []*ai.Message{
		{Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("private thinking one")}, Usage: costedUsage(1)},
		{Role: ai.RoleAssistant, ToolCalls: []ai.ToolCall{call}, Usage: costedUsage(2)},
		{Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("private thinking two")}, Usage: costedUsage(3)},
		{Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("done")}, Usage: costedUsage(4)},
	}}
	toolRuntime := toolRuntimeWithRequiredRead{ToolRuntime: &agentToolRuntimeFake{definitions: []ai.ToolDefinition{{Name: "read"}}}}
	workDir := t.TempDir()
	writeValidAgentWorkspace(workDir)
	builder := harness.NewContextBuilder(harness.NewPromptComposer(workDir), workDir)
	loop := pi.NewLoop(provider, pi.NewScheduler(toolRuntime, 1), true)
	runtime := pi.New(builder, loop, toolRuntime)
	reporter := &recordingReporter{}
	result, err := runtime.Run(context.Background(), validAgentRequest("run"), reporter)
	if err != nil {
		t.Fatal(err)
	}

	wantPhases := []pi.ModelInvocationPhase{
		pi.ModelInvocationPhaseThinking,
		pi.ModelInvocationPhaseAction,
		pi.ModelInvocationPhaseThinking,
		pi.ModelInvocationPhaseAction,
	}
	if len(result.Invocations) != 4 {
		t.Fatalf("Invocations = %#v", result.Invocations)
	}
	for index, invocation := range result.Invocations {
		if invocation.Sequence != uint32(index+1) || invocation.Phase != wantPhases[index] ||
			invocation.Usage.InputTokens != int64(index+1) {
			t.Fatalf("invocation %d = %#v", index, invocation)
		}
	}
	if len(result.NewMessages) != 3 {
		t.Fatalf("NewMessages = %#v", result.NewMessages)
	}
	for _, message := range result.NewMessages {
		text, _ := ai.TextContent(message.Content)
		if text == "private thinking one" || text == "private thinking two" {
			t.Fatalf("Thinking leaked: %#v", result.NewMessages)
		}
	}
	messageEventCount := 0
	for _, event := range reporter.Events() {
		if event.Type == pi.AgentEventMessage {
			messageEventCount++
			if event.Message == nil || event.Message.Content[0].Text != "done" {
				t.Fatalf("message event = %#v", event)
			}
		}
	}
	if messageEventCount != 1 {
		t.Fatalf("message event count = %d, want 1", messageEventCount)
	}
}

func costedUsage(inputTokens int64) *ai.Usage {
	return &ai.Usage{
		InputTokens:                   inputTokens,
		InputPriceUSDPerMillionTokens: 1,
		CostUSD:                       float64(inputTokens) / 1_000_000,
		PlatformID:                    "test",
		Model:                         "model",
	}
}

func withTestUsage(message *ai.Message) *ai.Message {
	if message == nil || message.Usage != nil {
		return message
	}
	cloned := *message
	cloned.Usage = costedUsage(0)
	return &cloned
}

func TestAgentRunPreparesRequestAndReturnsNewMessages(t *testing.T) {
	response := &ai.Message{Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("done")}}
	provider := &agentProviderFake{responses: []*ai.Message{response}}
	runtime := newPublicAgent(t, provider, &agentToolRuntimeFake{})

	result, err := runtime.Run(context.Background(), validAgentRequest("do work"), nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(result.NewMessages) != 1 {
		t.Fatalf("RunResult = %#v", result)
	}
	text, err := ai.TextContent(result.NewMessages[0].Content)
	if err != nil || text != "done" {
		t.Fatalf("assistant content/error = %q, %v", text, err)
	}
	requests := provider.requests
	if len(requests) != 1 || len(requests[0]) == 0 || requests[0][len(requests[0])-1].Content[0].Text != "do work" {
		t.Fatalf("provider requests = %#v", requests)
	}
}

func TestAgentRunRejectsInvalidRequestBeforeContextCreation(t *testing.T) {
	runtime := newPublicAgent(t, &agentProviderFake{}, &agentToolRuntimeFake{})

	result, err := runtime.Run(context.Background(), pi.RunRequest{}, nil)
	if !errors.Is(err, pierrors.ErrRequestInvalid) {
		t.Fatalf("Run() error = %v, want ErrRequestInvalid", err)
	}
	if len(result.NewMessages) != 0 {
		t.Fatalf("result = %#v", result)
	}
}

func TestAgentRunPreservesPartialMessagesOnClientFailure(t *testing.T) {
	call := ai.ToolCall{ID: "call-1", Name: "missing", Arguments: json.RawMessage(`{}`)}
	provider := &agentProviderFake{responses: []*ai.Message{{
		Role:      ai.RoleAssistant,
		ToolCalls: []ai.ToolCall{call},
	}}}
	runtime := newPublicAgent(t, provider, &agentToolRuntimeFake{
		definitions: []ai.ToolDefinition{{Name: "missing"}},
	})

	result, err := runtime.Run(context.Background(), validAgentRequest("hello"), nil)
	if err == nil || pierrors.ErrorCodeOf(err) != pierrors.ErrorCodeAIGeneration {
		t.Fatalf("Run() error = %v, want %q", err, pierrors.ErrorCodeAIGeneration)
	}
	if len(result.NewMessages) != 2 {
		t.Fatalf("RunResult = %#v, want tool call and completed tool result", result)
	}
	if result.NewMessages[0].ToolCalls[0].ID != "call-1" || result.NewMessages[1].ToolCallID != "call-1" {
		t.Fatalf("partial messages = %#v", result.NewMessages)
	}
	if len(result.Invocations) != 1 || result.Invocations[0].Sequence != 1 ||
		result.Invocations[0].Phase != pi.ModelInvocationPhaseAction {
		t.Fatalf("partial invocations = %#v, want completed first Action call", result.Invocations)
	}
}

func TestAgentRunMapsHistoryMessagesInOriginalOrder(t *testing.T) {
	response := &ai.Message{
		Role:    ai.RoleAssistant,
		Content: []ai.ContentBlock{ai.TextBlock("done")},
		Usage:   costedUsage(1),
	}
	provider := &agentProviderFake{responses: []*ai.Message{response}}
	runtime := newPublicAgent(t, provider, &agentToolRuntimeFake{})
	request := validAgentRequest("hello")
	request.History = []pi.Message{{
		ContentType: "text",
		SenderType:  "customer",
		Content:     "question",
	}, {
		ContentType: "text",
		SenderType:  "ai",
		Content:     "answer",
	}}
	_, err := runtime.Run(context.Background(), request, nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	captured := provider.requests[0]
	if len(captured) < 4 {
		t.Fatalf("provider request = %#v", captured)
	}
	history := captured[len(captured)-3 : len(captured)-1]
	if history[0].Role != ai.RoleUser || history[0].Content[0].Text != "question" ||
		history[1].Role != ai.RoleAssistant || history[1].Content[0].Text != "answer" ||
		captured[len(captured)-1].Role != ai.RoleUser || captured[len(captured)-1].Content[0].Text != "hello" {
		t.Fatalf("mapped history/input = %#v / %#v", history, captured[len(captured)-1])
	}
}

func TestAgentRunSupportsConcurrentIndependentRequests(t *testing.T) {
	const runs = 16
	provider := &echoAgentProvider{}
	runtime := newPublicAgent(t, provider, &agentToolRuntimeFake{})
	results := make([]pi.RunResult, runs)
	errorsByRun := make([]error, runs)

	var wait sync.WaitGroup
	for index := range runs {
		wait.Add(1)
		go func() {
			defer wait.Done()
			request := validAgentRequest(fmt.Sprintf("input-%d", index))
			results[index], errorsByRun[index] = runtime.Run(context.Background(), request, nil)
		}()
	}
	wait.Wait()

	for index := range runs {
		if errorsByRun[index] != nil {
			t.Fatalf("Run(%d) error = %v", index, errorsByRun[index])
		}
		text, err := ai.TextContent(results[index].NewMessages[0].Content)
		if err != nil || text != fmt.Sprintf("done:input-%d", index) {
			t.Fatalf("Run(%d) content/error = %q, %v", index, text, err)
		}
	}
}

type echoAgentProvider struct{}

func (*echoAgentProvider) Generate(_ context.Context, messages []ai.Message, _ []ai.ToolDefinition) (*ai.Message, error) {
	text, err := ai.TextContent(messages[len(messages)-1].Content)
	if err != nil {
		return nil, err
	}
	return &ai.Message{
		Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("done:" + text)}, Usage: costedUsage(0),
	}, nil
}
