package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	logsdk "github.com/PycMono/go-logger-sdk"
	"github.com/PycMono/go-reagent"
	"github.com/PycMono/go-reagent/pi/agent"
	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/internal/cli/conversation"
	"go.uber.org/fx"
)

// AgentRunner executes the configured Agent task exactly once.
type Prompt string

type AgentRunner struct {
	runtime            agent.Runner
	conversationRunner conversation.Runner
	persistenceEnabled bool
	userID             string
	conversationID     string
	prompt             Prompt
	reporter           agent.Reporter

	mu       sync.Mutex
	started  bool
	stopping bool
	cancel   context.CancelFunc
	done     chan struct{}
}

// NewAgentRunner creates the one-shot application runner.
func NewAgentRunner(
	runtime agent.Runner,
	conversationRunner conversation.Runner,
	cfg *reagent.Config,
	prompt Prompt,
	reporter agent.Reporter,
) (*AgentRunner, error) {
	if runtime == nil {
		return nil, errors.New("agent runner: runtime is required")
	}
	if cfg == nil {
		return nil, errors.New("agent runner: config is required")
	}
	runner := &AgentRunner{runtime: runtime, prompt: prompt, reporter: reporter}
	if !cfg.Conversation.Enabled {
		return runner, nil
	}
	if conversationRunner == nil {
		return nil, errors.New("agent runner: conversation runner is required")
	}
	runner.persistenceEnabled = true
	runner.conversationRunner = conversationRunner
	runner.userID = strings.TrimSpace(os.Getenv("AGENT_USER_ID"))
	if runner.userID == "" {
		return nil, fmt.Errorf("agent runner: %s is required", "AGENT_USER_ID")
	}
	runner.conversationID = strings.TrimSpace(os.Getenv("AGENT_CONVERSATION_ID"))
	if runner.conversationID == "" {
		return nil, fmt.Errorf("agent runner: %s is required", "AGENT_CONVERSATION_ID")
	}
	return runner, nil
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
		input := ai.Message{
			Role:    ai.RoleUser,
			Content: []ai.ContentBlock{ai.TextBlock(string(r.prompt))},
		}
		var err error
		if r.persistenceEnabled {
			_, err = r.conversationRunner.Run(ctx, conversation.RunRequest{
				UserID:         r.userID,
				ConversationID: r.conversationID,
				Input:          input,
			}, r.reporter)
		} else {
			_, err = r.runtime.Run(ctx, agent.RunRequest{Input: input}, r.reporter)
		}

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
