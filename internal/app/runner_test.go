package app

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PycMono/go-reagent/internal/config"
)

type runtimeFunc func(context.Context, string) error

func (f runtimeFunc) Run(ctx context.Context, prompt string) error { return f(ctx, prompt) }

func TestAgentRunnerPassesPromptToRuntime(t *testing.T) {
	called := make(chan struct{}, 1)
	runtime := runtimeFunc(func(_ context.Context, prompt string) error {
		if prompt != "test prompt" {
			t.Errorf("Run(prompt) = %q", prompt)
		}
		called <- struct{}{}
		return nil
	})
	runner := NewAgentRunner(runtime, config.Prompt("test prompt"))
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
	runner := NewAgentRunner(runtimeFunc(func(context.Context, string) error { return want }), config.Prompt("test"))
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
	runner := NewAgentRunner(runtimeFunc(func(context.Context, string) error {
		calls.Add(1)
		<-release
		return nil
	}), config.Prompt("test"))
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
	runner := NewAgentRunner(runtimeFunc(func(ctx context.Context, _ string) error {
		<-ctx.Done()
		close(canceled)
		return ctx.Err()
	}), config.Prompt("test"))
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
	runner := NewAgentRunner(runtimeFunc(func(context.Context, string) error {
		<-release
		return nil
	}), config.Prompt("test"))
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
