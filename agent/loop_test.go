package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PycMono/go-reagent/agent"
	"github.com/PycMono/go-reagent/ai"
	workspacepkg "github.com/PycMono/go-reagent/internal/workspace"
)

type rawClientFunc func(context.Context, []ai.Message, []ai.ToolDefinition) (*ai.Message, error)

func (f rawClientFunc) Generate(ctx context.Context, messages []ai.Message, tools []ai.ToolDefinition) (*ai.Message, error) {
	return f(ctx, messages, tools)
}

func TestLoopRejectsUnmeteredOrIncorrectlyCostedResponse(t *testing.T) {
	tests := []struct {
		name  string
		usage *ai.Usage
	}{
		{name: "missing"},
		{name: "blank platform", usage: &ai.Usage{Model: "m"}},
		{name: "blank model", usage: &ai.Usage{PlatformID: "p"}},
		{name: "negative input tokens", usage: &ai.Usage{PlatformID: "p", Model: "m", InputTokens: -1}},
		{name: "negative output tokens", usage: &ai.Usage{PlatformID: "p", Model: "m", OutputTokens: -1}},
		{name: "negative latency", usage: &ai.Usage{PlatformID: "p", Model: "m", LatencyMS: -1}},
		{name: "NaN price", usage: &ai.Usage{PlatformID: "p", Model: "m", InputPriceUSDPerMillionTokens: math.NaN()}},
		{name: "infinite cost", usage: &ai.Usage{PlatformID: "p", Model: "m", CostUSD: math.Inf(1)}},
		{name: "incorrect cost", usage: &ai.Usage{
			PlatformID: "p", Model: "m", InputTokens: 1_000_000,
			InputPriceUSDPerMillionTokens: 0.25, CostUSD: 0.20,
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := rawClientFunc(func(context.Context, []ai.Message, []ai.ToolDefinition) (*ai.Message, error) {
				return &ai.Message{
					Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("response")}, Usage: tt.usage,
				}, nil
			})
			loop := agent.NewLoop(client, agent.NewScheduler(&fakeRegistry{}, 1), false)
			messages, err := loop.Run(context.Background(), agent.RunContext{Messages: []ai.Message{{
				Role: ai.RoleUser, Content: []ai.ContentBlock{ai.TextBlock("input")},
			}}}, nil)
			if err == nil || !errors.Is(err, ai.ErrGeneration) {
				t.Fatalf("Run() error = %v, want generation error", err)
			}
			if len(messages) != 0 {
				t.Fatalf("uncosted response accepted: %#v", messages)
			}
		})
	}
}

type fakeProvider struct {
	responses      []*ai.Message
	err            error
	requests       [][]ai.Message
	availableTools [][]ai.ToolDefinition
}

func (p *fakeProvider) Generate(
	_ context.Context,
	messages []ai.Message,
	availableTools []ai.ToolDefinition,
) (*ai.Message, error) {
	p.requests = append(p.requests, append([]ai.Message(nil), messages...))
	p.availableTools = append(p.availableTools, append([]ai.ToolDefinition(nil), availableTools...))
	if p.err != nil {
		return nil, p.err
	}

	index := len(p.requests) - 1
	if index >= len(p.responses) {
		return nil, fmt.Errorf("unexpected provider call %d", index+1)
	}
	return withTestUsage(p.responses[index]), nil
}

type fakeRegistry struct {
	mu           sync.Mutex
	definitions  []ai.ToolDefinition
	results      map[string]agent.ToolResult
	calls        []ai.ToolCall
	afterExecute func(callCount int)
}

type loopTestRuntime struct {
	provider         ai.Client
	registry         agent.Registry
	factory          *workspacepkg.RunContextFactory
	enableThinking   bool
	MaxParallelTools int
}

func newAgentLoopForTest(
	llmProvider ai.Client,
	registry agent.Registry,
	workDir string,
	enableThinking bool,
) *loopTestRuntime {
	writeValidAgentWorkspace(workDir)
	return newAgentLoopRuntimeForTest(llmProvider, registryWithRequiredRead{Registry: registry}, workDir, enableThinking)
}

func newAgentLoopRuntimeForTest(
	llmProvider ai.Client,
	registry agent.Registry,
	workDir string,
	enableThinking bool,
) *loopTestRuntime {
	return &loopTestRuntime{
		provider:         llmProvider,
		registry:         registry,
		factory:          workspacepkg.NewRunContextFactory(workspacepkg.NewPromptComposer(workDir), workspacepkg.NewSkillLoader(workDir)),
		enableThinking:   enableThinking,
		MaxParallelTools: 4,
	}
}

type registryWithRequiredRead struct {
	agent.Registry
}

type recordingReporter struct {
	mu     sync.Mutex
	events []agent.AgentEvent
}

func (r *recordingReporter) Report(_ context.Context, event agent.AgentEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

func (r *recordingReporter) Events() []agent.AgentEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]agent.AgentEvent(nil), r.events...)
}

func TestLoopReportsEveryLifecycleEventWithoutAggregation(t *testing.T) {
	provider := &fakeProvider{responses: []*ai.Message{
		{Role: ai.RoleAssistant, Content: blocks("plan one")},
		{
			Role:    ai.RoleAssistant,
			Content: blocks("starting tool"),
			ToolCalls: []ai.ToolCall{{
				ID:        "call-1",
				Name:      "read",
				Arguments: json.RawMessage(`{"path":"a.txt"}`),
			}},
		},
		{Role: ai.RoleAssistant, Content: blocks("plan two")},
		{Role: ai.RoleAssistant, Content: blocks("done")},
	}}
	registry := &fakeRegistry{
		definitions: []ai.ToolDefinition{{Name: "read"}},
		results: map[string]agent.ToolResult{
			"read": toolResult(ai.ToolCall{ID: "call-1", Name: "read"}, "file A", false),
		},
	}
	reporter := &recordingReporter{}
	runtime := newAgentLoopForTest(provider, registry, t.TempDir(), true)

	if err := runtime.Run(context.Background(), "read a", reporter); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	call := ai.ToolCall{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"a.txt"}`)}
	result := toolResult(call, "file A", false)
	want := []agent.AgentEvent{
		agent.NewThinkingEvent(),
		agent.NewToolStartEvent(call),
		agent.NewToolEndEvent(call, result),
		agent.NewThinkingEvent(),
		agent.NewMessageEvent(ai.Message{
			Role:    ai.RoleAssistant,
			Content: blocks("done"),
			Usage:   costedUsage(0),
		}),
	}
	if got := reporter.Events(); !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %#v, want %#v", got, want)
	}
}

func (r registryWithRequiredRead) GetAvailableTools() []ai.ToolDefinition {
	definitions := r.Registry.GetAvailableTools()
	for _, definition := range definitions {
		if definition.Name == "read" {
			return definitions
		}
	}
	return append(definitions, ai.ToolDefinition{Name: "read", Description: "read workspace files", ParallelSafe: true})
}

func writeValidAgentWorkspace(workDir string) {
	agentsPath := filepath.Join(workDir, "AGENTS.md")
	if _, err := os.Stat(agentsPath); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(agentsPath, []byte("You are a test Agent."), 0o600); err != nil {
			panic(err)
		}
	} else if err != nil {
		panic(err)
	}
	skillDir := filepath.Join(workDir, "skills", "test-skill")
	if err := os.MkdirAll(skillDir, 0o700); err != nil {
		panic(err)
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")
	if _, err := os.Stat(skillPath); errors.Is(err, os.ErrNotExist) {
		content := "---\nname: test-skill\ndescription: Test workspace behavior\n---\nFollow the test workflow."
		if err := os.WriteFile(skillPath, []byte(content), 0o600); err != nil {
			panic(err)
		}
	} else if err != nil {
		panic(err)
	}
}

func (r *loopTestRuntime) Run(ctx context.Context, prompt string, reporter agent.Reporter) error {
	prepared, err := r.factory.Create(ctx, agent.RunRequest{Input: ai.Message{
		Role:    ai.RoleUser,
		Content: blocks(prompt),
	}}, r.registry.GetAvailableTools())
	if err != nil {
		return err
	}
	runContext := agent.RunContext{
		Messages: prepared.Messages,
		Tools:    prepared.Tools,
		Metadata: prepared.Metadata,
	}
	loop := agent.NewLoop(r.provider, agent.NewScheduler(r.registry, r.MaxParallelTools), r.enableThinking)
	_, err = loop.Run(ctx, runContext, reporter)
	return err
}

func (r *fakeRegistry) GetAvailableTools() []ai.ToolDefinition {
	return append([]ai.ToolDefinition(nil), r.definitions...)
}

func (r *fakeRegistry) Execute(ctx context.Context, call ai.ToolCall, observer agent.ToolEventObserver) (agent.ToolResult, error) {
	r.mu.Lock()
	r.calls = append(r.calls, call)
	callCount := len(r.calls)
	r.mu.Unlock()
	if r.afterExecute != nil {
		r.afterExecute(callCount)
	}
	if observer != nil {
		observer(ctx, agent.NewToolStart(call))
	}
	var result agent.ToolResult
	if result, ok := r.results[call.Name]; ok {
		if observer != nil {
			observer(ctx, agent.NewToolEnd(call, result))
		}
		return result, nil
	}
	result = toolResult(call, "tool is not registered", true)
	if observer != nil {
		observer(ctx, agent.NewToolEnd(call, result))
	}
	return result, nil
}

func (r *fakeRegistry) Calls() []ai.ToolCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]ai.ToolCall(nil), r.calls...)
}

type controlledRegistry struct {
	definitions []ai.ToolDefinition
	started     chan ai.ToolCall
	finished    chan ai.ToolCall
	gates       map[string]chan struct{}
	results     map[string]agent.ToolResult
}

func (r *controlledRegistry) GetAvailableTools() []ai.ToolDefinition {
	return append([]ai.ToolDefinition(nil), r.definitions...)
}

func (r *controlledRegistry) Execute(ctx context.Context, call ai.ToolCall, observer agent.ToolEventObserver) (agent.ToolResult, error) {
	if observer != nil {
		observer(ctx, agent.NewToolStart(call))
	}
	select {
	case r.started <- call:
	case <-ctx.Done():
		result := canceledToolResult(call, ctx.Err())
		if observer != nil {
			observer(ctx, agent.NewToolEnd(call, result))
		}
		return result, ctx.Err()
	}

	gate, exists := r.gates[call.ID]
	if !exists {
		result := toolResult(call, fmt.Sprintf("tool %q is not registered", call.Name), true)
		if observer != nil {
			observer(ctx, agent.NewToolEnd(call, result))
		}
		return result, nil
	}
	select {
	case <-gate:
	case <-ctx.Done():
		result := canceledToolResult(call, ctx.Err())
		if observer != nil {
			observer(ctx, agent.NewToolEnd(call, result))
		}
		return result, ctx.Err()
	}

	r.finished <- call
	var result agent.ToolResult
	if result, ok := r.results[call.ID]; ok {
		if observer != nil {
			observer(ctx, agent.NewToolEnd(call, result))
		}
		return result, nil
	}
	result = toolResult(call, call.Name, false)
	if observer != nil {
		observer(ctx, agent.NewToolEnd(call, result))
	}
	return result, nil
}

func canceledToolResult(call ai.ToolCall, err error) agent.ToolResult {
	return toolResult(call, err.Error(), true)
}

func TestAgentLoopUsesNoThinkingToolsSortedActionToolsAndFinalOnlyMessages(t *testing.T) {
	provider := &fakeProvider{responses: []*ai.Message{
		{Role: ai.RoleAssistant, Content: blocks("plan one")},
		{
			Role:    ai.RoleAssistant,
			Content: blocks("working"),
			ToolCalls: []ai.ToolCall{{
				ID:        "call-1",
				Name:      "zeta",
				Arguments: json.RawMessage(`{}`),
			}},
		},
		{Role: ai.RoleAssistant, Content: blocks("plan two")},
		{Role: ai.RoleAssistant, Content: blocks("done")},
	}}
	registry := &fakeRegistry{
		definitions: []ai.ToolDefinition{{Name: "zeta"}, {Name: "alpha", ParallelSafe: true}},
		results: map[string]agent.ToolResult{
			"zeta": toolResult(ai.ToolCall{ID: "call-1", Name: "zeta"}, "tool result", false),
		},
	}
	reporter := &recordingReporter{}
	loop := agent.NewLoop(provider, agent.NewScheduler(registry, 2), true)
	runContext := agent.RunContext{
		Messages: []ai.Message{
			{Role: ai.RoleSystem, Content: blocks("system")},
			{Role: ai.RoleUser, Content: blocks("do work")},
		},
		Tools: registry.definitions,
	}

	if _, err := loop.Run(context.Background(), runContext, reporter); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	for _, index := range []int{0, 2} {
		if len(provider.availableTools[index]) != 0 {
			t.Fatalf("thinking call %d tools = %#v, want none", index, provider.availableTools[index])
		}
	}
	for _, index := range []int{1, 3} {
		got := provider.availableTools[index]
		if len(got) != 2 || got[0].Name != "alpha" || got[1].Name != "zeta" {
			t.Fatalf("action call %d tools = %#v, want alpha,zeta", index, got)
		}
	}
	secondThinking := provider.requests[2]
	observation := findMessageByToolCallID(secondThinking, "call-1")
	if observation == nil || observation.Role != ai.RoleTool || messageText(t, *observation) != "tool result" {
		t.Fatalf("tool observation = %#v", observation)
	}
	events := reporter.Events()
	messageEvents := make([]agent.AgentEvent, 0)
	for _, event := range events {
		if event.Type == agent.AgentEventMessage {
			messageEvents = append(messageEvents, event)
		}
	}
	if len(messageEvents) != 1 || messageEvents[0].Message == nil || messageText(t, *messageEvents[0].Message) != "done" {
		t.Fatalf("message events = %#v, want final done only", messageEvents)
	}
}

func TestAgentLoopPassesContextAndAvailableToolsToProvider(t *testing.T) {
	provider := &fakeProvider{responses: []*ai.Message{
		{Role: ai.RoleAssistant, Content: blocks("done")},
	}}
	registry := &fakeRegistry{definitions: []ai.ToolDefinition{
		{Name: "bash", Description: "execute a command", InputSchema: map[string]any{"type": "object"}},
	}}
	engine := newAgentLoopForTest(provider, registry, t.TempDir(), false)

	if err := engine.Run(context.Background(), "hello", nil); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(provider.requests) != 1 {
		t.Fatalf("provider calls = %d, want 1", len(provider.requests))
	}
	request := provider.requests[0]
	if len(request) != 2 || request[0].Role != ai.RoleSystem || messageText(t, request[1]) != "hello" {
		t.Fatalf("initial context = %#v", request)
	}
	if len(provider.availableTools[0]) != 2 || provider.availableTools[0][0].Name != "bash" || provider.availableTools[0][1].Name != "read" {
		t.Fatalf("available tools = %#v", provider.availableTools[0])
	}
	if calls := registry.Calls(); len(calls) != 0 {
		t.Fatalf("tool calls = %d, want 0", len(calls))
	}
}

func TestAgentLoopBuildsWorkspaceContextForEachRun(t *testing.T) {
	workDir := t.TempDir()
	agentsPath := filepath.Join(workDir, "AGENTS.md")
	if err := os.WriteFile(agentsPath, []byte("engine-agent-guide-v1"), 0o600); err != nil {
		t.Fatal(err)
	}
	skillDir := filepath.Join(workDir, ".claw", "skills", "engine")
	if err := os.MkdirAll(skillDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(skillDir, "SKILL.md"),
		[]byte("---\nname: engine-skill\ndescription: engine-trigger-v1\n---\nengine-body-secret-v1"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	provider := &fakeProvider{responses: []*ai.Message{
		{Role: ai.RoleAssistant, Content: blocks("done one")},
		{Role: ai.RoleAssistant, Content: blocks("done two")},
	}}
	registry := &fakeRegistry{definitions: []ai.ToolDefinition{{Name: "read", ParallelSafe: true}}}
	agentEngine := newAgentLoopForTest(provider, registry, workDir, false)
	if err := agentEngine.Run(context.Background(), "hello one", nil); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	request := provider.requests[0]
	if len(request) != 2 || request[0].Role != ai.RoleSystem || messageText(t, request[1]) != "hello one" {
		t.Fatalf("initial context = %#v", request)
	}
	for _, want := range []string{
		"engine-agent-guide-v1", "engine-skill", "engine-trigger-v1",
		".claw/skills/engine/SKILL.md", "sha256:", "<available_skills>",
	} {
		if !strings.Contains(messageText(t, request[0]), want) {
			t.Fatalf("system prompt missing %q: %q", want, messageText(t, request[0]))
		}
	}
	if strings.Contains(messageText(t, request[0]), "engine-body-secret-v1") {
		t.Fatalf("system prompt leaked Skill Body: %q", messageText(t, request[0]))
	}

	if err := os.WriteFile(agentsPath, []byte("engine-agent-guide-v2"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(skillDir, "SKILL.md"),
		[]byte("---\nname: engine-skill\ndescription: engine-trigger-v2\n---\nengine-body-secret-v2"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := agentEngine.Run(context.Background(), "hello two", nil); err != nil {
		t.Fatalf("second Run() error = %v", err)
	}

	secondRequest := provider.requests[1]
	if len(secondRequest) != 2 || messageText(t, secondRequest[1]) != "hello two" {
		t.Fatalf("second initial context = %#v", secondRequest)
	}
	if !strings.Contains(messageText(t, secondRequest[0]), "engine-agent-guide-v2") ||
		strings.Contains(messageText(t, secondRequest[0]), "engine-agent-guide-v1") {
		t.Fatalf("second system prompt did not refresh AGENTS.md: %q", messageText(t, secondRequest[0]))
	}
	if !strings.Contains(messageText(t, secondRequest[0]), "engine-trigger-v2") ||
		strings.Contains(messageText(t, secondRequest[0]), "engine-trigger-v1") ||
		strings.Contains(messageText(t, secondRequest[0]), "engine-body-secret-v1") ||
		strings.Contains(messageText(t, secondRequest[0]), "engine-body-secret-v2") {
		t.Fatalf("second system prompt did not refresh catalog safely: %q", messageText(t, secondRequest[0]))
	}
}

func TestAgentLoopRequiresReadWhenSkillsAreAvailable(t *testing.T) {
	workDir := t.TempDir()
	skillDir := filepath.Join(workDir, "skills", "review")
	if err := os.MkdirAll(skillDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"),
		[]byte("---\nname: review\ndescription: Review changes\n---\nBody"), 0o600); err != nil {
		t.Fatal(err)
	}
	provider := &fakeProvider{responses: []*ai.Message{{Role: ai.RoleAssistant, Content: blocks("unused")}}}
	writeValidAgentWorkspace(workDir)
	agentEngine := newAgentLoopRuntimeForTest(provider, &fakeRegistry{}, workDir, false)

	err := agentEngine.Run(context.Background(), "review", nil)
	if err == nil || err.Error() != "agent runtime: required tool read is not registered" {
		t.Fatalf("Run() error = %v", err)
	}
	if len(provider.requests) != 0 {
		t.Fatalf("provider calls = %d, want 0", len(provider.requests))
	}
}

func TestAgentLoopAppendsToolObservationAndContinues(t *testing.T) {
	provider := &fakeProvider{responses: []*ai.Message{
		{
			Role: ai.RoleAssistant,
			ToolCalls: []ai.ToolCall{
				{ID: "call-1", Name: "echo", Arguments: json.RawMessage(`{"text":"hello"}`)},
			},
		},
		{Role: ai.RoleAssistant, Content: blocks("tool finished")},
	}}
	registry := &fakeRegistry{results: map[string]agent.ToolResult{
		"echo": toolResult(ai.ToolCall{ID: "call-1", Name: "echo"}, "hello", false),
	}}
	engine := newAgentLoopForTest(provider, registry, t.TempDir(), false)

	if err := engine.Run(context.Background(), "run echo", nil); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if calls := registry.Calls(); len(calls) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(calls))
	}
	if len(provider.requests) != 2 {
		t.Fatalf("provider calls = %d, want 2", len(provider.requests))
	}

	followUp := provider.requests[1]
	if len(followUp) != 4 {
		t.Fatalf("follow-up message count = %d, want 4", len(followUp))
	}
	observation := followUp[3]
	if observation.Role != ai.RoleTool || observation.ToolCallID != "call-1" || messageText(t, observation) != "hello" {
		t.Fatalf("observation = %#v", observation)
	}
}

func TestAgentLoopExecutesParallelSafeToolsConcurrentlyInCallOrder(t *testing.T) {
	toolCalls := []ai.ToolCall{
		{ID: "call-1", Name: "read-a", Arguments: json.RawMessage(`{}`)},
		{ID: "call-2", Name: "read-b", Arguments: json.RawMessage(`{}`)},
		{ID: "call-3", Name: "read-c", Arguments: json.RawMessage(`{}`)},
	}
	provider := &fakeProvider{responses: []*ai.Message{
		{Role: ai.RoleAssistant, ToolCalls: toolCalls},
		{Role: ai.RoleAssistant, Content: blocks("done")},
	}}
	gates := map[string]chan struct{}{
		"call-1": make(chan struct{}),
		"call-2": make(chan struct{}),
		"call-3": make(chan struct{}),
	}
	registry := &controlledRegistry{
		definitions: []ai.ToolDefinition{
			{Name: "read-a", ParallelSafe: true},
			{Name: "read-b", ParallelSafe: true},
			{Name: "read-c", ParallelSafe: true},
		},
		started:  make(chan ai.ToolCall, len(toolCalls)),
		finished: make(chan ai.ToolCall, len(toolCalls)),
		gates:    gates,
	}
	agentEngine := newAgentLoopForTest(provider, registry, t.TempDir(), false)

	done := make(chan error, 1)
	go func() {
		done <- agentEngine.Run(context.Background(), "read all", nil)
	}()
	runFinished := false
	defer func() {
		closeAllGates(gates)
		if !runFinished {
			<-done
		}
	}()

	started := make(map[string]bool, len(toolCalls))
	for range toolCalls {
		select {
		case call := <-registry.started:
			started[call.ID] = true
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("started calls = %#v, want all calls before releasing a gate", started)
		}
	}

	for _, callID := range []string{"call-3", "call-2", "call-1"} {
		close(gates[callID])
		finished := <-registry.finished
		if finished.ID != callID {
			t.Fatalf("finished call = %q, want %q", finished.ID, callID)
		}
	}

	if err := <-done; err != nil {
		runFinished = true
		t.Fatalf("Run() error = %v", err)
	}
	runFinished = true

	if len(provider.requests) != 2 {
		t.Fatalf("provider calls = %d, want 2", len(provider.requests))
	}
	followUp := provider.requests[1]
	if len(followUp) != 6 {
		t.Fatalf("follow-up message count = %d, want 6", len(followUp))
	}
	for index, wantID := range []string{"call-1", "call-2", "call-3"} {
		observation := followUp[3+index]
		if observation.ToolCallID != wantID {
			t.Fatalf("observation %d ID = %q, want %q", index, observation.ToolCallID, wantID)
		}
	}
}

func TestAgentLoopBoundsParallelTools(t *testing.T) {
	toolCalls := []ai.ToolCall{
		{ID: "call-1", Name: "read-1", Arguments: json.RawMessage(`{}`)},
		{ID: "call-2", Name: "read-2", Arguments: json.RawMessage(`{}`)},
		{ID: "call-3", Name: "read-3", Arguments: json.RawMessage(`{}`)},
		{ID: "call-4", Name: "read-4", Arguments: json.RawMessage(`{}`)},
	}
	definitions := make([]ai.ToolDefinition, 0, len(toolCalls))
	for _, call := range toolCalls {
		definitions = append(definitions, ai.ToolDefinition{Name: call.Name, ParallelSafe: true})
	}
	provider := &fakeProvider{responses: []*ai.Message{
		{Role: ai.RoleAssistant, ToolCalls: toolCalls},
		{Role: ai.RoleAssistant, Content: blocks("done")},
	}}
	registry := newControlledRegistry(toolCalls, definitions)
	agentEngine := newAgentLoopForTest(provider, registry, t.TempDir(), false)
	agentEngine.MaxParallelTools = 2

	done, markFinished := runEngineForTest(t, agentEngine, "read all", registry.gates)
	first := requireStartedCall(t, registry.started)
	second := requireStartedCall(t, registry.started)
	requireNoStartedCall(t, registry.started)

	close(registry.gates[first.ID])
	requireFinishedCall(t, registry.finished, first.ID)
	third := requireStartedCall(t, registry.started)
	if third.ID == first.ID || third.ID == second.ID {
		t.Fatalf("third started call = %q, want a queued call", third.ID)
	}

	closeAllGates(registry.gates)
	if err := <-done; err != nil {
		markFinished()
		t.Fatalf("Run() error = %v", err)
	}
	markFinished()
}

func TestAgentLoopUsesExclusiveToolsAsBarriers(t *testing.T) {
	toolCalls := []ai.ToolCall{
		{ID: "call-1", Name: "read-1", Arguments: json.RawMessage(`{}`)},
		{ID: "call-2", Name: "read-2", Arguments: json.RawMessage(`{}`)},
		{ID: "call-3", Name: "write", Arguments: json.RawMessage(`{}`)},
		{ID: "call-4", Name: "read-3", Arguments: json.RawMessage(`{}`)},
		{ID: "call-5", Name: "missing", Arguments: json.RawMessage(`{}`)},
	}
	definitions := []ai.ToolDefinition{
		{Name: "read-1", ParallelSafe: true},
		{Name: "read-2", ParallelSafe: true},
		{Name: "write"},
		{Name: "read-3", ParallelSafe: true},
	}
	provider := &fakeProvider{responses: []*ai.Message{
		{Role: ai.RoleAssistant, ToolCalls: toolCalls},
		{Role: ai.RoleAssistant, Content: blocks("done")},
	}}
	registry := newControlledRegistry(toolCalls, definitions)
	registry.results["call-5"] = toolResult(ai.ToolCall{ID: "call-5", Name: "missing"}, `tool "missing" is not registered`, true)
	agentEngine := newAgentLoopForTest(provider, registry, t.TempDir(), false)

	done, markFinished := runEngineForTest(t, agentEngine, "run ordered batch", registry.gates)
	first := requireStartedCall(t, registry.started)
	second := requireStartedCall(t, registry.started)
	startedReads := map[string]bool{first.Name: true, second.Name: true}
	if !startedReads["read-1"] || !startedReads["read-2"] {
		t.Fatalf("first phase = %#v, want read-1 and read-2", startedReads)
	}
	requireNoStartedCall(t, registry.started)

	close(registry.gates[first.ID])
	close(registry.gates[second.ID])
	requireFinishedCall(t, registry.finished, "")
	requireFinishedCall(t, registry.finished, "")
	writeCall := requireStartedCall(t, registry.started)
	if writeCall.Name != "write" {
		t.Fatalf("barrier call = %q, want write", writeCall.Name)
	}
	requireNoStartedCall(t, registry.started)

	close(registry.gates[writeCall.ID])
	requireFinishedCall(t, registry.finished, writeCall.ID)
	thirdRead := requireStartedCall(t, registry.started)
	if thirdRead.Name != "read-3" {
		t.Fatalf("third phase call = %q, want read-3", thirdRead.Name)
	}
	requireNoStartedCall(t, registry.started)

	close(registry.gates[thirdRead.ID])
	requireFinishedCall(t, registry.finished, thirdRead.ID)
	missing := requireStartedCall(t, registry.started)
	if missing.Name != "missing" {
		t.Fatalf("last phase call = %q, want missing", missing.Name)
	}
	close(registry.gates[missing.ID])
	requireFinishedCall(t, registry.finished, missing.ID)

	if err := <-done; err != nil {
		markFinished()
		t.Fatalf("Run() error = %v", err)
	}
	markFinished()
	observation := provider.requests[1][7]
	if observation.ToolCallID != "call-5" || !strings.Contains(messageText(t, observation), "not registered") {
		t.Fatalf("unknown observation = %#v", observation)
	}
}

func TestAgentLoopCompletesParallelSiblingsAfterToolError(t *testing.T) {
	toolCalls := []ai.ToolCall{
		{ID: "call-1", Name: "read-1", Arguments: json.RawMessage(`{}`)},
		{ID: "call-2", Name: "read-2", Arguments: json.RawMessage(`{}`)},
	}
	definitions := []ai.ToolDefinition{
		{Name: "read-1", ParallelSafe: true},
		{Name: "read-2", ParallelSafe: true},
	}
	provider := &fakeProvider{responses: []*ai.Message{
		{Role: ai.RoleAssistant, ToolCalls: toolCalls},
		{Role: ai.RoleAssistant, Content: blocks("done")},
	}}
	registry := newControlledRegistry(toolCalls, definitions)
	registry.results["call-1"] = toolResult(ai.ToolCall{ID: "call-1", Name: "read-1"}, "read failed", true)
	registry.results["call-2"] = toolResult(ai.ToolCall{ID: "call-2", Name: "read-2"}, "read ok", false)
	agentEngine := newAgentLoopForTest(provider, registry, t.TempDir(), false)

	done, markFinished := runEngineForTest(t, agentEngine, "read both", registry.gates)
	requireStartedCall(t, registry.started)
	requireStartedCall(t, registry.started)
	closeAllGates(registry.gates)
	if err := <-done; err != nil {
		markFinished()
		t.Fatalf("Run() error = %v", err)
	}
	markFinished()

	followUp := provider.requests[1]
	if followUp[3].ToolCallID != "call-1" || messageText(t, followUp[3]) != "read failed" {
		t.Fatalf("first observation = %#v", followUp[3])
	}
	if followUp[4].ToolCallID != "call-2" || messageText(t, followUp[4]) != "read ok" {
		t.Fatalf("second observation = %#v", followUp[4])
	}
}

func TestAgentLoopUsesSerialFallbackForNonpositiveParallelLimit(t *testing.T) {
	toolCalls := []ai.ToolCall{
		{ID: "call-1", Name: "read-1", Arguments: json.RawMessage(`{}`)},
		{ID: "call-2", Name: "read-2", Arguments: json.RawMessage(`{}`)},
	}
	definitions := []ai.ToolDefinition{
		{Name: "read-1", ParallelSafe: true},
		{Name: "read-2", ParallelSafe: true},
	}
	provider := &fakeProvider{responses: []*ai.Message{
		{Role: ai.RoleAssistant, ToolCalls: toolCalls},
		{Role: ai.RoleAssistant, Content: blocks("done")},
	}}
	registry := newControlledRegistry(toolCalls, definitions)
	agentEngine := newAgentLoopForTest(provider, registry, t.TempDir(), false)
	agentEngine.MaxParallelTools = 0

	done, markFinished := runEngineForTest(t, agentEngine, "read serially", registry.gates)
	first := requireStartedCall(t, registry.started)
	requireNoStartedCall(t, registry.started)
	close(registry.gates[first.ID])
	requireFinishedCall(t, registry.finished, first.ID)
	second := requireStartedCall(t, registry.started)
	if second.ID == first.ID {
		t.Fatalf("second started call = %q, want the other call", second.ID)
	}
	close(registry.gates[second.ID])
	requireFinishedCall(t, registry.finished, second.ID)

	if err := <-done; err != nil {
		markFinished()
		t.Fatalf("Run() error = %v", err)
	}
	markFinished()
}

func closeAllGates(gates map[string]chan struct{}) {
	for _, gate := range gates {
		select {
		case <-gate:
		default:
			close(gate)
		}
	}
}

func newControlledRegistry(
	calls []ai.ToolCall,
	definitions []ai.ToolDefinition,
) *controlledRegistry {
	gates := make(map[string]chan struct{}, len(calls))
	for _, call := range calls {
		gates[call.ID] = make(chan struct{})
	}
	return &controlledRegistry{
		definitions: definitions,
		started:     make(chan ai.ToolCall, len(calls)),
		finished:    make(chan ai.ToolCall, len(calls)),
		gates:       gates,
		results:     make(map[string]agent.ToolResult),
	}
}

func runEngineForTest(
	t *testing.T,
	agentEngine *loopTestRuntime,
	prompt string,
	gates map[string]chan struct{},
) (<-chan error, func()) {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		done <- agentEngine.Run(context.Background(), prompt, nil)
	}()

	finished := false
	markFinished := func() {
		finished = true
	}
	t.Cleanup(func() {
		closeAllGates(gates)
		if finished {
			return
		}
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("Engine did not stop after test gates were released")
		}
	})
	return done, markFinished
}

func requireStartedCall(t *testing.T, started <-chan ai.ToolCall) ai.ToolCall {
	t.Helper()
	select {
	case call := <-started:
		return call
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for tool to start")
		return ai.ToolCall{}
	}
}

func requireNoStartedCall(t *testing.T, started <-chan ai.ToolCall) {
	t.Helper()
	select {
	case call := <-started:
		t.Fatalf("tool %q started before its execution barrier opened", call.ID)
	case <-time.After(75 * time.Millisecond):
	}
}

func requireFinishedCall(t *testing.T, finished <-chan ai.ToolCall, wantID string) {
	t.Helper()
	select {
	case call := <-finished:
		if wantID != "" && call.ID != wantID {
			t.Fatalf("finished call = %q, want %q", call.ID, wantID)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for tool to finish")
	}
}

func TestAgentLoopRejectsInvalidToolCallIDsBeforeExecution(t *testing.T) {
	tests := []struct {
		name      string
		toolCalls []ai.ToolCall
		wantError string
	}{
		{
			name:      "empty ID",
			toolCalls: []ai.ToolCall{{Name: "echo", Arguments: json.RawMessage(`{}`)}},
			wantError: "empty ID",
		},
		{
			name: "duplicate ID",
			toolCalls: []ai.ToolCall{
				{ID: "call-1", Name: "echo", Arguments: json.RawMessage(`{}`)},
				{ID: "call-1", Name: "echo", Arguments: json.RawMessage(`{}`)},
			},
			wantError: "duplicate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &fakeProvider{responses: []*ai.Message{
				{Role: ai.RoleAssistant, ToolCalls: tt.toolCalls},
			}}
			registry := &fakeRegistry{results: map[string]agent.ToolResult{}}
			engine := newAgentLoopForTest(provider, registry, t.TempDir(), false)

			err := engine.Run(context.Background(), "run echo", nil)
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("Run() error = %v, want error containing %q", err, tt.wantError)
			}
			if calls := registry.Calls(); len(calls) != 0 {
				t.Fatalf("tool calls = %d, want 0", len(calls))
			}
		})
	}
}

func TestAgentLoopRejectsNilProviderResponse(t *testing.T) {
	provider := &fakeProvider{responses: []*ai.Message{nil}}
	engine := newAgentLoopForTest(provider, &fakeRegistry{}, t.TempDir(), false)

	err := engine.Run(context.Background(), "hello", nil)
	if err == nil || !strings.Contains(err.Error(), "empty response") {
		t.Fatalf("Run() error = %v, want empty response error", err)
	}
}

func TestAgentLoopWrapsProviderError(t *testing.T) {
	provider := &fakeProvider{err: errors.New("provider unavailable")}
	engine := newAgentLoopForTest(provider, &fakeRegistry{}, t.TempDir(), false)

	err := engine.Run(context.Background(), "hello", nil)
	if err == nil || !strings.Contains(err.Error(), "provider unavailable") {
		t.Fatalf("Run() error = %v, want wrapped provider error", err)
	}
}

func TestAgentLoopDoesNotCallProviderAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	provider := &fakeProvider{responses: []*ai.Message{
		{Role: ai.RoleAssistant, Content: blocks("must not run")},
	}}
	engine := newAgentLoopForTest(provider, &fakeRegistry{}, t.TempDir(), false)

	err := engine.Run(ctx, "hello", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	if len(provider.requests) != 0 {
		t.Fatalf("provider calls = %d, want 0", len(provider.requests))
	}
}

func TestAgentLoopStopsToolBatchAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	provider := &fakeProvider{responses: []*ai.Message{
		{
			Role: ai.RoleAssistant,
			ToolCalls: []ai.ToolCall{
				{ID: "call-1", Name: "first", Arguments: json.RawMessage(`{}`)},
				{ID: "call-2", Name: "second", Arguments: json.RawMessage(`{}`)},
			},
		},
	}}
	registry := &fakeRegistry{
		definitions: []ai.ToolDefinition{
			{Name: "first", ParallelSafe: true},
			{Name: "second", ParallelSafe: true},
		},
		results: map[string]agent.ToolResult{
			"first":  toolResult(ai.ToolCall{ID: "call-1", Name: "first"}, "one", false),
			"second": toolResult(ai.ToolCall{ID: "call-2", Name: "second"}, "two", false),
		},
		afterExecute: func(callCount int) {
			if callCount == 1 {
				cancel()
			}
		},
	}
	agentEngine := newAgentLoopForTest(provider, registry, t.TempDir(), false)
	agentEngine.MaxParallelTools = 1

	err := agentEngine.Run(ctx, "run both", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	if calls := registry.Calls(); len(calls) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(calls))
	}
}

func TestAgentLoopCarriesThinkingIntoActionContext(t *testing.T) {
	provider := &fakeProvider{responses: []*ai.Message{
		{Role: ai.RoleAssistant, Content: blocks("先规划，再行动。")},
		{Role: ai.RoleAssistant, Content: blocks("任务完成")},
	}}
	registry := &fakeRegistry{definitions: []ai.ToolDefinition{
		{Name: "bash", Description: "execute command"},
	}}
	engine := newAgentLoopForTest(provider, registry, t.TempDir(), true)

	if err := engine.Run(context.Background(), "检查目录", nil); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(provider.requests) != 2 {
		t.Fatalf("provider calls = %d, want 2", len(provider.requests))
	}
	if len(provider.availableTools[0]) != 0 {
		t.Fatalf("thinking tools = %#v, want none", provider.availableTools[0])
	}
	if len(provider.availableTools[1]) != 2 || provider.availableTools[1][0].Name != "bash" || provider.availableTools[1][1].Name != "read" {
		t.Fatalf("action tools = %#v, want bash and required read", provider.availableTools[1])
	}
	actionContext := provider.requests[1]
	if len(actionContext) != 4 || actionContext[2].Role != ai.RoleAssistant ||
		messageText(t, actionContext[2]) != "先规划，再行动。" ||
		actionContext[3].Role != ai.RoleUser || !strings.Contains(messageText(t, actionContext[3]), "进入 Action") {
		t.Fatalf("action context = %#v", actionContext)
	}
}

func TestAgentLoopRejectsNilThinkingResponse(t *testing.T) {
	provider := &fakeProvider{responses: []*ai.Message{nil}}
	engine := newAgentLoopForTest(provider, &fakeRegistry{}, t.TempDir(), true)

	err := engine.Run(context.Background(), "hello", nil)
	if err == nil || !strings.Contains(err.Error(), "Thinking") || !strings.Contains(err.Error(), "empty response") {
		t.Fatalf("Run() error = %v, want empty Thinking response error", err)
	}
}

func TestAgentLoopRejectsToolCallsDuringThinking(t *testing.T) {
	provider := &fakeProvider{responses: []*ai.Message{
		{
			Role: ai.RoleAssistant,
			ToolCalls: []ai.ToolCall{
				{ID: "call-1", Name: "bash", Arguments: json.RawMessage(`{}`)},
			},
		},
	}}
	registry := &fakeRegistry{definitions: []ai.ToolDefinition{{Name: "bash"}}}
	engine := newAgentLoopForTest(provider, registry, t.TempDir(), true)

	err := engine.Run(context.Background(), "hello", nil)
	if err == nil || !strings.Contains(err.Error(), "Thinking") || !strings.Contains(err.Error(), "tool calls") {
		t.Fatalf("Run() error = %v, want Thinking tool-call error", err)
	}
	if calls := registry.Calls(); len(calls) != 0 {
		t.Fatalf("tool calls = %d, want 0", len(calls))
	}
}

func TestAgentLoopRejectsInvalidThinkingMessages(t *testing.T) {
	tests := []struct {
		name     string
		response *ai.Message
		want     string
	}{
		{
			name:     "empty plan",
			response: &ai.Message{Role: ai.RoleAssistant},
			want:     "non-empty",
		},
		{
			name:     "wrong role",
			response: &ai.Message{Role: ai.RoleUser, Content: blocks("plan")},
			want:     "assistant role",
		},
		{
			name:     "unexpected tool call ID",
			response: &ai.Message{Role: ai.RoleAssistant, Content: blocks("plan"), ToolCallID: "call-1"},
			want:     "tool_call_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &fakeProvider{responses: []*ai.Message{tt.response}}
			engine := newAgentLoopForTest(provider, &fakeRegistry{}, t.TempDir(), true)

			err := engine.Run(context.Background(), "hello", nil)
			if err == nil || !strings.Contains(err.Error(), "Thinking") || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Run() error = %v, want Thinking error containing %q", err, tt.want)
			}
		})
	}
}

func TestAgentLoopRejectsInvalidActionMessages(t *testing.T) {
	tests := []struct {
		name     string
		response *ai.Message
		want     string
	}{
		{
			name:     "empty response",
			response: &ai.Message{Role: ai.RoleAssistant},
			want:     "no content or tool calls",
		},
		{
			name:     "wrong role",
			response: &ai.Message{Role: ai.RoleUser, Content: blocks("done")},
			want:     "assistant role",
		},
		{
			name:     "unexpected tool call ID",
			response: &ai.Message{Role: ai.RoleAssistant, Content: blocks("done"), ToolCallID: "call-1"},
			want:     "tool_call_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &fakeProvider{responses: []*ai.Message{tt.response}}
			engine := newAgentLoopForTest(provider, &fakeRegistry{}, t.TempDir(), false)

			err := engine.Run(context.Background(), "hello", nil)
			if err == nil || !strings.Contains(err.Error(), "Action") || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Run() error = %v, want Action error containing %q", err, tt.want)
			}
		})
	}
}

func TestAgentLoopRunsThinkingBeforeEveryActionTurn(t *testing.T) {
	provider := &fakeProvider{responses: []*ai.Message{
		{Role: ai.RoleAssistant, Content: blocks("先查看目录。")},
		{
			Role: ai.RoleAssistant,
			ToolCalls: []ai.ToolCall{
				{ID: "call-1", Name: "bash", Arguments: json.RawMessage(`{"command":"ls -la"}`)},
			},
		},
		{Role: ai.RoleAssistant, Content: blocks("已有观察结果，接下来总结。")},
		{Role: ai.RoleAssistant, Content: blocks("完成")},
	}}
	registry := &fakeRegistry{
		definitions: []ai.ToolDefinition{{Name: "bash"}},
		results: map[string]agent.ToolResult{
			"bash": toolResult(ai.ToolCall{ID: "call-1", Name: "bash"}, "main.go", false),
		},
	}
	engine := newAgentLoopForTest(provider, registry, t.TempDir(), true)

	if err := engine.Run(context.Background(), "检查目录", nil); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(provider.requests) != 4 {
		t.Fatalf("provider calls = %d, want 4", len(provider.requests))
	}
	for index, wantToolCount := range []int{0, 2, 0, 2} {
		if got := len(provider.availableTools[index]); got != wantToolCount {
			t.Fatalf("provider call %d tool count = %d, want %d", index+1, got, wantToolCount)
		}
	}
	secondThinkingContext := provider.requests[2]
	lastBeforeThinking := secondThinkingContext[len(secondThinkingContext)-1]
	if lastBeforeThinking.ToolCallID != "call-1" || messageText(t, lastBeforeThinking) != "main.go" {
		t.Fatalf("second thinking observation = %#v", lastBeforeThinking)
	}
	secondActionContext := provider.requests[3]
	observation := findMessageByToolCallID(secondActionContext, "call-1")
	if observation == nil || messageText(t, *observation) != "main.go" {
		t.Fatalf("second action observation = %#v", observation)
	}
	if got := secondActionContext[len(secondActionContext)-2]; got.Role != ai.RoleAssistant ||
		messageText(t, got) != "已有观察结果，接下来总结。" {
		t.Fatalf("second action thinking = %#v", got)
	}
	if got := secondActionContext[len(secondActionContext)-1]; got.Role != ai.RoleUser ||
		!strings.Contains(messageText(t, got), "进入 Action") {
		t.Fatalf("second action transition = %#v", got)
	}
}

func findMessageByToolCallID(messages []ai.Message, toolCallID string) *ai.Message {
	for index := range messages {
		if messages[index].ToolCallID == toolCallID {
			return &messages[index]
		}
	}
	return nil
}

func blocks(text string) []ai.ContentBlock {
	return []ai.ContentBlock{ai.TextBlock(text)}
}

func toolResult(call ai.ToolCall, text string, isError bool) agent.ToolResult {
	return agent.ToolResult{
		ToolCallID: call.ID,
		ToolName:   call.Name,
		Content:    blocks(text),
		IsError:    isError,
	}
}

func messageText(t *testing.T, message ai.Message) string {
	t.Helper()
	text, err := ai.TextContent(message.Content)
	if err != nil {
		t.Fatalf("TextContent() error = %v", err)
	}
	return text
}
