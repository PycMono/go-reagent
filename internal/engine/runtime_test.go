package engine

import (
	"context"
	"errors"
	"reflect"
	"testing"

	ctxpkg "github.com/PycMono/go-reagent/internal/context"
	"github.com/PycMono/go-reagent/internal/schema"
	"github.com/PycMono/go-reagent/internal/tools"
)

type runtimeFactoryFake struct {
	calls       int
	prompt      string
	definitions []schema.ToolDefinition
	runContext  ctxpkg.RunContext
	err         error
}

func (f *runtimeFactoryFake) Create(
	_ context.Context,
	prompt string,
	definitions []schema.ToolDefinition,
) (ctxpkg.RunContext, error) {
	f.calls++
	f.prompt = prompt
	f.definitions = append([]schema.ToolDefinition(nil), definitions...)
	return f.runContext, f.err
}

type runtimeLoopFake struct {
	calls      int
	runContext ctxpkg.RunContext
	reporter   Reporter
	err        error
}

type runtimeRegistryFake struct {
	definitions []schema.ToolDefinition
}

func (r *runtimeRegistryFake) GetAvailableTools() []schema.ToolDefinition {
	return append([]schema.ToolDefinition(nil), r.definitions...)
}

func (*runtimeRegistryFake) Execute(
	context.Context,
	schema.ToolCall,
	tools.ToolEventObserver,
) (schema.ToolResult, error) {
	return schema.ToolResult{}, nil
}

type runtimeReporterFake struct{}

func (*runtimeReporterFake) Report(context.Context, schema.AgentEvent) {}

func (l *runtimeLoopFake) Run(_ context.Context, runContext ctxpkg.RunContext, reporter Reporter) error {
	l.calls++
	l.runContext = runContext
	l.reporter = reporter
	return l.err
}

func TestAgentRuntimePreparesExactlyOnceBeforeLoop(t *testing.T) {
	definitions := []schema.ToolDefinition{{Name: "read", ParallelSafe: true}}
	registry := &runtimeRegistryFake{definitions: definitions}
	wantContext := ctxpkg.RunContext{
		Messages: []schema.Message{{Role: schema.RoleUser, Content: []schema.ContentBlock{schema.TextBlock("prepared")}}},
		Tools:    definitions,
	}
	factory := &runtimeFactoryFake{runContext: wantContext}
	loop := &runtimeLoopFake{}
	reporter := &runtimeReporterFake{}
	runtime := newAgentRuntime(factory, loop, registry, reporter)

	if err := runtime.Run(context.Background(), "do work"); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if factory.calls != 1 || factory.prompt != "do work" || !reflect.DeepEqual(factory.definitions, definitions) {
		t.Fatalf("factory calls=%d prompt=%q definitions=%#v", factory.calls, factory.prompt, factory.definitions)
	}
	if loop.calls != 1 || !reflect.DeepEqual(loop.runContext, wantContext) || loop.reporter != reporter {
		t.Fatalf("loop calls=%d context=%#v reporter=%T", loop.calls, loop.runContext, loop.reporter)
	}
}

func TestAgentRuntimePreparationErrorPreventsLoop(t *testing.T) {
	wantErr := errors.New("preparation failed")
	factory := &runtimeFactoryFake{err: wantErr}
	loop := &runtimeLoopFake{}
	runtime := newAgentRuntime(factory, loop, &runtimeRegistryFake{}, nil)

	err := runtime.Run(context.Background(), "do work")
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want %v", err, wantErr)
	}
	if factory.calls != 1 || loop.calls != 0 {
		t.Fatalf("factory calls=%d loop calls=%d, want 1 and 0", factory.calls, loop.calls)
	}
}

var _ tools.Registry = (*runtimeRegistryFake)(nil)
