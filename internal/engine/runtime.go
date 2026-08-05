package engine

import (
	"context"
	"errors"

	"github.com/PycMono/go-reagent/agent"
	"github.com/PycMono/go-reagent/ai"
	ctxpkg "github.com/PycMono/go-reagent/internal/context"
)

// AgentRuntime is the application-facing one-shot Agent contract.
type AgentRuntime interface {
	Run(context.Context, agent.RunRequest, agent.Reporter) (agent.RunResult, error)
}

type runContextCreator interface {
	Create(context.Context, agent.RunRequest, []ai.ToolDefinition) (ctxpkg.RunContext, error)
}

type agentLoopRunner interface {
	Run(context.Context, ctxpkg.RunContext, agent.Reporter) ([]ai.Message, error)
}

type runtime struct {
	factory  runContextCreator
	loop     agentLoopRunner
	registry agent.Registry
}

// NewAgentRuntime creates the application facade from concrete runtime components.
func NewAgentRuntime(
	factory *ctxpkg.RunContextFactory,
	loop *AgentLoop,
	registry agent.Registry,
) AgentRuntime {
	return newAgentRuntime(factory, loop, registry)
}

func newAgentRuntime(
	factory runContextCreator,
	loop agentLoopRunner,
	registry agent.Registry,
) AgentRuntime {
	return &runtime{factory: factory, loop: loop, registry: registry}
}

func (r *runtime) Run(
	ctx context.Context,
	request agent.RunRequest,
	reporter agent.Reporter,
) (agent.RunResult, error) {
	result := agent.RunResult{RunID: request.RunID}
	if r == nil || r.factory == nil || r.loop == nil || r.registry == nil {
		return result, errors.New("agent runtime: factory, loop, and registry are required")
	}
	runContext, err := r.factory.Create(ctx, request, r.registry.GetAvailableTools())
	if err != nil {
		return result, err
	}

	newMessages, err := r.loop.Run(ctx, runContext, reporter)
	result.NewMessages = append([]ai.Message(nil), newMessages...)

	return result, err
}
