package app

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PycMono/go-reagent/internal/engine"
)

type agentFunc func(context.Context, string, engine.Reporter) error

func (f agentFunc) Run(ctx context.Context, prompt string, reporter engine.Reporter) error {
	return f(ctx, prompt, reporter)
}

func TestAgentRunnerPassesPromptAndReporterToAgent(t *testing.T) {
	reporter := engine.NewTerminalReporter()
	called := make(chan struct{}, 1)
	agent := agentFunc(func(_ context.Context, prompt string, gotReporter engine.Reporter) error {
		if prompt != "test prompt" || gotReporter != reporter {
			t.Errorf("Run(prompt, reporter) = (%q, %T)", prompt, gotReporter)
		}
		called <- struct{}{}
		return nil
	})
	runner := NewAgentRunner(agent, reporter, Prompt("test prompt"))
	completed := make(chan error, 1)

	if err := runner.Start(func(err error) { completed <- err }); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("Agent.Run was not called")
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

func TestAgentRunnerReportsAgentError(t *testing.T) {
	want := errors.New("agent failed")
	runner := NewAgentRunner(
		agentFunc(func(context.Context, string, engine.Reporter) error { return want }),
		engine.NewTerminalReporter(),
		Prompt("test"),
	)
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
	runner := NewAgentRunner(
		agentFunc(func(context.Context, string, engine.Reporter) error {
			calls.Add(1)
			<-release
			return nil
		}),
		engine.NewTerminalReporter(),
		Prompt("test"),
	)
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
		t.Fatalf("Agent.Run calls = %d, want 1", got)
	}
}

func TestAgentRunnerStopCancelsAndWaitsForAgent(t *testing.T) {
	canceled := make(chan struct{})
	runner := NewAgentRunner(
		agentFunc(func(ctx context.Context, _ string, _ engine.Reporter) error {
			<-ctx.Done()
			close(canceled)
			return ctx.Err()
		}),
		engine.NewTerminalReporter(),
		Prompt("test"),
	)
	if err := runner.Start(nil); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := runner.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	select {
	case <-canceled:
	default:
		t.Fatal("Stop returned before Agent observed cancellation")
	}
}

func TestAgentRunnerStopHonorsDeadline(t *testing.T) {
	release := make(chan struct{})
	runner := NewAgentRunner(
		agentFunc(func(context.Context, string, engine.Reporter) error {
			<-release
			return nil
		}),
		engine.NewTerminalReporter(),
		Prompt("test"),
	)
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
