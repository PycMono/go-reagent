package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/PycMono/go-reagent/internal/schema"
)

type registryEntry struct {
	definition   schema.ToolDefinition
	tool         Tool
	validateArgs func(json.RawMessage) error
	handler      Handler
}

type registryImpl struct {
	tools map[string]registryEntry
}

func NewRegistry(params RegistryParams) (Registry, error) {
	registry := &registryImpl{tools: make(map[string]registryEntry, len(params.Tools))}
	handler := composeHandler(params.Middlewares)
	for _, tool := range params.Tools {
		if isNilTool(tool) {
			return nil, errors.New("tool must not be nil")
		}
		definition, err := safeToolDefinition(tool)
		if err != nil {
			return nil, err
		}
		name := strings.TrimSpace(definition.Name)
		if name == "" {
			return nil, errors.New("tool definition name must not be empty")
		}
		if definition.Name != name {
			return nil, fmt.Errorf("tool definition name %q must not contain surrounding whitespace", definition.Name)
		}
		if _, exists := registry.tools[name]; exists {
			return nil, fmt.Errorf("tool %q is already registered", name)
		}
		validateArgs, err := compileSchemaValidator(definition)
		if err != nil {
			return nil, err
		}
		registry.tools[name] = registryEntry{
			definition:   definition,
			tool:         tool,
			validateArgs: validateArgs,
			handler:      handler,
		}
	}
	return registry, nil
}

func safeToolDefinition(tool Tool) (definition schema.ToolDefinition, err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("tool metadata panicked during registration")
		}
	}()
	return tool.Definition(), nil
}

func (r *registryImpl) GetAvailableTools() []schema.ToolDefinition {
	definitions := make([]schema.ToolDefinition, 0, len(r.tools))
	for _, entry := range r.tools {
		definitions = append(definitions, entry.definition)
	}
	sort.Slice(definitions, func(i, j int) bool {
		return definitions[i].Name < definitions[j].Name
	})
	return definitions
}

func (r *registryImpl) Execute(
	ctx context.Context,
	call schema.ToolCall,
	observer ToolEventObserver,
) (schema.ToolResult, error) {
	if ctx == nil {
		return errorResult(call, errors.New("tool execution context is nil")), nil
	}
	if err := ctx.Err(); err != nil {
		return schema.ToolResult{}, err
	}
	entry, ok := r.tools[call.Name]
	if !ok {
		return errorResult(call, fmt.Errorf("tool %q is not registered", call.Name)), nil
	}
	observe(ctx, observer, schema.NewToolStart(call))
	execution := Execution{
		Call:         call,
		Definition:   entry.definition,
		Tool:         entry.tool,
		Observer:     observer,
		ValidateArgs: entry.validateArgs,
	}
	output, err := entry.handler(ctx, execution, nil)
	result := normalizeToolResult(call, output, err)
	observe(ctx, observer, schema.NewToolEnd(call, result))
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return result, err
	}
	return result, nil
}

func observe(ctx context.Context, observer ToolEventObserver, event schema.ToolEvent) {
	if observer != nil {
		observer(ctx, event)
	}
}

func normalizeToolResult(call schema.ToolCall, output schema.ToolOutput, err error) schema.ToolResult {
	if err != nil && len(output.Content) == 0 {
		output.Content = []schema.ContentBlock{schema.TextBlock(err.Error())}
	}
	if err == nil && len(output.Content) == 0 {
		output.Content = []schema.ContentBlock{schema.TextBlock("(no output)")}
	}
	output = limitToolOutput(output)
	return schema.ToolResult{
		ToolCallID: call.ID,
		ToolName:   call.Name,
		Content:    output.Content,
		Details:    output.Details,
		IsError:    err != nil,
	}
}

func errorResult(call schema.ToolCall, err error) schema.ToolResult {
	return normalizeToolResult(call, schema.ToolOutput{}, err)
}

func isNilTool(tool Tool) bool {
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
