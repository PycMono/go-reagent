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

	"github.com/PycMono/go-reagent/internal/engine"
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

func (r *fakeRegistry) GetAvailableTools() []schema.ToolDefinition {
	return append([]schema.ToolDefinition(nil), r.definitions...)
}

func (r *fakeRegistry) Execute(_ context.Context, call schema.ToolCall) schema.ToolResult {
	r.mu.Lock()
	r.calls = append(r.calls, call)
	callCount := len(r.calls)
	r.mu.Unlock()
	if r.afterExecute != nil {
		r.afterExecute(callCount)
	}
	if result, ok := r.results[call.Name]; ok {
		return result
	}
	return schema.ToolResult{ToolCallID: call.ID, Output: "tool is not registered", IsError: true}
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

func (r *controlledRegistry) Execute(ctx context.Context, call schema.ToolCall) schema.ToolResult {
	select {
	case r.started <- call:
	case <-ctx.Done():
		return canceledToolResult(call, ctx.Err())
	}

	gate, exists := r.gates[call.ID]
	if !exists {
		return schema.ToolResult{
			ToolCallID: call.ID,
			Output:     fmt.Sprintf("tool %q is not registered", call.Name),
			IsError:    true,
		}
	}
	select {
	case <-gate:
	case <-ctx.Done():
		return canceledToolResult(call, ctx.Err())
	}

	r.finished <- call
	if result, ok := r.results[call.ID]; ok {
		return result
	}
	return schema.ToolResult{ToolCallID: call.ID, Output: call.Name}
}

func canceledToolResult(call schema.ToolCall, err error) schema.ToolResult {
	return schema.ToolResult{ToolCallID: call.ID, Output: err.Error(), IsError: true}
}

func TestAgentEnginePassesContextAndAvailableToolsToProvider(t *testing.T) {
	provider := &fakeProvider{responses: []*schema.Message{
		{Role: schema.RoleAssistant, Content: "done"},
	}}
	registry := &fakeRegistry{definitions: []schema.ToolDefinition{
		{Name: "bash", Description: "execute a command", InputSchema: map[string]any{"type": "object"}},
	}}
	engine := engine.NewAgentEngine(provider, registry, t.TempDir(), false)

	if err := engine.Run(context.Background(), "hello", nil); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(provider.requests) != 1 {
		t.Fatalf("provider calls = %d, want 1", len(provider.requests))
	}
	request := provider.requests[0]
	if len(request) != 2 || request[0].Role != schema.RoleSystem || request[1].Content != "hello" {
		t.Fatalf("initial context = %#v", request)
	}
	if len(provider.availableTools[0]) != 1 || provider.availableTools[0][0].Name != "bash" {
		t.Fatalf("available tools = %#v", provider.availableTools[0])
	}
	if calls := registry.Calls(); len(calls) != 0 {
		t.Fatalf("tool calls = %d, want 0", len(calls))
	}
}

func TestAgentEngineBuildsWorkspaceContextForEachRun(t *testing.T) {
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
		{Role: schema.RoleAssistant, Content: "done one"},
		{Role: schema.RoleAssistant, Content: "done two"},
	}}
	registry := &fakeRegistry{definitions: []schema.ToolDefinition{{Name: "read_file", ParallelSafe: true}}}
	agentEngine := engine.NewAgentEngine(provider, registry, workDir, false)
	if err := agentEngine.Run(context.Background(), "hello one", nil); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	request := provider.requests[0]
	if len(request) != 2 || request[0].Role != schema.RoleSystem || request[1].Content != "hello one" {
		t.Fatalf("initial context = %#v", request)
	}
	for _, want := range []string{
		"engine-agent-guide-v1", "engine-skill", "engine-trigger-v1",
		".claw/skills/engine/SKILL.md", "sha256:", "<available_skills>",
	} {
		if !strings.Contains(request[0].Content, want) {
			t.Fatalf("system prompt missing %q: %q", want, request[0].Content)
		}
	}
	if strings.Contains(request[0].Content, "engine-body-secret-v1") {
		t.Fatalf("system prompt leaked Skill Body: %q", request[0].Content)
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
	if len(secondRequest) != 2 || secondRequest[1].Content != "hello two" {
		t.Fatalf("second initial context = %#v", secondRequest)
	}
	if !strings.Contains(secondRequest[0].Content, "engine-agent-guide-v2") ||
		strings.Contains(secondRequest[0].Content, "engine-agent-guide-v1") {
		t.Fatalf("second system prompt did not refresh AGENTS.md: %q", secondRequest[0].Content)
	}
	if !strings.Contains(secondRequest[0].Content, "engine-trigger-v2") ||
		strings.Contains(secondRequest[0].Content, "engine-trigger-v1") ||
		strings.Contains(secondRequest[0].Content, "engine-body-secret-v1") ||
		strings.Contains(secondRequest[0].Content, "engine-body-secret-v2") {
		t.Fatalf("second system prompt did not refresh catalog safely: %q", secondRequest[0].Content)
	}
}

func TestAgentEngineRequiresReadFileWhenSkillsAreAvailable(t *testing.T) {
	workDir := t.TempDir()
	skillDir := filepath.Join(workDir, "skills", "review")
	if err := os.MkdirAll(skillDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"),
		[]byte("---\nname: review\ndescription: Review changes\n---\nBody"), 0o600); err != nil {
		t.Fatal(err)
	}
	provider := &fakeProvider{responses: []*schema.Message{{Role: schema.RoleAssistant, Content: "unused"}}}
	agentEngine := engine.NewAgentEngine(provider, &fakeRegistry{}, workDir, false)

	err := agentEngine.Run(context.Background(), "review", nil)
	if err == nil || !strings.Contains(err.Error(), "read_file") {
		t.Fatalf("Run() error = %v", err)
	}
	if len(provider.requests) != 0 {
		t.Fatalf("provider calls = %d, want 0", len(provider.requests))
	}
}

func TestAgentEngineAppendsToolObservationAndContinues(t *testing.T) {
	provider := &fakeProvider{responses: []*schema.Message{
		{
			Role: schema.RoleAssistant,
			ToolCalls: []schema.ToolCall{
				{ID: "call-1", Name: "echo", Arguments: json.RawMessage(`{"text":"hello"}`)},
			},
		},
		{Role: schema.RoleAssistant, Content: "tool finished"},
	}}
	registry := &fakeRegistry{results: map[string]schema.ToolResult{
		"echo": {ToolCallID: "call-1", Output: "hello"},
	}}
	engine := engine.NewAgentEngine(provider, registry, t.TempDir(), false)

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
	if observation.Role != schema.RoleUser || observation.ToolCallID != "call-1" || observation.Content != "hello" {
		t.Fatalf("observation = %#v", observation)
	}
}

func TestAgentEngineExecutesParallelSafeToolsConcurrentlyInCallOrder(t *testing.T) {
	toolCalls := []schema.ToolCall{
		{ID: "call-1", Name: "read-a", Arguments: json.RawMessage(`{}`)},
		{ID: "call-2", Name: "read-b", Arguments: json.RawMessage(`{}`)},
		{ID: "call-3", Name: "read-c", Arguments: json.RawMessage(`{}`)},
	}
	provider := &fakeProvider{responses: []*schema.Message{
		{Role: schema.RoleAssistant, ToolCalls: toolCalls},
		{Role: schema.RoleAssistant, Content: "done"},
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
	agentEngine := engine.NewAgentEngine(provider, registry, t.TempDir(), false)

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

func TestAgentEngineBoundsParallelTools(t *testing.T) {
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
		{Role: schema.RoleAssistant, Content: "done"},
	}}
	registry := newControlledRegistry(toolCalls, definitions)
	agentEngine := engine.NewAgentEngine(provider, registry, t.TempDir(), false)
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

func TestAgentEngineUsesExclusiveToolsAsBarriers(t *testing.T) {
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
		{Role: schema.RoleAssistant, Content: "done"},
	}}
	registry := newControlledRegistry(toolCalls, definitions)
	registry.results["call-5"] = schema.ToolResult{
		ToolCallID: "call-5",
		Output:     `tool "missing" is not registered`,
		IsError:    true,
	}
	agentEngine := engine.NewAgentEngine(provider, registry, t.TempDir(), false)

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
	if observation.ToolCallID != "call-5" || !strings.Contains(observation.Content, "not registered") {
		t.Fatalf("unknown observation = %#v", observation)
	}
}

func TestAgentEngineCompletesParallelSiblingsAfterToolError(t *testing.T) {
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
		{Role: schema.RoleAssistant, Content: "done"},
	}}
	registry := newControlledRegistry(toolCalls, definitions)
	registry.results["call-1"] = schema.ToolResult{ToolCallID: "call-1", Output: "read failed", IsError: true}
	registry.results["call-2"] = schema.ToolResult{ToolCallID: "call-2", Output: "read ok"}
	agentEngine := engine.NewAgentEngine(provider, registry, t.TempDir(), false)

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
	if followUp[3].ToolCallID != "call-1" || followUp[3].Content != "read failed" {
		t.Fatalf("first observation = %#v", followUp[3])
	}
	if followUp[4].ToolCallID != "call-2" || followUp[4].Content != "read ok" {
		t.Fatalf("second observation = %#v", followUp[4])
	}
}

func TestAgentEngineUsesSerialFallbackForNonpositiveParallelLimit(t *testing.T) {
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
		{Role: schema.RoleAssistant, Content: "done"},
	}}
	registry := newControlledRegistry(toolCalls, definitions)
	agentEngine := engine.NewAgentEngine(provider, registry, t.TempDir(), false)
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
	agentEngine *engine.AgentEngine,
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

func TestAgentEngineRejectsInvalidToolCallIDsBeforeExecution(t *testing.T) {
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
			engine := engine.NewAgentEngine(provider, registry, t.TempDir(), false)

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

func TestAgentEngineRejectsNilProviderResponse(t *testing.T) {
	provider := &fakeProvider{responses: []*schema.Message{nil}}
	engine := engine.NewAgentEngine(provider, &fakeRegistry{}, t.TempDir(), false)

	err := engine.Run(context.Background(), "hello", nil)
	if err == nil || !strings.Contains(err.Error(), "empty response") {
		t.Fatalf("Run() error = %v, want empty response error", err)
	}
}

func TestAgentEngineWrapsProviderError(t *testing.T) {
	provider := &fakeProvider{err: errors.New("provider unavailable")}
	engine := engine.NewAgentEngine(provider, &fakeRegistry{}, t.TempDir(), false)

	err := engine.Run(context.Background(), "hello", nil)
	if err == nil || !strings.Contains(err.Error(), "provider unavailable") {
		t.Fatalf("Run() error = %v, want wrapped provider error", err)
	}
}

func TestAgentEngineDoesNotCallProviderAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	provider := &fakeProvider{responses: []*schema.Message{
		{Role: schema.RoleAssistant, Content: "must not run"},
	}}
	engine := engine.NewAgentEngine(provider, &fakeRegistry{}, t.TempDir(), false)

	err := engine.Run(ctx, "hello", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	if len(provider.requests) != 0 {
		t.Fatalf("provider calls = %d, want 0", len(provider.requests))
	}
}

func TestAgentEngineStopsToolBatchAfterCancellation(t *testing.T) {
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
			"first":  {ToolCallID: "call-1", Output: "one"},
			"second": {ToolCallID: "call-2", Output: "two"},
		},
		afterExecute: func(callCount int) {
			if callCount == 1 {
				cancel()
			}
		},
	}
	agentEngine := engine.NewAgentEngine(provider, registry, t.TempDir(), false)
	agentEngine.MaxParallelTools = 1

	err := agentEngine.Run(ctx, "run both", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	if calls := registry.Calls(); len(calls) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(calls))
	}
}

func TestAgentEngineCarriesThinkingIntoActionContext(t *testing.T) {
	provider := &fakeProvider{responses: []*schema.Message{
		{Role: schema.RoleAssistant, Content: "先规划，再行动。"},
		{Role: schema.RoleAssistant, Content: "任务完成"},
	}}
	registry := &fakeRegistry{definitions: []schema.ToolDefinition{
		{Name: "bash", Description: "execute command"},
	}}
	engine := engine.NewAgentEngine(provider, registry, t.TempDir(), true)

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
		actionContext[2].Content != "先规划，再行动。" ||
		actionContext[3].Role != schema.RoleUser || !strings.Contains(actionContext[3].Content, "进入 Action") {
		t.Fatalf("action context = %#v", actionContext)
	}
}

func TestAgentEngineProgressivelyReadsSkillWithRealReadFile(t *testing.T) {
	workDir := t.TempDir()
	skillDir := filepath.Join(workDir, "skills", "git-workflow")
	if err := os.MkdirAll(skillDir, 0o700); err != nil {
		t.Fatal(err)
	}
	const skillLocation = "skills/git-workflow/SKILL.md"
	const skillBody = "progressive-body-secret"
	if err := os.WriteFile(filepath.Join(workDir, filepath.FromSlash(skillLocation)),
		[]byte("---\nname: git-workflow\ndescription: Handle Git workflows\n---\n"+skillBody), 0o600); err != nil {
		t.Fatal(err)
	}
	readTool, err := tools.NewReadFileTool(workDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = readTool.Close() })
	registry := tools.NewRegistry()
	if err := registry.Register(readTool); err != nil {
		t.Fatal(err)
	}
	provider := &fakeProvider{responses: []*schema.Message{
		{Role: schema.RoleAssistant, Content: "选择 git-workflow，先读取技能。"},
		{Role: schema.RoleAssistant, ToolCalls: []schema.ToolCall{{
			ID: "read-page-1", Name: "read_file",
			Arguments: json.RawMessage(`{"path":"skills/git-workflow/SKILL.md","limit":4}`),
		}}},
		{Role: schema.RoleAssistant, Content: "发现 continuation marker，继续读取。"},
		{Role: schema.RoleAssistant, ToolCalls: []schema.ToolCall{{
			ID: "read-page-2", Name: "read_file",
			Arguments: json.RawMessage(`{"path":"skills/git-workflow/SKILL.md","offset":5}`),
		}}},
		{Role: schema.RoleAssistant, Content: "技能已完整读取，可以执行。"},
		{Role: schema.RoleAssistant, Content: "done"},
	}}
	agentEngine := engine.NewAgentEngine(provider, registry, workDir, true)

	if err := agentEngine.Run(context.Background(), "提交代码", nil); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(provider.requests) != 6 {
		t.Fatalf("provider calls = %d, want 6", len(provider.requests))
	}
	for _, request := range provider.requests {
		if strings.Contains(request[0].Content, skillBody) {
			t.Fatalf("System Prompt leaked Skill Body: %q", request[0].Content)
		}
	}
	firstObservation := findMessageByToolCallID(provider.requests[2], "read-page-1")
	if firstObservation == nil || !strings.Contains(firstObservation.Content, "Use offset=5") ||
		strings.Contains(firstObservation.Content, skillBody) {
		t.Fatalf("first observation = %#v", firstObservation)
	}
	secondObservation := findMessageByToolCallID(provider.requests[4], "read-page-2")
	if secondObservation == nil || secondObservation.Content != skillBody {
		t.Fatalf("second observation = %#v", secondObservation)
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

func TestAgentEngineRejectsNilThinkingResponse(t *testing.T) {
	provider := &fakeProvider{responses: []*schema.Message{nil}}
	engine := engine.NewAgentEngine(provider, &fakeRegistry{}, t.TempDir(), true)

	err := engine.Run(context.Background(), "hello", nil)
	if err == nil || !strings.Contains(err.Error(), "Thinking") || !strings.Contains(err.Error(), "empty response") {
		t.Fatalf("Run() error = %v, want empty Thinking response error", err)
	}
}

func TestAgentEngineRejectsToolCallsDuringThinking(t *testing.T) {
	provider := &fakeProvider{responses: []*schema.Message{
		{
			Role: schema.RoleAssistant,
			ToolCalls: []schema.ToolCall{
				{ID: "call-1", Name: "bash", Arguments: json.RawMessage(`{}`)},
			},
		},
	}}
	registry := &fakeRegistry{definitions: []schema.ToolDefinition{{Name: "bash"}}}
	engine := engine.NewAgentEngine(provider, registry, t.TempDir(), true)

	err := engine.Run(context.Background(), "hello", nil)
	if err == nil || !strings.Contains(err.Error(), "Thinking") || !strings.Contains(err.Error(), "tool calls") {
		t.Fatalf("Run() error = %v, want Thinking tool-call error", err)
	}
	if calls := registry.Calls(); len(calls) != 0 {
		t.Fatalf("tool calls = %d, want 0", len(calls))
	}
}

func TestAgentEngineRejectsInvalidThinkingMessages(t *testing.T) {
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
			response: &schema.Message{Role: schema.RoleUser, Content: "plan"},
			want:     "assistant role",
		},
		{
			name:     "unexpected tool call ID",
			response: &schema.Message{Role: schema.RoleAssistant, Content: "plan", ToolCallID: "call-1"},
			want:     "tool_call_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &fakeProvider{responses: []*schema.Message{tt.response}}
			engine := engine.NewAgentEngine(provider, &fakeRegistry{}, t.TempDir(), true)

			err := engine.Run(context.Background(), "hello", nil)
			if err == nil || !strings.Contains(err.Error(), "Thinking") || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Run() error = %v, want Thinking error containing %q", err, tt.want)
			}
		})
	}
}

func TestAgentEngineRejectsInvalidActionMessages(t *testing.T) {
	tests := []struct {
		name     string
		response *schema.Message
		want     string
	}{
		{
			name:     "wrong role",
			response: &schema.Message{Role: schema.RoleUser, Content: "done"},
			want:     "assistant role",
		},
		{
			name:     "unexpected tool call ID",
			response: &schema.Message{Role: schema.RoleAssistant, Content: "done", ToolCallID: "call-1"},
			want:     "tool_call_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &fakeProvider{responses: []*schema.Message{tt.response}}
			engine := engine.NewAgentEngine(provider, &fakeRegistry{}, t.TempDir(), false)

			err := engine.Run(context.Background(), "hello", nil)
			if err == nil || !strings.Contains(err.Error(), "Action") || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Run() error = %v, want Action error containing %q", err, tt.want)
			}
		})
	}
}

func TestAgentEngineRunsThinkingBeforeEveryActionTurn(t *testing.T) {
	provider := &fakeProvider{responses: []*schema.Message{
		{Role: schema.RoleAssistant, Content: "先查看目录。"},
		{
			Role: schema.RoleAssistant,
			ToolCalls: []schema.ToolCall{
				{ID: "call-1", Name: "bash", Arguments: json.RawMessage(`{"command":"ls -la"}`)},
			},
		},
		{Role: schema.RoleAssistant, Content: "已有观察结果，接下来总结。"},
		{Role: schema.RoleAssistant, Content: "完成"},
	}}
	registry := &fakeRegistry{
		definitions: []schema.ToolDefinition{{Name: "bash"}},
		results: map[string]schema.ToolResult{
			"bash": {ToolCallID: "call-1", Output: "main.go"},
		},
	}
	engine := engine.NewAgentEngine(provider, registry, t.TempDir(), true)

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
	if lastBeforeThinking.ToolCallID != "call-1" || lastBeforeThinking.Content != "main.go" {
		t.Fatalf("second thinking observation = %#v", lastBeforeThinking)
	}
	secondActionContext := provider.requests[3]
	observation := findMessageByToolCallID(secondActionContext, "call-1")
	if observation == nil || observation.Content != "main.go" {
		t.Fatalf("second action observation = %#v", observation)
	}
	if got := secondActionContext[len(secondActionContext)-2]; got.Role != schema.RoleAssistant ||
		got.Content != "已有观察结果，接下来总结。" {
		t.Fatalf("second action thinking = %#v", got)
	}
	if got := secondActionContext[len(secondActionContext)-1]; got.Role != schema.RoleUser ||
		!strings.Contains(got.Content, "进入 Action") {
		t.Fatalf("second action transition = %#v", got)
	}
}
