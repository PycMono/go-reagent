package app

import (
	"context"
	"errors"
	"sync"

	logsdk "github.com/PycMono/go-logger-sdk"
	"github.com/PycMono/go-reagent/internal/config"
	"github.com/PycMono/go-reagent/internal/engine"
	"go.uber.org/fx"
)

// AgentRunner executes the configured Agent task exactly once.
type AgentRunner struct {
	runtime engine.AgentRuntime
	prompt  config.Prompt

	mu       sync.Mutex
	started  bool
	stopping bool
	cancel   context.CancelFunc
	done     chan struct{}
}

// NewAgentRunner creates the one-shot application runner.
func NewAgentRunner(runtime engine.AgentRuntime, prompt config.Prompt) *AgentRunner {
	return &AgentRunner{runtime: runtime, prompt: prompt}
}

// Start launches the Agent without blocking the Fx OnStart hook.
func (r *AgentRunner) Start(onComplete func(error)) error {
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return errors.New("agent runner 已经启动")
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.started = true
	r.cancel = cancel
	r.done = make(chan struct{})
	done := r.done
	r.mu.Unlock()

	go func() {
		err := r.runtime.Run(ctx, string(r.prompt))

		r.mu.Lock()
		stopping := r.stopping
		close(done)
		r.mu.Unlock()

		if !stopping && onComplete != nil {
			onComplete(err)
		}
	}()
	return nil
}

// Stop cancels an active Agent and waits until it releases its dependencies.
func (r *AgentRunner) Stop(ctx context.Context) error {
	r.mu.Lock()
	if !r.started {
		r.mu.Unlock()
		return nil
	}
	r.stopping = true
	cancel := r.cancel
	done := r.done
	r.mu.Unlock()

	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// RegisterAgentLifecycle connects the one-shot Runner to Fx process lifecycle.
func RegisterAgentLifecycle(
	lifecycle fx.Lifecycle,
	shutdowner fx.Shutdowner,
	runner *AgentRunner,
) {
	lifecycle.Append(fx.Hook{
		OnStart: func(context.Context) error {
			return runner.Start(func(runErr error) {
				var shutdownErr error
				if runErr != nil {
					logsdk.Error(context.Background(), "Agent 引擎运行失败",
						logsdk.Any("component", "bootstrap"),
						logsdk.Err(runErr),
					)
					shutdownErr = shutdowner.Shutdown(fx.ExitCode(1))
				} else {
					shutdownErr = shutdowner.Shutdown()
				}
				if shutdownErr != nil {
					logsdk.Error(context.Background(), "触发 Fx 关闭失败",
						logsdk.Any("component", "bootstrap"),
						logsdk.Err(shutdownErr),
					)
				}
			})
		},
		OnStop: runner.Stop,
	})
}
