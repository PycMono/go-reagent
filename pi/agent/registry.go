package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/PycMono/go-reagent/pi/ai"
)

type registryEntry struct {
	definition   ai.ToolDefinition
	tool         Tool
	validateArgs func(json.RawMessage) error
	handler      Handler
}

type registryImpl struct {
	tools map[string]registryEntry
}

func NewRegistry(options RegistryOptions) (Registry, error) {
	registry := &registryImpl{tools: make(map[string]registryEntry, len(options.Tools))}
	handler := composeHandler(options.Middlewares)
	for _, tool := range options.Tools {
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

func safeToolDefinition(tool Tool) (definition ai.ToolDefinition, err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("tool metadata panicked during registration")
		}
	}()
	return tool.Definition(), nil
}

func (r *registryImpl) GetAvailableTools() []ai.ToolDefinition {
	definitions := make([]ai.ToolDefinition, 0, len(r.tools))
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
	call ai.ToolCall,
	observer ToolEventObserver,
) (ToolResult, error) {
	if ctx == nil {
		return errorResult(call, errors.New("tool execution context is nil")), nil
	}
	if err := ctx.Err(); err != nil {
		return ToolResult{}, err
	}
	entry, ok := r.tools[call.Name]
	if !ok {
		return errorResult(call, fmt.Errorf("tool %q is not registered", call.Name)), nil
	}
	observe(ctx, observer, NewToolStart(call))
	execution := Execution{
		Call:         call,
		Definition:   entry.definition,
		Tool:         entry.tool,
		Observer:     observer,
		ValidateArgs: entry.validateArgs,
	}
	output, err := entry.handler(ctx, execution, nil)
	result := normalizeToolResult(call, output, err)
	observe(ctx, observer, NewToolEnd(call, result))
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return result, err
	}
	return result, nil
}

func observe(ctx context.Context, observer ToolEventObserver, event ToolEvent) {
	if observer != nil {
		observer(ctx, event)
	}
}

func normalizeToolResult(call ai.ToolCall, output ToolOutput, err error) ToolResult {
	if err != nil && len(output.Content) == 0 {
		output.Content = []ai.ContentBlock{ai.TextBlock(err.Error())}
	}
	if err == nil && len(output.Content) == 0 {
		output.Content = []ai.ContentBlock{ai.TextBlock("(no output)")}
	}
	output = limitToolOutput(output)
	return ToolResult{
		ToolCallID: call.ID,
		ToolName:   call.Name,
		Content:    output.Content,
		Details:    output.Details,
		IsError:    err != nil,
	}
}

func errorResult(call ai.ToolCall, err error) ToolResult {
	return normalizeToolResult(call, ToolOutput{}, err)
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
