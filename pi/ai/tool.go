package ai

import (
	"context"
	"encoding/json"
	"errors"
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

// InputSchemaObject returns the tool input schema as a JSON object while
// normalizing json.Number values for SDK serialization.
func (definition ToolDefinition) InputSchemaObject() (map[string]any, error) {
	object, err := toolSchemaObject(definition.InputSchema)
	if err != nil {
		return nil, fmt.Errorf("tool %q input schema: %w", definition.Name, err)
	}
	if schemaType, exists := object["type"]; exists && schemaType != "object" {
		return nil, fmt.Errorf("tool %q input schema type must be object", definition.Name)
	}
	return object, nil
}

func toolSchemaObject(value any) (map[string]any, error) {
	if value == nil {
		return nil, nil
	}
	if object, ok := value.(map[string]any); ok {
		normalized, err := normalizeToolSchemaNumbers(object)
		if err != nil {
			return nil, err
		}
		return normalized.(map[string]any), nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var object map[string]any
	if err := json.Unmarshal(encoded, &object); err != nil {
		return nil, err
	}
	if object == nil {
		return nil, errors.New("JSON schema must be an object")
	}
	return object, nil
}

func normalizeToolSchemaNumbers(value any) (any, error) {
	switch typed := value.(type) {
	case json.Number:
		if integer, err := typed.Int64(); err == nil {
			return integer, nil
		}
		number, err := typed.Float64()
		if err != nil {
			return nil, fmt.Errorf("invalid JSON schema number %q: %w", typed, err)
		}
		return number, nil
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, child := range typed {
			normalized, err := normalizeToolSchemaNumbers(child)
			if err != nil {
				return nil, err
			}
			result[key] = normalized
		}
		return result, nil
	case []any:
		result := make([]any, len(typed))
		for index, child := range typed {
			normalized, err := normalizeToolSchemaNumbers(child)
			if err != nil {
				return nil, err
			}
			result[index] = normalized
		}
		return result, nil
	default:
		return value, nil
	}
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
	Content ContentBlocks `json:"content"`
	Details any           `json:"details,omitempty"`
}

// ToolUpdate is an incremental update emitted while a tool is running.
type ToolUpdate struct {
	Content ContentBlocks `json:"content"`
	Details any           `json:"details,omitempty"`
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
