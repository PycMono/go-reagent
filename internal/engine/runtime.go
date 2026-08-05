package engine

import (
	"context"
	"errors"

	"github.com/PycMono/go-reagent/ai"
	ctxpkg "github.com/PycMono/go-reagent/internal/context"
	"github.com/PycMono/go-reagent/internal/schema"
	"github.com/PycMono/go-reagent/internal/tools"
)

// AgentRuntime is the application-facing one-shot Agent contract.
type AgentRuntime interface {
	Run(context.Context, schema.RunRequest, Reporter) (schema.RunResult, error)
}

type runContextCreator interface {
	Create(context.Context, schema.RunRequest, []ai.ToolDefinition) (ctxpkg.RunContext, error)
}

type agentLoopRunner interface {
	Run(context.Context, ctxpkg.RunContext, Reporter) ([]ai.Message, error)
}

type runtime struct {
	factory  runContextCreator
	loop     agentLoopRunner
	registry tools.Registry
}

// NewAgentRuntime creates the application facade from concrete runtime components.
func NewAgentRuntime(
	factory *ctxpkg.RunContextFactory,
	loop *AgentLoop,
	registry tools.Registry,
) AgentRuntime {
	return newAgentRuntime(factory, loop, registry)
}

func newAgentRuntime(
	factory runContextCreator,
	loop agentLoopRunner,
	registry tools.Registry,
) AgentRuntime {
	return &runtime{factory: factory, loop: loop, registry: registry}
}

func (r *runtime) Run(
	ctx context.Context,
	request schema.RunRequest,
	reporter Reporter,
) (schema.RunResult, error) {
	result := schema.RunResult{RunID: request.RunID}
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
