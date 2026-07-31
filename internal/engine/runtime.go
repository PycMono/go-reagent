package engine

import (
	"context"
	"errors"

	ctxpkg "github.com/PycMono/go-reagent/internal/context"
	"github.com/PycMono/go-reagent/internal/schema"
	"github.com/PycMono/go-reagent/internal/tools"
)

// AgentRuntime is the application-facing one-shot Agent contract.
type AgentRuntime interface {
	Run(context.Context, string) error
}

type runContextCreator interface {
	Create(context.Context, string, []schema.ToolDefinition) (ctxpkg.RunContext, error)
}

type agentLoopRunner interface {
	Run(context.Context, ctxpkg.RunContext, Reporter) error
}

type runtime struct {
	factory  runContextCreator
	loop     agentLoopRunner
	registry tools.Registry
	reporter Reporter
}

// NewAgentRuntime creates the application facade from concrete runtime components.
func NewAgentRuntime(
	factory *ctxpkg.RunContextFactory,
	loop *AgentLoop,
	registry tools.Registry,
	reporter Reporter,
) AgentRuntime {
	return newAgentRuntime(factory, loop, registry, reporter)
}

func newAgentRuntime(
	factory runContextCreator,
	loop agentLoopRunner,
	registry tools.Registry,
	reporter Reporter,
) AgentRuntime {
	return &runtime{factory: factory, loop: loop, registry: registry, reporter: reporter}
}

func (r *runtime) Run(ctx context.Context, prompt string) error {
	if r == nil || r.factory == nil || r.loop == nil || r.registry == nil {
		return errors.New("agent runtime: factory, loop, and registry are required")
	}
	runContext, err := r.factory.Create(ctx, prompt, r.registry.GetAvailableTools())
	if err != nil {
		return err
	}
	return r.loop.Run(ctx, runContext, r.reporter)
}
