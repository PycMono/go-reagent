// Package toolexec 是 Tool 执行域：注册（Registry）、经中间件链执行单个
// 调用（Executor）、批量调度（Scheduler）与执行生命周期事件（Event）。
// 命名对齐 pi.dev 的 tool execution 词汇。
package toolexec

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/PycMono/go-reagent/pi/ai"
	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
	"github.com/PycMono/go-reagent/pi/middleware"
)

// EventObserver 接收 Tool 执行的生命周期事件。
type EventObserver func(context.Context, Event)

// Executor 查找并经中间件链执行单个 Tool 调用。
type Executor interface {
	Definitions() []ai.ToolDefinition
	Execute(context.Context, ai.ToolCall, EventObserver) (Result, error)
}

// ExecutorOptions contains the immutable tool and middleware snapshot.
type ExecutorOptions struct {
	Tools       []ai.Tool
	Middlewares []middleware.Handler
}

type executor struct {
	registry *Registry
	handlers []middleware.Handler
}

func NewExecutor(options ExecutorOptions) (Executor, error) {
	registry, err := NewRegistry(options.Tools)
	if err != nil {
		return nil, err
	}
	registry.Freeze()
	return NewExecutorFromRegistry(registry, options.Middlewares), nil
}

func NewExecutorFromRegistry(registry *Registry, middlewares []middleware.Handler) Executor {
	// 追加终端 handler 时复制切片，避免污染调用方的底层数组。
	handlers := append([]middleware.Handler{}, middlewares...)
	handlers = append(handlers, middleware.ExecuteTool)
	return &executor{registry: registry, handlers: handlers}
}

func (e *executor) Definitions() []ai.ToolDefinition {
	return e.registry.Definitions()
}

func (e *executor) Execute(
	ctx context.Context,
	call ai.ToolCall,
	observer EventObserver,
) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	toolEntry, ok := e.registry.lookup(call.Name)
	if !ok {
		return errorResult(call, fmt.Errorf("tool %q is not registered", call.Name)), nil
	}
	observe(ctx, observer, NewStartEvent(call))
	execution := &middleware.Execution{
		Ctx:          ctx,
		Call:         call,
		Definition:   toolEntry.definition,
		Tool:         toolEntry.tool,
		Observer:     adaptUpdateObserver(observer),
		ValidateArgs: toolEntry.validateArgs,
	}
	execution.Run(e.handlers)
	output, err := execution.Output, execution.Err
	if contextErr := ctx.Err(); contextErr != nil {
		err = contextErr
	}
	result := normalizeResult(call, output, err)
	observe(ctx, observer, NewEndEvent(call, result))
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return result, err
	}
	return result, nil
}

// adaptUpdateObserver 把 EventObserver 适配为中间件包的 UpdateObserver。
func adaptUpdateObserver(observer EventObserver) middleware.UpdateObserver {
	if observer == nil {
		return nil
	}
	return func(ctx context.Context, call ai.ToolCall, update ai.ToolUpdate) {
		observer(ctx, NewUpdateEvent(call, update))
	}
}

func observe(ctx context.Context, observer EventObserver, event Event) {
	if observer != nil {
		observer(ctx, event)
	}
}

func normalizeResult(call ai.ToolCall, output ai.ToolOutput, err error) Result {
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
	return Result{
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

func errorResult(call ai.ToolCall, err error) Result {
	return normalizeResult(call, ai.ToolOutput{}, err)
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
