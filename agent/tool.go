package agent

import (
	"context"
	"encoding/json"

	"github.com/PycMono/go-reagent/ai"
)

type UpdateEmitter func(ToolUpdate)

type Tool interface {
	Definition() ai.ToolDefinition
	Execute(context.Context, json.RawMessage, UpdateEmitter) (ToolOutput, error)
}

type ToolEventObserver func(context.Context, ToolEvent)

type Registry interface {
	GetAvailableTools() []ai.ToolDefinition
	Execute(context.Context, ai.ToolCall, ToolEventObserver) (ToolResult, error)
}

type Execution struct {
	Call         ai.ToolCall
	Definition   ai.ToolDefinition
	Tool         Tool
	Observer     ToolEventObserver
	ValidateArgs func(json.RawMessage) error
}

type Handler func(context.Context, Execution, UpdateEmitter) (ToolOutput, error)
type Middleware func(Handler) Handler

type MiddlewareRegistration struct {
	Name       string
	Order      int
	Middleware Middleware
}

// RegistryOptions contains the immutable tool and middleware snapshot.
type RegistryOptions struct {
	Tools       []Tool
	Middlewares []MiddlewareRegistration
}
