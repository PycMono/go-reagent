package application

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PycMono/go-reagent/config"
	"github.com/PycMono/go-reagent/conversation"
	"github.com/PycMono/go-reagent/pi/agent"
	"github.com/PycMono/go-reagent/pi/ai"
)

type runtimeFunc func(context.Context, agent.RunRequest, agent.Reporter) (agent.RunResult, error)

type conversationFunc func(context.Context, conversation.RunRequest, agent.Reporter) (agent.RunResult, error)

func (f runtimeFunc) Run(
	ctx context.Context,
	request agent.RunRequest,
	reporter agent.Reporter,
) (agent.RunResult, error) {
	return f(ctx, request, reporter)
}

func (f conversationFunc) Run(
	ctx context.Context,
	request conversation.RunRequest,
	reporter agent.Reporter,
) (agent.RunResult, error) {
	return f(ctx, request, reporter)
}

type runnerReporterFake struct{}

func (*runnerReporterFake) Report(context.Context, agent.AgentEvent) {}

func TestAgentRunnerUsesStatelessRuntimeWhenPersistenceDisabled(t *testing.T) {
	runtimeCalled := make(chan struct{}, 1)
	runtime := runtimeFunc(func(context.Context, agent.RunRequest, agent.Reporter) (agent.RunResult, error) {
		runtimeCalled <- struct{}{}
		return agent.RunResult{}, nil
	})
	conversationRunner := conversationFunc(func(context.Context, conversation.RunRequest, agent.Reporter) (agent.RunResult, error) {
		t.Error("ConversationRunner.Run should not be called")
		return agent.RunResult{}, nil
	})
	runner, err := NewAgentRunner(runtime, conversationRunner, &config.Config{}, Prompt("test prompt"), nil)
	if err != nil {
		t.Fatal(err)
	}
	completed := make(chan error, 1)
	if err := runner.Start(func(err error) { completed <- err }); err != nil {
		t.Fatal(err)
	}
	select {
	case <-runtimeCalled:
	case <-time.After(time.Second):
		t.Fatal("AgentRuntime.Run was not called")
	}
	if err := <-completed; err != nil {
		t.Fatal(err)
	}
}

func TestAgentRunnerUsesConversationRunnerWhenPersistenceEnabled(t *testing.T) {
	t.Setenv("AGENT_USER_ID", " user-1 ")
	t.Setenv("AGENT_CONVERSATION_ID", " conversation-1 ")
	reporter := &runnerReporterFake{}
	runtime := runtimeFunc(func(context.Context, agent.RunRequest, agent.Reporter) (agent.RunResult, error) {
		t.Error("AgentRuntime.Run should not be called directly")
		return agent.RunResult{}, nil
	})
	called := make(chan struct{}, 1)
	conversationRunner := conversationFunc(func(_ context.Context, request conversation.RunRequest, gotReporter agent.Reporter) (agent.RunResult, error) {
		if request.UserID != "user-1" || request.ConversationID != "conversation-1" {
			t.Errorf("conversation identity = %q, %q", request.UserID, request.ConversationID)
		}
		text, err := ai.TextContent(request.Input.Content)
		if err != nil || request.Input.Role != ai.RoleUser || text != "test prompt" {
			t.Errorf("conversation input = %#v, %v", request.Input, err)
		}
		if gotReporter != reporter {
			t.Errorf("reporter = %T, want injected reporter", gotReporter)
		}
		called <- struct{}{}
		return agent.RunResult{}, nil
	})
	runner, err := NewAgentRunner(runtime, conversationRunner, &config.Config{
		Conversation: config.ConversationConfig{Enabled: true},
	}, Prompt("test prompt"), reporter)
	if err != nil {
		t.Fatal(err)
	}
	completed := make(chan error, 1)
	if err := runner.Start(func(err error) { completed <- err }); err != nil {
		t.Fatal(err)
	}
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("ConversationRunner.Run was not called")
	}
	if err := <-completed; err != nil {
		t.Fatal(err)
	}
}

func TestNewAgentRunnerValidatesPersistenceIdentityOnlyWhenEnabled(t *testing.T) {
	runtime := runtimeFunc(func(context.Context, agent.RunRequest, agent.Reporter) (agent.RunResult, error) {
		return agent.RunResult{}, nil
	})
	conversationRunner := conversationFunc(func(context.Context, conversation.RunRequest, agent.Reporter) (agent.RunResult, error) {
		return agent.RunResult{}, nil
	})
	t.Setenv("AGENT_USER_ID", "")
	t.Setenv("AGENT_CONVERSATION_ID", "")
	if _, err := NewAgentRunner(runtime, nil, &config.Config{}, "test", nil); err != nil {
		t.Fatalf("disabled NewAgentRunner() error = %v", err)
	}
	_, err := NewAgentRunner(runtime, conversationRunner, &config.Config{
		Conversation: config.ConversationConfig{Enabled: true},
	}, "test", nil)
	if err == nil || !strings.Contains(err.Error(), "AGENT_USER_ID") {
		t.Fatalf("missing user ID error = %v", err)
	}
	t.Setenv("AGENT_USER_ID", "user")
	_, err = NewAgentRunner(runtime, conversationRunner, &config.Config{
		Conversation: config.ConversationConfig{Enabled: true},
	}, "test", nil)
	if err == nil || !strings.Contains(err.Error(), "AGENT_CONVERSATION_ID") {
		t.Fatalf("missing conversation ID error = %v", err)
	}
}

func TestAgentRunnerReportsConversationError(t *testing.T) {
	t.Setenv("AGENT_USER_ID", "user")
	t.Setenv("AGENT_CONVERSATION_ID", "conversation")
	want := errors.New("conversation failed")
	runner, err := NewAgentRunner(
		runtimeFunc(func(context.Context, agent.RunRequest, agent.Reporter) (agent.RunResult, error) {
			return agent.RunResult{}, nil
		}),
		conversationFunc(func(context.Context, conversation.RunRequest, agent.Reporter) (agent.RunResult, error) {
			return agent.RunResult{}, want
		}),
		&config.Config{Conversation: config.ConversationConfig{Enabled: true}},
		"test",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	completed := make(chan error, 1)
	if err := runner.Start(func(err error) { completed <- err }); err != nil {
		t.Fatal(err)
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

func TestAgentRunnerStopCancelsSelectedConversationRunner(t *testing.T) {
	t.Setenv("AGENT_USER_ID", "user")
	t.Setenv("AGENT_CONVERSATION_ID", "conversation")
	canceled := make(chan struct{})
	runner, err := NewAgentRunner(
		runtimeFunc(func(context.Context, agent.RunRequest, agent.Reporter) (agent.RunResult, error) {
			return agent.RunResult{}, nil
		}),
		conversationFunc(func(ctx context.Context, _ conversation.RunRequest, _ agent.Reporter) (agent.RunResult, error) {
			<-ctx.Done()
			close(canceled)
			return agent.RunResult{}, ctx.Err()
		}),
		&config.Config{Conversation: config.ConversationConfig{Enabled: true}},
		"test",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Start(nil); err != nil {
		t.Fatal(err)
	}
	if err := runner.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-canceled:
	default:
		t.Fatal("Stop returned before conversation runner observed cancellation")
	}
}

func TestAgentRunnerBuildsStructuredRequestAndForwardsReporter(t *testing.T) {
	called := make(chan struct{}, 1)
	reporter := &runnerReporterFake{}
	runtime := runtimeFunc(func(_ context.Context, request agent.RunRequest, gotReporter agent.Reporter) (agent.RunResult, error) {
		if request.Input.Role != ai.RoleUser {
			t.Errorf("Run(Input.Role) = %q, want user", request.Input.Role)
		}
		text, err := ai.TextContent(request.Input.Content)
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
		return agent.RunResult{}, nil
	})
	runner := newStatelessAgentRunner(t, runtime, Prompt("test prompt"), reporter)
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
	runner := newStatelessAgentRunner(t, runtimeFunc(func(context.Context, agent.RunRequest, agent.Reporter) (agent.RunResult, error) {
		return agent.RunResult{}, want
	}), Prompt("test"), nil)
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
	runner := newStatelessAgentRunner(t, runtimeFunc(func(context.Context, agent.RunRequest, agent.Reporter) (agent.RunResult, error) {
		calls.Add(1)
		<-release
		return agent.RunResult{}, nil
	}), Prompt("test"), nil)
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
	runner := newStatelessAgentRunner(t, runtimeFunc(func(ctx context.Context, _ agent.RunRequest, _ agent.Reporter) (agent.RunResult, error) {
		<-ctx.Done()
		close(canceled)
		return agent.RunResult{}, ctx.Err()
	}), Prompt("test"), nil)
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
	runner := newStatelessAgentRunner(t, runtimeFunc(func(context.Context, agent.RunRequest, agent.Reporter) (agent.RunResult, error) {
		<-release
		return agent.RunResult{}, nil
	}), Prompt("test"), nil)
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

func newStatelessAgentRunner(t *testing.T, runtime agent.Runner, prompt Prompt, reporter agent.Reporter) *AgentRunner {
	t.Helper()
	runner, err := NewAgentRunner(runtime, nil, &config.Config{}, prompt, reporter)
	if err != nil {
		t.Fatalf("NewAgentRunner() error = %v", err)
	}
	return runner
}
