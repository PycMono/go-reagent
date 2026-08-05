package ai

import "context"

// Client generates one model response from messages and available tools.
type Client interface {
	Generate(context.Context, []Message, []ToolDefinition) (*Message, error)
}
