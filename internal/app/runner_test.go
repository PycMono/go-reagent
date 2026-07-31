package app

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PycMono/go-reagent/internal/config"
	"github.com/PycMono/go-reagent/internal/engine"
	"github.com/PycMono/go-reagent/internal/schema"
)

type runtimeFunc func(context.Context, schema.RunRequest, engine.Reporter) (schema.RunResult, error)

func (f runtimeFunc) Run(
	ctx context.Context,
	request schema.RunRequest,
	reporter engine.Reporter,
) (schema.RunResult, error) {
	return f(ctx, request, reporter)
}

type runnerReporterFake struct{}

func (*runnerReporterFake) Report(context.Context, schema.AgentEvent) {}

func TestAgentRunnerBuildsStructuredRequestAndForwardsReporter(t *testing.T) {
	called := make(chan struct{}, 1)
	reporter := &runnerReporterFake{}
	runtime := runtimeFunc(func(_ context.Context, request schema.RunRequest, gotReporter engine.Reporter) (schema.RunResult, error) {
		if request.Input.Role != schema.RoleUser {
			t.Errorf("Run(Input.Role) = %q, want user", request.Input.Role)
		}
		text, err := schema.TextContent(request.Input.Content)
		if err != nil || text != "test prompt" {
			t.Errorf("Run(Input.Content) = %q, %v", text, err)
		}
		if len(request.History) != 0 || len(request.Context) != 0 {
			t.Errorf("Run(request) = %#v, want empty history and context", request)
		}
		if gotReporter != reporter {
			t.Errorf("Run(reporter) = %T, want injected reporter", gotReporter)
		}
		called <- struct{}{}
		return schema.RunResult{}, nil
	})
	runner := NewAgentRunner(runtime, config.Prompt("test prompt"), reporter)
	completed := make(chan error, 1)

	if err := runner.Start(func(err error) { completed <- err }); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("AgentRuntime.Run was not called")
	}
	select {
	case err := <-completed:
		if err != nil {
			t.Fatalf("completion error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("completion callback was not called")
	}
}

func TestAgentRunnerReportsRuntimeError(t *testing.T) {
	want := errors.New("runtime failed")
	runner := NewAgentRunner(runtimeFunc(func(context.Context, schema.RunRequest, engine.Reporter) (schema.RunResult, error) {
		return schema.RunResult{}, want
	}), config.Prompt("test"), nil)
	completed := make(chan error, 1)
	if err := runner.Start(func(err error) { completed <- err }); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	select {
	case got := <-completed:
		if !errors.Is(got, want) {
			t.Fatalf("completion error = %v, want %v", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("completion callback was not called")
	}
}

func TestAgentRunnerRejectsSecondStart(t *testing.T) {
	release := make(chan struct{})
	var calls atomic.Int32
	runner := NewAgentRunner(runtimeFunc(func(context.Context, schema.RunRequest, engine.Reporter) (schema.RunResult, error) {
		calls.Add(1)
		<-release
		return schema.RunResult{}, nil
	}), config.Prompt("test"), nil)
	if err := runner.Start(nil); err != nil {
		t.Fatalf("first Start() error = %v", err)
	}
	if err := runner.Start(nil); err == nil {
		t.Fatal("second Start() error = nil")
	}
	close(release)
	if err := runner.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("AgentRuntime.Run calls = %d, want 1", got)
	}
}

func TestAgentRunnerStopCancelsAndWaitsForRuntime(t *testing.T) {
	canceled := make(chan struct{})
	runner := NewAgentRunner(runtimeFunc(func(ctx context.Context, _ schema.RunRequest, _ engine.Reporter) (schema.RunResult, error) {
		<-ctx.Done()
		close(canceled)
		return schema.RunResult{}, ctx.Err()
	}), config.Prompt("test"), nil)
	if err := runner.Start(nil); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := runner.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	select {
	case <-canceled:
	default:
		t.Fatal("Stop returned before runtime observed cancellation")
	}
}

func TestAgentRunnerStopHonorsDeadline(t *testing.T) {
	release := make(chan struct{})
	runner := NewAgentRunner(runtimeFunc(func(context.Context, schema.RunRequest, engine.Reporter) (schema.RunResult, error) {
		<-release
		return schema.RunResult{}, nil
	}), config.Prompt("test"), nil)
	if err := runner.Start(nil); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := runner.Stop(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stop() error = %v, want deadline exceeded", err)
	}
	close(release)
	if err := runner.Stop(context.Background()); err != nil {
		t.Fatalf("second Stop() error = %v", err)
	}
}
