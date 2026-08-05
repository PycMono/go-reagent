package reagent

import (
	"context"
	"errors"
	"sync"

	agentcore "github.com/PycMono/go-reagent/agent"
	"go.uber.org/fx"
)

// Agent is the concurrency-safe synchronous SDK facade.
type Agent struct {
	app     *fx.App
	runtime agentcore.Runner

	mu        sync.Mutex
	closed    bool
	active    sync.WaitGroup
	closeOnce sync.Once
	closeErr  error
}

// New constructs and starts the default Agent graph from a defensive config copy.
func New(input *Config) (*Agent, error) {
	if input == nil {
		return nil, wrap(ErrorCodeConfigInvalid, "New", errors.New("config is required"))
	}
	config := cloneConfig(input)
	if err := config.normalizeAndValidate(); err != nil {
		return nil, wrap(ErrorCodeConfigInvalid, "New", err)
	}
	app, runtime, err := buildAgent(config)
	if err != nil {
		return nil, classifyInitialization("New", err)
	}
	return &Agent{app: app, runtime: runtime}, nil
}

// Run executes one stateless request and returns only newly generated messages.
func (a *Agent) Run(ctx context.Context, request RunRequest) (RunResult, error) {
	result := RunResult{RunID: request.RunID}
	if ctx == nil {
		return result, wrap(ErrorCodeRequestInvalid, "Run", errors.New("context is required"))
	}
	if a == nil || a.runtime == nil {
		return result, wrap(ErrorCodeInternal, "Run", errors.New("agent is not initialized"))
	}
	if !a.beginRun() {
		return result, wrap(ErrorCodeClosed, "Run", ErrClosed)
	}
	defer a.active.Done()

	result, err := a.runtime.Run(ctx, request, nil)
	return result, classify("Run", err)
}

func (a *Agent) beginRun() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return false
	}
	a.active.Add(1)
	return true
}

// Close rejects new Runs, waits for admitted Runs, and stops owned resources once.
func (a *Agent) Close(ctx context.Context) error {
	if ctx == nil {
		return wrap(ErrorCodeRequestInvalid, "Close", errors.New("context is required"))
	}
	if a == nil || a.app == nil {
		return wrap(ErrorCodeInternal, "Close", errors.New("agent is not initialized"))
	}
	a.closeOnce.Do(func() { a.closeErr = a.close(ctx) })
	return a.closeErr
}

func (a *Agent) close(ctx context.Context) error {
	a.mu.Lock()
	a.closed = true
	a.mu.Unlock()

	done := make(chan struct{})
	go func() {
		a.active.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		return classify("Close", ctx.Err())
	}
	return classify("Close", a.app.Stop(ctx))
}
