package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/PycMono/go-reagent/pi/agent"
	"github.com/PycMono/go-reagent/pi/ai"
)

type agentFactoryFake struct {
	mu         sync.Mutex
	calls      int
	requests   []agent.RunRequest
	runContext agent.RunContext
	err        error
}

func (f *agentFactoryFake) Create(
	_ context.Context,
	request agent.RunRequest,
	definitions []ai.ToolDefinition,
) (agent.RunContext, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.requests = append(f.requests, request)
	if f.err != nil {
		return agent.RunContext{}, f.err
	}
	if f.runContext.Messages != nil || f.runContext.Tools != nil || f.runContext.Metadata != nil {
		return f.runContext, nil
	}
	messages := append([]ai.Message(nil), request.History...)
	messages = append(messages, request.Input)
	return agent.RunContext{Messages: messages, Tools: definitions, Metadata: request.Metadata}, nil
}

type agentClientFake struct {
	mu        sync.Mutex
	responses []*ai.Message
	requests  [][]ai.Message
}

func (c *agentClientFake) Generate(
	_ context.Context,
	messages []ai.Message,
	_ []ai.ToolDefinition,
) (*ai.Message, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests = append(c.requests, messages)
	index := len(c.requests) - 1
	if index >= len(c.responses) {
		return nil, fmt.Errorf("unexpected client call %d", index+1)
	}
	return withTestUsage(c.responses[index]), nil
}

type agentRegistryFake struct {
	definitions []ai.ToolDefinition
}

func (r *agentRegistryFake) GetAvailableTools() []ai.ToolDefinition {
	return append([]ai.ToolDefinition(nil), r.definitions...)
}

func (*agentRegistryFake) Execute(_ context.Context, call ai.ToolCall, _ agent.ToolEventObserver) (agent.ToolResult, error) {
	return agent.ToolResult{
		ToolCallID: call.ID,
		ToolName:   call.Name,
		Content:    []ai.ContentBlock{ai.TextBlock("tool is not registered")},
		IsError:    true,
	}, nil
}

func newPublicAgent(t *testing.T, factory agent.ContextFactory, client ai.Client, registry agent.Registry) *agent.Agent {
	t.Helper()
	loop := agent.NewLoop(client, agent.NewScheduler(registry, 2), false)
	runtime, err := agent.New(factory, loop, registry)
	if err != nil {
		t.Fatalf("agent.New() error = %v", err)
	}
	return runtime
}

func validAgentRequest(runID, input string) agent.RunRequest {
	return agent.RunRequest{
		RunID: runID,
		Input: ai.Message{
			Role:    ai.RoleUser,
			Content: []ai.ContentBlock{ai.TextBlock(input)},
		},
	}
}

func TestAgentRunReturnsEveryCostedInvocationInCallOrder(t *testing.T) {
	call := ai.ToolCall{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"AGENTS.md"}`)}
	client := &agentClientFake{responses: []*ai.Message{
		{Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("private thinking one")}, Usage: costedUsage(1)},
		{Role: ai.RoleAssistant, ToolCalls: []ai.ToolCall{call}, Usage: costedUsage(2)},
		{Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("private thinking two")}, Usage: costedUsage(3)},
		{Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("done")}, Usage: costedUsage(4)},
	}}
	registry := &agentRegistryFake{definitions: []ai.ToolDefinition{{Name: "read"}}}
	factory := &agentFactoryFake{runContext: agent.RunContext{
		Messages: []ai.Message{{Role: ai.RoleUser, Content: []ai.ContentBlock{ai.TextBlock("run")}}},
		Tools:    registry.definitions,
	}}
	loop := agent.NewLoop(client, agent.NewScheduler(registry, 1), true)
	runtime, err := agent.New(factory, loop, registry)
	if err != nil {
		t.Fatal(err)
	}
	reporter := &recordingReporter{}
	result, err := runtime.Run(context.Background(), validAgentRequest("run-cost", "run"), reporter)
	if err != nil {
		t.Fatal(err)
	}

	wantPhases := []agent.ModelInvocationPhase{
		agent.ModelInvocationPhaseThinking,
		agent.ModelInvocationPhaseAction,
		agent.ModelInvocationPhaseThinking,
		agent.ModelInvocationPhaseAction,
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
		if event.Type == agent.AgentEventMessage {
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
	factory := &agentFactoryFake{}
	response := &ai.Message{Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("done")}}
	client := &agentClientFake{responses: []*ai.Message{response}}
	runtime := newPublicAgent(t, factory, client, &agentRegistryFake{})

	result, err := runtime.Run(context.Background(), validAgentRequest("run-1", "do work"), nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.RunID != "run-1" || len(result.NewMessages) != 1 {
		t.Fatalf("RunResult = %#v", result)
	}
	text, err := ai.TextContent(result.NewMessages[0].Content)
	if err != nil || text != "done" {
		t.Fatalf("assistant content/error = %q, %v", text, err)
	}
	if factory.calls != 1 || len(factory.requests) != 1 || factory.requests[0].RunID != "run-1" {
		t.Fatalf("factory calls/requests = %d, %#v", factory.calls, factory.requests)
	}
}

func TestAgentRunRejectsInvalidRequestBeforeContextCreation(t *testing.T) {
	factory := &agentFactoryFake{}
	runtime := newPublicAgent(t, factory, &agentClientFake{}, &agentRegistryFake{})

	result, err := runtime.Run(context.Background(), agent.RunRequest{RunID: "invalid"}, nil)
	if !errors.Is(err, agent.ErrRequestInvalid) {
		t.Fatalf("Run() error = %v, want ErrRequestInvalid", err)
	}
	if result.RunID != "invalid" || len(result.NewMessages) != 0 || factory.calls != 0 {
		t.Fatalf("result/factory calls = %#v, %d", result, factory.calls)
	}
}

func TestAgentRunPreservesPartialMessagesOnClientFailure(t *testing.T) {
	call := ai.ToolCall{ID: "call-1", Name: "missing", Arguments: json.RawMessage(`{}`)}
	client := &agentClientFake{responses: []*ai.Message{{
		Role:      ai.RoleAssistant,
		ToolCalls: []ai.ToolCall{call},
	}}}
	runtime := newPublicAgent(t, &agentFactoryFake{}, client, &agentRegistryFake{
		definitions: []ai.ToolDefinition{{Name: "missing"}},
	})

	result, err := runtime.Run(context.Background(), validAgentRequest("run-partial", "hello"), nil)
	if err == nil || !errors.Is(err, ai.ErrGeneration) {
		t.Fatalf("Run() error = %v, want ai.ErrGeneration", err)
	}
	if result.RunID != "run-partial" || len(result.NewMessages) != 2 {
		t.Fatalf("RunResult = %#v, want tool call and completed tool result", result)
	}
	if result.NewMessages[0].ToolCalls[0].ID != "call-1" || result.NewMessages[1].ToolCallID != "call-1" {
		t.Fatalf("partial messages = %#v", result.NewMessages)
	}
	if len(result.Invocations) != 1 || result.Invocations[0].Sequence != 1 ||
		result.Invocations[0].Phase != agent.ModelInvocationPhaseAction {
		t.Fatalf("partial invocations = %#v, want completed first Action call", result.Invocations)
	}
}

func TestAgentRunClonesCallerAndProviderOwnedValues(t *testing.T) {
	factory := &agentFactoryFake{}
	response := &ai.Message{
		Role:    ai.RoleAssistant,
		Content: []ai.ContentBlock{ai.TextBlock("done")},
		Usage:   costedUsage(1),
	}
	client := &agentClientFake{responses: []*ai.Message{response}}
	runtime := newPublicAgent(t, factory, client, &agentRegistryFake{})
	request := validAgentRequest("run-clone", "hello")
	request.History = []ai.Message{{
		Role: ai.RoleAssistant,
		ToolCalls: []ai.ToolCall{{
			ID: "history-call", Name: "read", Arguments: json.RawMessage(`{"path":"history"}`),
		}},
	}}
	request.Context = []agent.ContextBlock{{Name: "profile", Content: "gold"}}
	request.Metadata = map[string]string{"tenant": "one"}

	result, err := runtime.Run(context.Background(), request, nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	request.History[0].ToolCalls[0].Arguments[0] = 'x'
	request.Input.Content[0].Text = "changed"
	request.Context[0].Content = "changed"
	request.Metadata["tenant"] = "changed"
	response.Content[0].Text = "changed"
	response.Usage.PlatformID = "changed"

	captured := factory.requests[0]
	if string(captured.History[0].ToolCalls[0].Arguments) != `{"path":"history"}` ||
		captured.Input.Content[0].Text != "hello" ||
		captured.Context[0].Content != "gold" || captured.Metadata["tenant"] != "one" {
		t.Fatalf("captured request mutated = %#v", captured)
	}
	if result.NewMessages[0].Content[0].Text != "done" {
		t.Fatalf("result mutated = %#v", result.NewMessages)
	}
	if result.NewMessages[0].Usage == nil || result.NewMessages[0].Usage.PlatformID != "test" {
		t.Fatalf("result Usage mutated = %#v", result.NewMessages[0].Usage)
	}
}

func TestAgentRunSupportsConcurrentIndependentRequests(t *testing.T) {
	const runs = 16
	factory := &agentFactoryFake{}
	client := &echoAgentClient{}
	runtime := newPublicAgent(t, factory, client, &agentRegistryFake{})
	results := make([]agent.RunResult, runs)
	errorsByRun := make([]error, runs)

	var wait sync.WaitGroup
	for index := range runs {
		wait.Add(1)
		go func() {
			defer wait.Done()
			request := validAgentRequest(fmt.Sprintf("run-%d", index), fmt.Sprintf("input-%d", index))
			results[index], errorsByRun[index] = runtime.Run(context.Background(), request, nil)
		}()
	}
	wait.Wait()

	for index := range runs {
		if errorsByRun[index] != nil {
			t.Fatalf("Run(%d) error = %v", index, errorsByRun[index])
		}
		if results[index].RunID != fmt.Sprintf("run-%d", index) {
			t.Fatalf("Run(%d) result = %#v", index, results[index])
		}
		text, err := ai.TextContent(results[index].NewMessages[0].Content)
		if err != nil || text != fmt.Sprintf("done:input-%d", index) {
			t.Fatalf("Run(%d) content/error = %q, %v", index, text, err)
		}
	}
}

type echoAgentClient struct{}

func (*echoAgentClient) Generate(_ context.Context, messages []ai.Message, _ []ai.ToolDefinition) (*ai.Message, error) {
	text, err := ai.TextContent(messages[len(messages)-1].Content)
	if err != nil {
		return nil, err
	}
	return &ai.Message{
		Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("done:" + text)}, Usage: costedUsage(0),
	}, nil
}
