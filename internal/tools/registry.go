package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"runtime/debug"
	"sort"
	"strings"
	"sync"

	logsdk "github.com/PycMono/go-logger-sdk"
	"github.com/PycMono/go-reagent/internal/schema"
)

// BaseTool 是所有具体工具必须实现的通用接口。
type BaseTool interface {
	Name() string
	Definition() schema.ToolDefinition
	Execute(ctx context.Context, args json.RawMessage) (string, error)
}

// Registry 定义了工具的注册与分发执行接口
type Registry interface {
	// GetAvailableTools 返回当前系统挂载的所有可用工具的 Schema
	GetAvailableTools() []schema.ToolDefinition

	// Execute 实际执行模型请求的工具，并返回结果
	Execute(ctx context.Context, call schema.ToolCall) schema.ToolResult
}

// MutableRegistry 在 Engine 使用的只读执行接口上增加启动期注册能力。
type MutableRegistry interface {
	Registry
	Register(tool BaseTool) error
}

type registryImpl struct {
	mu    sync.RWMutex
	tools map[string]BaseTool
}

// NewRegistry 创建一个线程安全的内存工具注册表。
func NewRegistry() MutableRegistry {
	return &registryImpl{tools: make(map[string]BaseTool)}
}

func (r *registryImpl) Register(tool BaseTool) (err error) {
	if isNilTool(tool) {
		return errors.New("tool must not be nil")
	}
	defer func() {
		if recover() != nil {
			err = errors.New("tool metadata panicked during registration")
		}
	}()

	name := strings.TrimSpace(tool.Name())
	if name == "" {
		return errors.New("tool name must not be empty")
	}
	definition := tool.Definition()
	if definition.Name != name {
		return fmt.Errorf("tool definition name %q must match registered name %q", definition.Name, name)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("tool %q is already registered", name)
	}
	r.tools[name] = tool
	logsdk.Info(context.Background(), "[Registry] 成功挂载工具",
		logsdk.Any("component", "registry"),
		logsdk.Any("tool", name),
	)
	return nil
}

func (r *registryImpl) GetAvailableTools() []schema.ToolDefinition {
	r.mu.RLock()
	tools := make([]BaseTool, 0, len(r.tools))
	for _, tool := range r.tools {
		tools = append(tools, tool)
	}
	r.mu.RUnlock()

	definitions := make([]schema.ToolDefinition, 0, len(tools))
	for _, tool := range tools {
		definitions = append(definitions, tool.Definition())
	}
	sort.Slice(definitions, func(i, j int) bool {
		return definitions[i].Name < definitions[j].Name
	})
	return definitions
}

func (r *registryImpl) Execute(ctx context.Context, call schema.ToolCall) (result schema.ToolResult) {
	result.ToolCallID = call.ID
	if ctx == nil {
		result.Output = "tool execution failed: context is nil"
		result.IsError = true
		return result
	}
	if err := ctx.Err(); err != nil {
		result.Output = fmt.Sprintf("tool execution canceled: %v", err)
		result.IsError = true
		return result
	}

	r.mu.RLock()
	tool, exists := r.tools[call.Name]
	r.mu.RUnlock()
	if !exists {
		result.Output = fmt.Sprintf("tool %q is not registered", call.Name)
		result.IsError = true
		return result
	}

	defer func() {
		if recover() == nil {
			return
		}
		logsdk.Error(ctx, "工具执行 panic",
			logsdk.Any("component", "registry"),
			logsdk.Any("tool", call.Name),
			logsdk.Any("stack", debug.Stack()),
		)
		result = schema.ToolResult{
			ToolCallID: call.ID,
			Output:     fmt.Sprintf("tool %q panicked during execution", call.Name),
			IsError:    true,
		}
	}()

	output, err := tool.Execute(ctx, call.Arguments)
	if err != nil {
		result.Output = fmt.Sprintf("tool %q failed: %v", call.Name, err)
		result.IsError = true
		return result
	}
	if err := ctx.Err(); err != nil {
		result.Output = fmt.Sprintf("tool execution canceled: %v", err)
		result.IsError = true
		return result
	}

	result.Output = output
	return result
}

func isNilTool(tool BaseTool) bool {
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
