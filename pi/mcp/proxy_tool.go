package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/PycMono/go-reagent/pi/ai"
)

var _ ai.Tool = (*proxyTool)(nil)

// proxyTool 把远端 MCP 工具包装为 ai.Tool：注册时以暴露名转发定义，
// 执行时以远端名转发调用。仅由 mcpExtension.Register 构造。
type proxyTool struct {
	caller     *Client
	remoteName string
	definition ai.ToolDefinition
}

func newProxyTool(caller *Client, remote Tool, exposedName string) *proxyTool {
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

// Execute 以远端名转发调用，把结果压成一段文本交给 Agent：远端报错
// 或 isError 时转成 error，否则取 text 内容块（缺失时兜底 structuredContent）。
func (tool *proxyTool) Execute(ctx context.Context, arguments json.RawMessage, _ ai.UpdateEmitter) (ai.ToolOutput, error) {
	result, err := tool.caller.CallTool(ctx, tool.remoteName, arguments)
	if err != nil {
		return ai.ToolOutput{}, fmt.Errorf("call MCP tool %q: %w", tool.remoteName, err)
	}
	if result.IsError {
		return ai.ToolOutput{}, fmt.Errorf("remote tool %q returned an error", tool.remoteName)
	}

	text, err := result.text()
	if err != nil {
		return ai.ToolOutput{}, fmt.Errorf("MCP tool %q: %w", tool.remoteName, err)
	}
	if text == "" {
		return ai.ToolOutput{}, nil
	}

	return ai.ToolOutput{Content: []ai.ContentBlock{ai.TextBlock(text)}}, nil
}
