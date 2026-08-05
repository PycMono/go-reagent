package agent

import (
	"context"
	"errors"
	"fmt"
)

// Runner executes one stateless Agent request.
type Runner interface {
	Run(context.Context, RunRequest, Reporter) (RunResult, error)
}

// Agent is a reusable stateless runtime facade.
type Agent struct {
	factory  ContextFactory
	loop     *Loop
	registry Registry
}

// New creates an Agent from its lower-layer runtime contracts.
func New(factory ContextFactory, loop *Loop, registry Registry) (*Agent, error) {
	if factory == nil || loop == nil || registry == nil {
		return nil, errors.New("agent runtime: factory, loop, and registry are required")
	}
	return &Agent{factory: factory, loop: loop, registry: registry}, nil
}

// Run validates and executes one isolated request.
func (a *Agent) Run(ctx context.Context, request RunRequest, reporter Reporter) (RunResult, error) {
	result := RunResult{RunID: request.RunID}
	if a == nil || a.factory == nil || a.loop == nil || a.registry == nil {
		return result, errors.New("agent runtime: factory, loop, and registry are required")
	}
	if ctx == nil {
		return result, fmt.Errorf("%w: context is required", ErrRequestInvalid)
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := validateRunRequest(request); err != nil {
		return result, err
	}

	request = cloneRequest(request)
	runContext, err := a.factory.Create(ctx, request, a.registry.GetAvailableTools())
	if err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}

	newMessages, err := a.loop.Run(ctx, runContext, reporter)
	result.NewMessages = cloneMessages(newMessages)
	return result, err
}

var _ Runner = (*Agent)(nil)
