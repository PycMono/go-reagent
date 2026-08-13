package ai

import (
	"context"
	"encoding/json"
)

// ToolCall 表示模型发起的一次工具调用请求。
type ToolCall struct {
	// ID 是工具调用的唯一标识。
	ID string `json:"id"`
	// Name 是模型请求调用的工具名称。
	Name string `json:"name"`
	// Arguments 保存未经解析的 JSON 参数，由具体工具负责解析。
	Arguments json.RawMessage `json:"arguments"`
}

// ToolDefinition 描述一个可供模型调用的工具。
type ToolDefinition struct {
	// Name 是工具的唯一名称。
	Name string `json:"name"`
	// Label 是用于展示的工具名称。
	Label string `json:"label,omitempty"`
	// Description 说明工具的用途。
	Description string `json:"description"`
	// InputSchema 使用 JSON Schema 描述工具的输入参数。
	InputSchema any `json:"input_schema"`

	// ParallelSafe 表示运行框架能否在同一批次中并发执行该工具，默认值为 false。
	ParallelSafe bool `json:"parallel_safe,omitempty"`
}

// ToolOutput is the final content produced by one tool execution.
type ToolOutput struct {
	Content []ContentBlock `json:"content"`
	Details any            `json:"details,omitempty"`
}

// ToolUpdate is an incremental update emitted while a tool is running.
type ToolUpdate struct {
	Content []ContentBlock `json:"content"`
	Details any            `json:"details,omitempty"`
}

// UpdateEmitter receives incremental updates from a running tool.
type UpdateEmitter func(ToolUpdate)

// Tool is the provider-neutral execution contract shared by Agent Core and Harness tools.
type Tool interface {
	Definition() ToolDefinition
	Execute(context.Context, json.RawMessage, UpdateEmitter) (ToolOutput, error)
}
