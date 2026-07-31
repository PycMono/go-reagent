package tools

import (
	"context"
	"encoding/json"

	"github.com/PycMono/go-reagent/internal/schema"
	"go.uber.org/fx"
)

type UpdateEmitter func(schema.ToolUpdate)

type Tool interface {
	Definition() schema.ToolDefinition
	Execute(context.Context, json.RawMessage, UpdateEmitter) (schema.ToolOutput, error)
}

type ToolEventObserver func(context.Context, schema.ToolEvent)

type Registry interface {
	GetAvailableTools() []schema.ToolDefinition
	Execute(context.Context, schema.ToolCall, ToolEventObserver) (schema.ToolResult, error)
}

type Execution struct {
	Call         schema.ToolCall
	Definition   schema.ToolDefinition
	Tool         Tool
	Observer     ToolEventObserver
	ValidateArgs func(json.RawMessage) error
}

type Handler func(context.Context, Execution, UpdateEmitter) (schema.ToolOutput, error)
type Middleware func(Handler) Handler

type MiddlewareRegistration struct {
	Name       string
	Order      int
	Middleware Middleware
}

type RegistryParams struct {
	fx.In

	Tools       []Tool                   `group:"agent_tools"`
	Middlewares []MiddlewareRegistration `group:"tool_middlewares"`
}

func textToolOutput(text string) schema.ToolOutput {
	if text == "" {
		return schema.ToolOutput{}
	}
	return schema.ToolOutput{Content: []schema.ContentBlock{schema.TextBlock(text)}}
}
