package pi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/PycMono/go-reagent/pi/ai"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

type ToolEventObserver func(context.Context, ToolEvent)

type ToolRuntime interface {
	Definitions() []ai.ToolDefinition
	Execute(context.Context, ai.ToolCall, ToolEventObserver) (ToolResult, error)
}

// ToolRuntimeOptions contains the immutable tool and middleware snapshot.
type ToolRuntimeOptions struct {
	Tools       []ai.Tool
	Middlewares []MiddlewareRegistration
}

type toolEntry struct {
	definition   ai.ToolDefinition
	tool         ai.Tool
	validateArgs func(json.RawMessage) error
	handler      Handler
}

type toolRuntime struct {
	tools map[string]toolEntry
}

func NewToolRuntime(options ToolRuntimeOptions) (ToolRuntime, error) {
	runtime := &toolRuntime{tools: make(map[string]toolEntry, len(options.Tools))}
	handler := composeHandler(options.Middlewares)
	for _, tool := range options.Tools {
		definition := tool.Definition()
		name := strings.TrimSpace(definition.Name)
		if name == "" {
			return nil, errors.New("tool definition name must not be empty")
		}
		if definition.Name != name {
			return nil, fmt.Errorf("tool definition name %q must not contain surrounding whitespace", definition.Name)
		}
		if _, exists := runtime.tools[name]; exists {
			return nil, fmt.Errorf("tool %q is already registered", name)
		}
		validateArgs, err := compileSchemaValidator(definition)
		if err != nil {
			return nil, err
		}
		runtime.tools[name] = toolEntry{
			definition:   definition,
			tool:         tool,
			validateArgs: validateArgs,
			handler:      handler,
		}
	}
	return runtime, nil
}

func (r *toolRuntime) Definitions() []ai.ToolDefinition {
	definitions := make([]ai.ToolDefinition, 0, len(r.tools))
	for _, entry := range r.tools {
		definitions = append(definitions, entry.definition)
	}
	sort.Slice(definitions, func(i, j int) bool {
		return definitions[i].Name < definitions[j].Name
	})
	return definitions
}

func (r *toolRuntime) Execute(
	ctx context.Context,
	call ai.ToolCall,
	observer ToolEventObserver,
) (ToolResult, error) {
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
	if contextErr := ctx.Err(); contextErr != nil {
		err = contextErr
	}
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

func normalizeToolResult(call ai.ToolCall, output ai.ToolOutput, err error) ToolResult {
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
	return normalizeToolResult(call, ai.ToolOutput{}, err)
}

func compileSchemaValidator(definition ai.ToolDefinition) (func(json.RawMessage) error, error) {
	schemaJSON, err := json.Marshal(definition.InputSchema)
	if err != nil {
		return nil, fmt.Errorf("marshal input schema for tool %q: %w", definition.Name, err)
	}
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaJSON))
	if err != nil {
		return nil, fmt.Errorf("decode input schema for tool %q: %w", definition.Name, err)
	}
	location := "urn:go-reagent:tool:" + definition.Name
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(location, document); err != nil {
		return nil, fmt.Errorf("register input schema for tool %q: %w", definition.Name, err)
	}
	compiled, err := compiler.Compile(location)
	if err != nil {
		return nil, fmt.Errorf("compile input schema for tool %q: %w", definition.Name, err)
	}

	return func(arguments json.RawMessage) error {
		decoder := json.NewDecoder(bytes.NewReader(arguments))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err != nil {
			return fmt.Errorf("invalid arguments for tool %q: %w", definition.Name, err)
		}
		var extra any
		if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
			if err != nil {
				return fmt.Errorf("invalid trailing arguments for tool %q: %w", definition.Name, err)
			}
			return fmt.Errorf("invalid trailing arguments for tool %q", definition.Name)
		}
		if err := compiled.Validate(value); err != nil {
			return fmt.Errorf("arguments do not match schema for tool %q: %w", definition.Name, err)
		}
		return nil
	}, nil
}
