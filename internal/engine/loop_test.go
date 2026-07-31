package engine_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	ctxpkg "github.com/PycMono/go-reagent/internal/context"
	"github.com/PycMono/go-reagent/internal/engine"
	"github.com/PycMono/go-reagent/internal/provider"
	"github.com/PycMono/go-reagent/internal/schema"
	"github.com/PycMono/go-reagent/internal/tools"
)

type fakeProvider struct {
	responses      []*schema.Message
	err            error
	requests       [][]schema.Message
	availableTools [][]schema.ToolDefinition
}

func (p *fakeProvider) Generate(
	_ context.Context,
	messages []schema.Message,
	availableTools []schema.ToolDefinition,
) (*schema.Message, error) {
	p.requests = append(p.requests, append([]schema.Message(nil), messages...))
	p.availableTools = append(p.availableTools, append([]schema.ToolDefinition(nil), availableTools...))
	if p.err != nil {
		return nil, p.err
	}

	index := len(p.requests) - 1
	if index >= len(p.responses) {
		return nil, fmt.Errorf("unexpected provider call %d", index+1)
	}
	return p.responses[index], nil
}

type fakeRegistry struct {
	mu           sync.Mutex
	definitions  []schema.ToolDefinition
	results      map[string]schema.ToolResult
	calls        []schema.ToolCall
	afterExecute func(callCount int)
}

type loopTestRuntime struct {
	provider         provider.LLMProvider
	registry         tools.Registry
	factory          *ctxpkg.RunContextFactory
	enableThinking   bool
	MaxParallelTools int
}

func newAgentLoopForTest(
	llmProvider provider.LLMProvider,
	registry tools.Registry,
	workDir string,
	enableThinking bool,
) *loopTestRuntime {
	return &loopTestRuntime{
		provider:         llmProvider,
		registry:         registry,
		factory:          ctxpkg.NewRunContextFactory(ctxpkg.NewPromptComposer(workDir), ctxpkg.NewSkillLoader(workDir)),
		enableThinking:   enableThinking,
		MaxParallelTools: 4,
	}
}

func (r *loopTestRuntime) Run(ctx context.Context, prompt string, reporter engine.Reporter) error {
	runContext, err := r.factory.Create(ctx, prompt, r.registry.GetAvailableTools())
	if err != nil {
		return err
	}
	loop := engine.NewAgentLoop(r.provider, engine.NewToolScheduler(r.registry, r.MaxParallelTools), r.enableThinking)
	return loop.Run(ctx, runContext, reporter)
}

func (r *fakeRegistry) GetAvailableTools() []schema.ToolDefinition {
	return append([]schema.ToolDefinition(nil), r.definitions...)
}

func (r *fakeRegistry) Execute(ctx context.Context, call schema.ToolCall, observer tools.ToolEventObserver) (schema.ToolResult, error) {
	r.mu.Lock()
	r.calls = append(r.calls, call)
	callCount := len(r.calls)
	r.mu.Unlock()
	if r.afterExecute != nil {
		r.afterExecute(callCount)
	}
	if observer != nil {
		observer(ctx, schema.NewToolStart(call))
	}
	var result schema.ToolResult
	if result, ok := r.results[call.Name]; ok {
		if observer != nil {
			observer(ctx, schema.NewToolEnd(call, result))
		}
		return result, nil
	}
	result = toolResult(call, "tool is not registered", true)
	if observer != nil {
		observer(ctx, schema.NewToolEnd(call, result))
	}
	return result, nil
}

func (r *fakeRegistry) Calls() []schema.ToolCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]schema.ToolCall(nil), r.calls...)
}

type controlledRegistry struct {
	definitions []schema.ToolDefinition
	started     chan schema.ToolCall
	finished    chan schema.ToolCall
	gates       map[string]chan struct{}
	results     map[string]schema.ToolResult
}

func (r *controlledRegistry) GetAvailableTools() []schema.ToolDefinition {
	return append([]schema.ToolDefinition(nil), r.definitions...)
}

func (r *controlledRegistry) Execute(ctx context.Context, call schema.ToolCall, observer tools.ToolEventObserver) (schema.ToolResult, error) {
	if observer != nil {
		observer(ctx, schema.NewToolStart(call))
	}
	select {
	case r.started <- call:
	case <-ctx.Done():
		result := canceledToolResult(call, ctx.Err())
		if observer != nil {
			observer(ctx, schema.NewToolEnd(call, result))
		}
		return result, ctx.Err()
	}

	gate, exists := r.gates[call.ID]
	if !exists {
		result := toolResult(call, fmt.Sprintf("tool %q is not registered", call.Name), true)
		if observer != nil {
			observer(ctx, schema.NewToolEnd(call, result))
		}
		return result, nil
	}
	select {
	case <-gate:
	case <-ctx.Done():
		result := canceledToolResult(call, ctx.Err())
		if observer != nil {
			observer(ctx, schema.NewToolEnd(call, result))
		}
		return result, ctx.Err()
	}

	r.finished <- call
	var result schema.ToolResult
	if result, ok := r.results[call.ID]; ok {
		if observer != nil {
			observer(ctx, schema.NewToolEnd(call, result))
		}
		return result, nil
	}
	result = toolResult(call, call.Name, false)
	if observer != nil {
		observer(ctx, schema.NewToolEnd(call, result))
	}
	return result, nil
}

func canceledToolResult(call schema.ToolCall, err error) schema.ToolResult {
	return toolResult(call, err.Error(), true)
}

func TestAgentLoopUsesNoThinkingToolsSortedActionToolsAndFinalOnlyMessages(t *testing.T) {
	provider := &fakeProvider{responses: []*schema.Message{
		{Role: schema.RoleAssistant, Content: blocks("plan one")},
		{
			Role:    schema.RoleAssistant,
			Content: blocks("working"),
			ToolCalls: []schema.ToolCall{{
				ID:        "call-1",
				Name:      "zeta",
				Arguments: json.RawMessage(`{}`),
			}},
		},
		{Role: schema.RoleAssistant, Content: blocks("plan two")},
		{Role: schema.RoleAssistant, Content: blocks("done")},
	}}
	registry := &fakeRegistry{
		definitions: []schema.ToolDefinition{{Name: "zeta"}, {Name: "alpha", ParallelSafe: true}},
		results: map[string]schema.ToolResult{
			"zeta": toolResult(schema.ToolCall{ID: "call-1", Name: "zeta"}, "tool result", false),
		},
	}
	reporter := &recordingReporter{}
	loop := engine.NewAgentLoop(provider, engine.NewToolScheduler(registry, 2), true)
	runContext := ctxpkg.RunContext{
		Messages: []schema.Message{
			{Role: schema.RoleSystem, Content: blocks("system")},
			{Role: schema.RoleUser, Content: blocks("do work")},
		},
		Tools: registry.definitions,
	}

	if err := loop.Run(context.Background(), runContext, reporter); err != nil {
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
	if observation == nil || observation.Role != schema.RoleTool || messageText(t, *observation) != "tool result" {
		t.Fatalf("tool observation = %#v", observation)
	}
	events := reporter.Events()
	messageEvents := make([]schema.AgentEvent, 0)
	for _, event := range events {
		if event.Type == schema.AgentEventMessage {
			messageEvents = append(messageEvents, event)
		}
	}
	if len(messageEvents) != 1 || messageEvents[0].Message == nil || messageText(t, *messageEvents[0].Message) != "done" {
		t.Fatalf("message events = %#v, want final done only", messageEvents)
	}
}

func TestAgentLoopPassesContextAndAvailableToolsToProvider(t *testing.T) {
	provider := &fakeProvider{responses: []*schema.Message{
		{Role: schema.RoleAssistant, Content: blocks("done")},
	}}
	registry := &fakeRegistry{definitions: []schema.ToolDefinition{
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
	if len(request) != 2 || request[0].Role != schema.RoleSystem || messageText(t, request[1]) != "hello" {
		t.Fatalf("initial context = %#v", request)
	}
	if len(provider.availableTools[0]) != 1 || provider.availableTools[0][0].Name != "bash" {
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

	provider := &fakeProvider{responses: []*schema.Message{
		{Role: schema.RoleAssistant, Content: blocks("done one")},
		{Role: schema.RoleAssistant, Content: blocks("done two")},
	}}
	registry := &fakeRegistry{definitions: []schema.ToolDefinition{{Name: "read", ParallelSafe: true}}}
	agentEngine := newAgentLoopForTest(provider, registry, workDir, false)
	if err := agentEngine.Run(context.Background(), "hello one", nil); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	request := provider.requests[0]
	if len(request) != 2 || request[0].Role != schema.RoleSystem || messageText(t, request[1]) != "hello one" {
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
	provider := &fakeProvider{responses: []*schema.Message{{Role: schema.RoleAssistant, Content: blocks("unused")}}}
	agentEngine := newAgentLoopForTest(provider, &fakeRegistry{}, workDir, false)

	err := agentEngine.Run(context.Background(), "review", nil)
	if err == nil || !strings.Contains(err.Error(), "Registry 未挂载 read") || strings.Contains(err.Error(), "read_file") {
		t.Fatalf("Run() error = %v", err)
	}
	if len(provider.requests) != 0 {
		t.Fatalf("provider calls = %d, want 0", len(provider.requests))
	}
}

func TestAgentLoopAppendsToolObservationAndContinues(t *testing.T) {
	provider := &fakeProvider{responses: []*schema.Message{
		{
			Role: schema.RoleAssistant,
			ToolCalls: []schema.ToolCall{
				{ID: "call-1", Name: "echo", Arguments: json.RawMessage(`{"text":"hello"}`)},
			},
		},
		{Role: schema.RoleAssistant, Content: blocks("tool finished")},
	}}
	registry := &fakeRegistry{results: map[string]schema.ToolResult{
		"echo": toolResult(schema.ToolCall{ID: "call-1", Name: "echo"}, "hello", false),
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
	if observation.Role != schema.RoleTool || observation.ToolCallID != "call-1" || messageText(t, observation) != "hello" {
		t.Fatalf("observation = %#v", observation)
	}
}

func TestAgentLoopExecutesParallelSafeToolsConcurrentlyInCallOrder(t *testing.T) {
	toolCalls := []schema.ToolCall{
		{ID: "call-1", Name: "read-a", Arguments: json.RawMessage(`{}`)},
		{ID: "call-2", Name: "read-b", Arguments: json.RawMessage(`{}`)},
		{ID: "call-3", Name: "read-c", Arguments: json.RawMessage(`{}`)},
	}
	provider := &fakeProvider{responses: []*schema.Message{
		{Role: schema.RoleAssistant, ToolCalls: toolCalls},
		{Role: schema.RoleAssistant, Content: blocks("done")},
	}}
	gates := map[string]chan struct{}{
		"call-1": make(chan struct{}),
		"call-2": make(chan struct{}),
		"call-3": make(chan struct{}),
	}
	registry := &controlledRegistry{
		definitions: []schema.ToolDefinition{
			{Name: "read-a", ParallelSafe: true},
			{Name: "read-b", ParallelSafe: true},
			{Name: "read-c", ParallelSafe: true},
		},
		started:  make(chan schema.ToolCall, len(toolCalls)),
		finished: make(chan schema.ToolCall, len(toolCalls)),
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
	toolCalls := []schema.ToolCall{
		{ID: "call-1", Name: "read-1", Arguments: json.RawMessage(`{}`)},
		{ID: "call-2", Name: "read-2", Arguments: json.RawMessage(`{}`)},
		{ID: "call-3", Name: "read-3", Arguments: json.RawMessage(`{}`)},
		{ID: "call-4", Name: "read-4", Arguments: json.RawMessage(`{}`)},
	}
	definitions := make([]schema.ToolDefinition, 0, len(toolCalls))
	for _, call := range toolCalls {
		definitions = append(definitions, schema.ToolDefinition{Name: call.Name, ParallelSafe: true})
	}
	provider := &fakeProvider{responses: []*schema.Message{
		{Role: schema.RoleAssistant, ToolCalls: toolCalls},
		{Role: schema.RoleAssistant, Content: blocks("done")},
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
	toolCalls := []schema.ToolCall{
		{ID: "call-1", Name: "read-1", Arguments: json.RawMessage(`{}`)},
		{ID: "call-2", Name: "read-2", Arguments: json.RawMessage(`{}`)},
		{ID: "call-3", Name: "write", Arguments: json.RawMessage(`{}`)},
		{ID: "call-4", Name: "read-3", Arguments: json.RawMessage(`{}`)},
		{ID: "call-5", Name: "missing", Arguments: json.RawMessage(`{}`)},
	}
	definitions := []schema.ToolDefinition{
		{Name: "read-1", ParallelSafe: true},
		{Name: "read-2", ParallelSafe: true},
		{Name: "write"},
		{Name: "read-3", ParallelSafe: true},
	}
	provider := &fakeProvider{responses: []*schema.Message{
		{Role: schema.RoleAssistant, ToolCalls: toolCalls},
		{Role: schema.RoleAssistant, Content: blocks("done")},
	}}
	registry := newControlledRegistry(toolCalls, definitions)
	registry.results["call-5"] = toolResult(schema.ToolCall{ID: "call-5", Name: "missing"}, `tool "missing" is not registered`, true)
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
	toolCalls := []schema.ToolCall{
		{ID: "call-1", Name: "read-1", Arguments: json.RawMessage(`{}`)},
		{ID: "call-2", Name: "read-2", Arguments: json.RawMessage(`{}`)},
	}
	definitions := []schema.ToolDefinition{
		{Name: "read-1", ParallelSafe: true},
		{Name: "read-2", ParallelSafe: true},
	}
	provider := &fakeProvider{responses: []*schema.Message{
		{Role: schema.RoleAssistant, ToolCalls: toolCalls},
		{Role: schema.RoleAssistant, Content: blocks("done")},
	}}
	registry := newControlledRegistry(toolCalls, definitions)
	registry.results["call-1"] = toolResult(schema.ToolCall{ID: "call-1", Name: "read-1"}, "read failed", true)
	registry.results["call-2"] = toolResult(schema.ToolCall{ID: "call-2", Name: "read-2"}, "read ok", false)
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
	toolCalls := []schema.ToolCall{
		{ID: "call-1", Name: "read-1", Arguments: json.RawMessage(`{}`)},
		{ID: "call-2", Name: "read-2", Arguments: json.RawMessage(`{}`)},
	}
	definitions := []schema.ToolDefinition{
		{Name: "read-1", ParallelSafe: true},
		{Name: "read-2", ParallelSafe: true},
	}
	provider := &fakeProvider{responses: []*schema.Message{
		{Role: schema.RoleAssistant, ToolCalls: toolCalls},
		{Role: schema.RoleAssistant, Content: blocks("done")},
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
	calls []schema.ToolCall,
	definitions []schema.ToolDefinition,
) *controlledRegistry {
	gates := make(map[string]chan struct{}, len(calls))
	for _, call := range calls {
		gates[call.ID] = make(chan struct{})
	}
	return &controlledRegistry{
		definitions: definitions,
		started:     make(chan schema.ToolCall, len(calls)),
		finished:    make(chan schema.ToolCall, len(calls)),
		gates:       gates,
		results:     make(map[string]schema.ToolResult),
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

func requireStartedCall(t *testing.T, started <-chan schema.ToolCall) schema.ToolCall {
	t.Helper()
	select {
	case call := <-started:
		return call
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for tool to start")
		return schema.ToolCall{}
	}
}

func requireNoStartedCall(t *testing.T, started <-chan schema.ToolCall) {
	t.Helper()
	select {
	case call := <-started:
		t.Fatalf("tool %q started before its execution barrier opened", call.ID)
	case <-time.After(75 * time.Millisecond):
	}
}

func requireFinishedCall(t *testing.T, finished <-chan schema.ToolCall, wantID string) {
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
		toolCalls []schema.ToolCall
		wantError string
	}{
		{
			name:      "empty ID",
			toolCalls: []schema.ToolCall{{Name: "echo", Arguments: json.RawMessage(`{}`)}},
			wantError: "empty ID",
		},
		{
			name: "duplicate ID",
			toolCalls: []schema.ToolCall{
				{ID: "call-1", Name: "echo", Arguments: json.RawMessage(`{}`)},
				{ID: "call-1", Name: "echo", Arguments: json.RawMessage(`{}`)},
			},
			wantError: "duplicate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &fakeProvider{responses: []*schema.Message{
				{Role: schema.RoleAssistant, ToolCalls: tt.toolCalls},
			}}
			registry := &fakeRegistry{results: map[string]schema.ToolResult{}}
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
	provider := &fakeProvider{responses: []*schema.Message{nil}}
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

	provider := &fakeProvider{responses: []*schema.Message{
		{Role: schema.RoleAssistant, Content: blocks("must not run")},
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
	provider := &fakeProvider{responses: []*schema.Message{
		{
			Role: schema.RoleAssistant,
			ToolCalls: []schema.ToolCall{
				{ID: "call-1", Name: "first", Arguments: json.RawMessage(`{}`)},
				{ID: "call-2", Name: "second", Arguments: json.RawMessage(`{}`)},
			},
		},
	}}
	registry := &fakeRegistry{
		definitions: []schema.ToolDefinition{
			{Name: "first", ParallelSafe: true},
			{Name: "second", ParallelSafe: true},
		},
		results: map[string]schema.ToolResult{
			"first":  toolResult(schema.ToolCall{ID: "call-1", Name: "first"}, "one", false),
			"second": toolResult(schema.ToolCall{ID: "call-2", Name: "second"}, "two", false),
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
	provider := &fakeProvider{responses: []*schema.Message{
		{Role: schema.RoleAssistant, Content: blocks("先规划，再行动。")},
		{Role: schema.RoleAssistant, Content: blocks("任务完成")},
	}}
	registry := &fakeRegistry{definitions: []schema.ToolDefinition{
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
	if len(provider.availableTools[1]) != 1 || provider.availableTools[1][0].Name != "bash" {
		t.Fatalf("action tools = %#v, want bash", provider.availableTools[1])
	}
	actionContext := provider.requests[1]
	if len(actionContext) != 4 || actionContext[2].Role != schema.RoleAssistant ||
		messageText(t, actionContext[2]) != "先规划，再行动。" ||
		actionContext[3].Role != schema.RoleUser || !strings.Contains(messageText(t, actionContext[3]), "进入 Action") {
		t.Fatalf("action context = %#v", actionContext)
	}
}

func TestAgentLoopRejectsNilThinkingResponse(t *testing.T) {
	provider := &fakeProvider{responses: []*schema.Message{nil}}
	engine := newAgentLoopForTest(provider, &fakeRegistry{}, t.TempDir(), true)

	err := engine.Run(context.Background(), "hello", nil)
	if err == nil || !strings.Contains(err.Error(), "Thinking") || !strings.Contains(err.Error(), "empty response") {
		t.Fatalf("Run() error = %v, want empty Thinking response error", err)
	}
}

func TestAgentLoopRejectsToolCallsDuringThinking(t *testing.T) {
	provider := &fakeProvider{responses: []*schema.Message{
		{
			Role: schema.RoleAssistant,
			ToolCalls: []schema.ToolCall{
				{ID: "call-1", Name: "bash", Arguments: json.RawMessage(`{}`)},
			},
		},
	}}
	registry := &fakeRegistry{definitions: []schema.ToolDefinition{{Name: "bash"}}}
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
		response *schema.Message
		want     string
	}{
		{
			name:     "empty plan",
			response: &schema.Message{Role: schema.RoleAssistant},
			want:     "non-empty",
		},
		{
			name:     "wrong role",
			response: &schema.Message{Role: schema.RoleUser, Content: blocks("plan")},
			want:     "assistant role",
		},
		{
			name:     "unexpected tool call ID",
			response: &schema.Message{Role: schema.RoleAssistant, Content: blocks("plan"), ToolCallID: "call-1"},
			want:     "tool_call_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &fakeProvider{responses: []*schema.Message{tt.response}}
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
		response *schema.Message
		want     string
	}{
		{
			name:     "empty response",
			response: &schema.Message{Role: schema.RoleAssistant},
			want:     "no content or tool calls",
		},
		{
			name:     "wrong role",
			response: &schema.Message{Role: schema.RoleUser, Content: blocks("done")},
			want:     "assistant role",
		},
		{
			name:     "unexpected tool call ID",
			response: &schema.Message{Role: schema.RoleAssistant, Content: blocks("done"), ToolCallID: "call-1"},
			want:     "tool_call_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &fakeProvider{responses: []*schema.Message{tt.response}}
			engine := newAgentLoopForTest(provider, &fakeRegistry{}, t.TempDir(), false)

			err := engine.Run(context.Background(), "hello", nil)
			if err == nil || !strings.Contains(err.Error(), "Action") || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Run() error = %v, want Action error containing %q", err, tt.want)
			}
		})
	}
}

func TestAgentLoopRunsThinkingBeforeEveryActionTurn(t *testing.T) {
	provider := &fakeProvider{responses: []*schema.Message{
		{Role: schema.RoleAssistant, Content: blocks("先查看目录。")},
		{
			Role: schema.RoleAssistant,
			ToolCalls: []schema.ToolCall{
				{ID: "call-1", Name: "bash", Arguments: json.RawMessage(`{"command":"ls -la"}`)},
			},
		},
		{Role: schema.RoleAssistant, Content: blocks("已有观察结果，接下来总结。")},
		{Role: schema.RoleAssistant, Content: blocks("完成")},
	}}
	registry := &fakeRegistry{
		definitions: []schema.ToolDefinition{{Name: "bash"}},
		results: map[string]schema.ToolResult{
			"bash": toolResult(schema.ToolCall{ID: "call-1", Name: "bash"}, "main.go", false),
		},
	}
	engine := newAgentLoopForTest(provider, registry, t.TempDir(), true)

	if err := engine.Run(context.Background(), "检查目录", nil); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(provider.requests) != 4 {
		t.Fatalf("provider calls = %d, want 4", len(provider.requests))
	}
	for index, wantToolCount := range []int{0, 1, 0, 1} {
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
	if got := secondActionContext[len(secondActionContext)-2]; got.Role != schema.RoleAssistant ||
		messageText(t, got) != "已有观察结果，接下来总结。" {
		t.Fatalf("second action thinking = %#v", got)
	}
	if got := secondActionContext[len(secondActionContext)-1]; got.Role != schema.RoleUser ||
		!strings.Contains(messageText(t, got), "进入 Action") {
		t.Fatalf("second action transition = %#v", got)
	}
}

func findMessageByToolCallID(messages []schema.Message, toolCallID string) *schema.Message {
	for index := range messages {
		if messages[index].ToolCallID == toolCallID {
			return &messages[index]
		}
	}
	return nil
}

func blocks(text string) []schema.ContentBlock {
	return []schema.ContentBlock{schema.TextBlock(text)}
}

func toolResult(call schema.ToolCall, text string, isError bool) schema.ToolResult {
	return schema.ToolResult{
		ToolCallID: call.ID,
		ToolName:   call.Name,
		Content:    blocks(text),
		IsError:    isError,
	}
}

func messageText(t *testing.T, message schema.Message) string {
	t.Helper()
	text, err := schema.TextContent(message.Content)
	if err != nil {
		t.Fatalf("TextContent() error = %v", err)
	}
	return text
}
