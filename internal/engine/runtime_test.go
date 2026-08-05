package engine

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/PycMono/go-reagent/agent"
	"github.com/PycMono/go-reagent/ai"
	ctxpkg "github.com/PycMono/go-reagent/internal/context"
)

type runtimeFactoryFake struct {
	calls       int
	request     agent.RunRequest
	definitions []ai.ToolDefinition
	runContext  ctxpkg.RunContext
	err         error
}

func (f *runtimeFactoryFake) Create(
	_ context.Context,
	request agent.RunRequest,
	definitions []ai.ToolDefinition,
) (ctxpkg.RunContext, error) {
	f.calls++
	f.request = request
	f.definitions = append([]ai.ToolDefinition(nil), definitions...)
	return f.runContext, f.err
}

type runtimeLoopFake struct {
	calls      int
	runContext ctxpkg.RunContext
	reporter   agent.Reporter
	messages   []ai.Message
	err        error
}

type runtimeRegistryFake struct {
	definitions []ai.ToolDefinition
}

func (r *runtimeRegistryFake) GetAvailableTools() []ai.ToolDefinition {
	return append([]ai.ToolDefinition(nil), r.definitions...)
}

func (*runtimeRegistryFake) Execute(
	context.Context,
	ai.ToolCall,
	agent.ToolEventObserver,
) (agent.ToolResult, error) {
	return agent.ToolResult{}, nil
}

type runtimeReporterFake struct{}

func (*runtimeReporterFake) Report(context.Context, agent.AgentEvent) {}

func (l *runtimeLoopFake) Run(_ context.Context, runContext ctxpkg.RunContext, reporter agent.Reporter) ([]ai.Message, error) {
	l.calls++
	l.runContext = runContext
	l.reporter = reporter
	return append([]ai.Message(nil), l.messages...), l.err
}

func TestAgentRuntimePreparesStructuredRequestAndReturnsIncrement(t *testing.T) {
	definitions := []ai.ToolDefinition{{Name: "read", ParallelSafe: true}}
	registry := &runtimeRegistryFake{definitions: definitions}
	wantContext := ctxpkg.RunContext{
		Messages: []ai.Message{{Role: ai.RoleUser, Content: []ai.ContentBlock{ai.TextBlock("prepared")}}},
		Tools:    definitions,
	}
	factory := &runtimeFactoryFake{runContext: wantContext}
	wantMessages := []ai.Message{{Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("done")}}}
	loop := &runtimeLoopFake{messages: wantMessages}
	reporter := &runtimeReporterFake{}
	runtime := newAgentRuntime(factory, loop, registry)
	request := agent.RunRequest{
		RunID: "run-1",
		Input: ai.Message{
			Role:    ai.RoleUser,
			Content: []ai.ContentBlock{ai.TextBlock("do work")},
		},
		Metadata: map[string]string{"conversationId": "conversation-1"},
	}

	result, err := runtime.Run(context.Background(), request, reporter)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if factory.calls != 1 || !reflect.DeepEqual(factory.request, request) || !reflect.DeepEqual(factory.definitions, definitions) {
		t.Fatalf("factory calls=%d request=%#v definitions=%#v", factory.calls, factory.request, factory.definitions)
	}
	if loop.calls != 1 || !reflect.DeepEqual(loop.runContext, wantContext) || loop.reporter != reporter {
		t.Fatalf("loop calls=%d context=%#v reporter=%T", loop.calls, loop.runContext, loop.reporter)
	}
	if result.RunID != "run-1" || !reflect.DeepEqual(result.NewMessages, wantMessages) {
		t.Fatalf("RunResult = %#v, want RunID and loop messages", result)
	}
}

func TestAgentRuntimePreparationErrorPreservesRunID(t *testing.T) {
	wantErr := errors.New("preparation failed")
	factory := &runtimeFactoryFake{err: wantErr}
	loop := &runtimeLoopFake{}
	runtime := newAgentRuntime(factory, loop, &runtimeRegistryFake{})
	request := agent.RunRequest{RunID: "run-preparation-error"}

	result, err := runtime.Run(context.Background(), request, nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want %v", err, wantErr)
	}
	if result.RunID != request.RunID || len(result.NewMessages) != 0 {
		t.Fatalf("RunResult = %#v, want RunID with no messages", result)
	}
	if factory.calls != 1 || loop.calls != 0 {
		t.Fatalf("factory calls=%d loop calls=%d, want 1 and 0", factory.calls, loop.calls)
	}
}

func TestAgentRuntimeLoopErrorPreservesIncrement(t *testing.T) {
	wantErr := errors.New("loop failed")
	wantMessages := []ai.Message{{Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("partial")}}}
	factory := &runtimeFactoryFake{runContext: ctxpkg.RunContext{}}
	loop := &runtimeLoopFake{messages: wantMessages, err: wantErr}
	runtime := newAgentRuntime(factory, loop, &runtimeRegistryFake{})

	result, err := runtime.Run(context.Background(), agent.RunRequest{RunID: "run-loop-error"}, nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want %v", err, wantErr)
	}
	if result.RunID != "run-loop-error" || !reflect.DeepEqual(result.NewMessages, wantMessages) {
		t.Fatalf("RunResult = %#v, want partial loop messages", result)
	}
}

func TestAgentRuntimeMissingDependenciesPreservesRunID(t *testing.T) {
	var runtime *runtime
	result, err := runtime.Run(context.Background(), agent.RunRequest{RunID: "run-invalid-runtime"}, nil)
	if err == nil || err.Error() != "agent runtime: factory, loop, and registry are required" {
		t.Fatalf("Run() error = %v", err)
	}
	if result.RunID != "run-invalid-runtime" || len(result.NewMessages) != 0 {
		t.Fatalf("RunResult = %#v, want RunID with no messages", result)
	}
}

var _ agent.Registry = (*runtimeRegistryFake)(nil)
