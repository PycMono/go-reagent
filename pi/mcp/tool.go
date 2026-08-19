package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/PycMono/go-reagent/pi/ai"
)

type toolCaller interface {
	CallTool(context.Context, string, json.RawMessage) (CallToolResult, error)
}

type proxyTool struct {
	caller     toolCaller
	remoteName string
	definition ai.ToolDefinition
}

func newProxyTool(caller toolCaller, remote Tool, exposedName string) *proxyTool {
	label := strings.TrimSpace(remote.Title)
	if label == "" {
		label = remote.Name
	}
	return &proxyTool{
		caller:     caller,
		remoteName: remote.Name,
		definition: ai.ToolDefinition{
			Name:         exposedName,
			Label:        label,
			Description:  remote.Description,
			InputSchema:  remote.InputSchema,
			ParallelSafe: false,
		},
	}
}

func (tool *proxyTool) Definition() ai.ToolDefinition { return tool.definition }

func (tool *proxyTool) Execute(ctx context.Context, arguments json.RawMessage, _ ai.UpdateEmitter) (ai.ToolOutput, error) {
	result, err := tool.caller.CallTool(ctx, tool.remoteName, arguments)
	if err != nil {
		return ai.ToolOutput{}, fmt.Errorf("call MCP tool %q: %w", tool.remoteName, err)
	}
	if result.IsError {
		return ai.ToolOutput{}, fmt.Errorf("remote tool %q returned an error", tool.remoteName)
	}
	texts := make([]string, 0, len(result.Content))
	for _, content := range result.Content {
		if content.Type != "text" {
			return ai.ToolOutput{}, fmt.Errorf("MCP tool %q returned unsupported content type %q", tool.remoteName, content.Type)
		}
		texts = append(texts, content.Text)
	}
	if len(texts) > 0 {
		return ai.ToolOutput{Content: []ai.ContentBlock{ai.TextBlock(strings.Join(texts, "\n"))}}, nil
	}
	if result.StructuredContent != nil {
		data, err := json.Marshal(result.StructuredContent)
		if err != nil {
			return ai.ToolOutput{}, fmt.Errorf("encode structured content from MCP tool %q: %w", tool.remoteName, err)
		}
		return ai.ToolOutput{Content: []ai.ContentBlock{ai.TextBlock(string(data))}}, nil
	}
	return ai.ToolOutput{}, nil
}

var _ ai.Tool = (*proxyTool)(nil)
