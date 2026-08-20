package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
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

// ToolCalls 是一次响应中的一批工具调用请求。
type ToolCalls []ToolCall

// Validate 校验这批调用的固有契约：ID 非空且不重复、参数均为合法 JSON。
// 结果与 ToolCallID 的对齐依赖这些不变量。
func (calls ToolCalls) Validate() error {
	seen := make(map[string]struct{}, len(calls))
	for index, call := range calls {
		if call.ID == "" {
			return fmt.Errorf("tool call at index %d has empty ID", index)
		}
		if _, exists := seen[call.ID]; exists {
			return fmt.Errorf("duplicate tool call ID %q", call.ID)
		}
		seen[call.ID] = struct{}{}
		if !json.Valid(call.Arguments) {
			return fmt.Errorf("tool call %q arguments are invalid JSON", call.ID)
		}
	}

	return nil
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

// ToolDefinitions 是一批可供模型调用的工具定义。
type ToolDefinitions []ToolDefinition

// Has 报告是否存在指定名称的工具定义。
func (definitions ToolDefinitions) Has(name string) bool {
	for _, definition := range definitions {
		if definition.Name == name {
			return true
		}
	}
	return false
}

// ParallelSafety 返回每个工具名称对应的并发安全标记快照。
func (definitions ToolDefinitions) ParallelSafety() map[string]bool {
	parallelSafe := make(map[string]bool, len(definitions))
	for _, definition := range definitions {
		parallelSafe[definition.Name] = definition.ParallelSafe
	}
	return parallelSafe
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

// IsNilTool 报告工具接口是否为空或装有一个类型化 nil 值。
func IsNilTool(tool Tool) bool {
	if tool == nil {
		return true
	}
	value := reflect.ValueOf(tool)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
