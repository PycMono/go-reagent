package pi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/PycMono/go-reagent/pi/ai"
	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
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

type toolRuntime struct {
	registry *toolRegistry
	handler  Handler
}

func NewToolRuntime(options ToolRuntimeOptions) (ToolRuntime, error) {
	registry, err := newToolRegistry(options.Tools)
	if err != nil {
		return nil, err
	}
	registry.freeze()
	return newToolRuntimeFromRegistry(registry, options.Middlewares), nil
}

func newToolRuntimeFromRegistry(registry *toolRegistry, middlewares []MiddlewareRegistration) ToolRuntime {
	return &toolRuntime{registry: registry, handler: composeHandler(middlewares)}
}

func (r *toolRuntime) Definitions() []ai.ToolDefinition {
	return r.registry.definitions()
}

func (r *toolRuntime) Execute(
	ctx context.Context,
	call ai.ToolCall,
	observer ToolEventObserver,
) (ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return ToolResult{}, err
	}
	entry, ok := r.registry.lookup(call.Name)
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
	output, err := r.handler(ctx, execution, nil)
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
	var errorCode pierrors.ErrorCode
	if err != nil {
		errorCode = pierrors.ErrorCodeOf(pierrors.ClassifyTool("tool execute", err))
	}
	if err != nil && len(output.Content) == 0 {
		output.Content = []ai.ContentBlock{ai.TextBlock(toolErrorText(err))}
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
		ErrorCode:  errorCode,
	}
}

func toolErrorText(err error) string {
	var classified *pierrors.Error
	if errors.As(err, &classified) {
		return classified.Err.Error()
	}
	return err.Error()
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
