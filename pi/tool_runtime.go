package pi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/PycMono/go-reagent/pi/ai"
	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
	"github.com/PycMono/go-reagent/pi/middleware"
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
	Middlewares []middleware.Handler
}

type toolRuntime struct {
	registry *toolRegistry
	handlers []middleware.Handler
}

func NewToolRuntime(options ToolRuntimeOptions) (ToolRuntime, error) {
	registry, err := newToolRegistry(options.Tools)
	if err != nil {
		return nil, err
	}
	registry.freeze()
	return newToolRuntimeFromRegistry(registry, options.Middlewares), nil
}

func newToolRuntimeFromRegistry(registry *toolRegistry, middlewares []middleware.Handler) ToolRuntime {
	// 追加终端 handler 时复制切片，避免污染调用方的底层数组。
	handlers := append([]middleware.Handler{}, middlewares...)
	handlers = append(handlers, middleware.ExecuteTool)
	return &toolRuntime{registry: registry, handlers: handlers}
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
	execution := &middleware.Execution{
		Ctx:          ctx,
		Call:         call,
		Definition:   entry.definition,
		Tool:         entry.tool,
		Observer:     adaptUpdateObserver(observer),
		ValidateArgs: entry.validateArgs,
	}
	execution.Run(r.handlers)
	output, err := execution.Output, execution.Err
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

// adaptUpdateObserver 把 ToolEventObserver 适配为中间件包的 UpdateObserver，
// ToolEvent 的构造留在主包，middleware 包不反向依赖 pi。
func adaptUpdateObserver(observer ToolEventObserver) middleware.UpdateObserver {
	if observer == nil {
		return nil
	}
	return func(ctx context.Context, call ai.ToolCall, update ai.ToolUpdate) {
		observer(ctx, NewToolUpdate(call, update))
	}
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

const (
	maxToolOutputBytes         = 50 * 1024
	toolOutputTruncationMarker = "\n[output truncated]"
)

func limitToolOutput(output ai.ToolOutput) ai.ToolOutput {
	limited, truncated := limitContent(output.Content)
	output.Content = limited
	if truncated {
		output.Details = withTruncationDetail(output.Details)
	}
	return output
}

func limitContent(content []ai.ContentBlock) ([]ai.ContentBlock, bool) {
	remaining := maxToolOutputBytes
	limited := make([]ai.ContentBlock, 0, len(content)+1)
	truncated := false
	for _, block := range content {
		if block.Type != ai.ContentTypeText {
			limited = append(limited, block)
			continue
		}
		text := strings.ToValidUTF8(block.Text, "�")
		if len(text) <= remaining {
			block.Text = text
			limited = append(limited, block)
			remaining -= len(text)
			continue
		}
		cut := remaining
		for cut > 0 && !utf8.ValidString(text[:cut]) {
			cut--
		}
		block.Text = text[:cut]
		limited = append(limited, block)
		truncated = true
		break
	}
	if !truncated && len(limited) < len(content) {
		truncated = true
	}
	if truncated {
		limited = append(limited, ai.TextBlock(toolOutputTruncationMarker))
	}
	return limited, truncated
}

func withTruncationDetail(details any) any {
	if existing, ok := details.(map[string]any); ok {
		cloned := make(map[string]any, len(existing)+1)
		for key, value := range existing {
			cloned[key] = value
		}
		cloned["truncated"] = true
		return cloned
	}
	if details == nil {
		return map[string]any{"truncated": true}
	}
	return map[string]any{"tool_details": details, "truncated": true}
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
