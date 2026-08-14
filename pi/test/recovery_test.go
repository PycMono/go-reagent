package test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/harness"
	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
)

type providerStep struct {
	response *ai.Message
	err      error
}

type scriptedProvider struct {
	mu        sync.Mutex
	steps     []providerStep
	requests  [][]ai.Message
	afterCall func(int)
}

func (p *scriptedProvider) Generate(
	_ context.Context,
	messages []ai.Message,
	_ []ai.ToolDefinition,
) (*ai.Message, error) {
	p.mu.Lock()
	call := len(p.requests) + 1
	p.requests = append(p.requests, append([]ai.Message(nil), messages...))
	step := p.steps[call-1]
	p.mu.Unlock()

	if p.afterCall != nil {
		p.afterCall(call)
	}
	return withTestUsage(step.response), step.err
}

func (p *scriptedProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.requests)
}

func TestLoopRetriesTransientGenerationTwice(t *testing.T) {
	provider := &scriptedProvider{steps: []providerStep{
		{err: pierrors.Wrap(pierrors.ErrorCodeAITransient, "test", errors.New("first"))},
		{err: pierrors.Wrap(pierrors.ErrorCodeAIRateLimited, "test", errors.New("second"))},
		{response: &ai.Message{Role: ai.RoleAssistant, Content: blocks("done")}},
	}}
	loop := pi.NewLoop(provider, pi.NewScheduler(&fakeToolRuntime{}, 1), false)
	started := time.Now()
	messages, err := loop.Run(context.Background(), harness.Context{Messages: []ai.Message{{
		Role: ai.RoleUser, Content: blocks("run"),
	}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if provider.callCount() != 3 || len(messages) != 1 {
		t.Fatalf("calls/messages = %d/%#v, want 3/one", provider.callCount(), messages)
	}
	if elapsed := time.Since(started); elapsed < 1400*time.Millisecond {
		t.Fatalf("elapsed = %s, want both retry delays", elapsed)
	}
}

func TestLoopDoesNotRetryTerminalAICode(t *testing.T) {
	provider := &scriptedProvider{steps: []providerStep{{
		err: pierrors.Wrap(pierrors.ErrorCodeAIUnauthorized, "test", errors.New("unauthorized")),
	}}}
	loop := pi.NewLoop(provider, pi.NewScheduler(&fakeToolRuntime{}, 1), false)
	_, err := loop.Run(context.Background(), harness.Context{Messages: []ai.Message{{
		Role: ai.RoleUser, Content: blocks("run"),
	}}}, nil)
	if pierrors.ErrorCodeOf(err) != pierrors.ErrorCodeAIUnauthorized || provider.callCount() != 1 {
		t.Fatalf("error/calls = %v/%d", err, provider.callCount())
	}
}

func TestLoopCancelsDuringRetryBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	provider := &scriptedProvider{
		steps: []providerStep{{
			err: pierrors.Wrap(pierrors.ErrorCodeAITransient, "test", errors.New("temporary")),
		}},
		afterCall: func(call int) {
			if call == 1 {
				cancel()
			}
		},
	}
	loop := pi.NewLoop(provider, pi.NewScheduler(&fakeToolRuntime{}, 1), false)
	started := time.Now()
	_, err := loop.Run(ctx, harness.Context{Messages: []ai.Message{{
		Role: ai.RoleUser, Content: blocks("run"),
	}}}, nil)
	if !errors.Is(err, context.Canceled) || provider.callCount() != 1 {
		t.Fatalf("error/calls = %v/%d", err, provider.callCount())
	}
	if elapsed := time.Since(started); elapsed >= 500*time.Millisecond {
		t.Fatalf("elapsed = %s, retry backoff ignored cancellation", elapsed)
	}
}
