// Package toolexec 是 Tool 执行域：Registry 管理工具注册生命周期，Runtime
// 经中间件链执行单个调用并负责批量调度，Event 表达执行生命周期。
// 命名对齐 pi.dev 的 tool execution 词汇。
package toolexec

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/PycMono/go-reagent/pi/ai"
	pierrors "github.com/PycMono/go-reagent/pi/errors"
	"github.com/PycMono/go-reagent/pi/middleware"
)

// Runtime 负责注册工具的单次执行与有界批量调度。
type Runtime struct {
	registry    *Registry
	handlers    []middleware.Handler
	maxParallel int
}

// NewRuntime 创建共享 registry、固定中间件快照和并发上限的工具运行时。
func NewRuntime(registry *Registry, middlewares []middleware.Handler, maxParallel int) *Runtime {
	// 追加终端 handler 时复制切片，避免污染调用方的底层数组。
	handlers := append([]middleware.Handler{}, middlewares...)
	handlers = append(handlers, middleware.ExecuteTool)
	return &Runtime{registry: registry, handlers: handlers, maxParallel: maxParallel}
}

// Definitions 返回 Registry 当前的工具定义快照。
func (runtime *Runtime) Definitions() []ai.ToolDefinition {
	return runtime.registry.Definitions()
}

// Execute 查找并经中间件链执行单个工具调用。
func (runtime *Runtime) Execute(
	ctx context.Context,
	call ai.ToolCall,
	observer EventObserver,
) (Event, error) {
	if err := ctx.Err(); err != nil {
		return Event{}, err
	}
	toolEntry, ok := runtime.registry.lookup(call.Name) // 查找工具
	if !ok {
		return normalizeEndEvent(call, ai.ToolOutput{}, fmt.Errorf("tool %q is not registered", call.Name)), nil
	}

	var updateObserver middleware.UpdateObserver
	if observer != nil {
		observer(ctx, NewStartEvent(call))
		updateObserver = func(ctx context.Context, call ai.ToolCall, update ai.ToolUpdate) {
			observer(ctx, NewUpdateEvent(call, update))
		}
	}

	//→ execution.Run(handlers)
	//→ Tracing
	//→ PanicRecovery
	//→ SchemaValidation
	//→ Logging
	//→ EventForwarding
	//→ 可选 Permission / Retry / Timeout
	//→ ExecuteTool
	//→ e.Tool.Execute(...)

	execution := &middleware.Execution{
		Ctx:          ctx,
		Call:         call,
		Definition:   toolEntry.definition,
		Tool:         toolEntry.tool,
		Observer:     updateObserver,
		ValidateArgs: toolEntry.validateArgs,
	}
	execution.Run(runtime.handlers) // 调用中间件
	output, err := execution.Output, execution.Err
	if contextErr := ctx.Err(); contextErr != nil {
		err = contextErr
	}

	event := normalizeEndEvent(call, output, err)
	if observer != nil {
		observer(ctx, event)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return event, err
	}

	return event, nil
}

// SubagentTool 是子代理工具的标记接口：实现它的工具在调度与 Loop
// 策略上享受特殊待遇（如单批配额）。定义在本包避免反向依赖根包。
type SubagentTool interface {
	ai.Tool
	IsSubagentTool() bool
}

// IsSubagentTool 报告指定工具是否为已注册的子代理工具
// （按 Registry 条目类型断言，不按名字前缀；未知工具返回 false）。
func (runtime *Runtime) IsSubagentTool(name string) bool {
	toolEntry, ok := runtime.registry.lookup(name)
	if !ok {
		return false
	}
	subagent, ok := toolEntry.tool.(SubagentTool)
	return ok && subagent.IsSubagentTool()
}

// Schedule 按 ParallelSafe 把 calls 拆成有序批次：连续安全调用并发执行，
// 非安全或未知调用形成串行屏障。每批并发量受 maxParallel 限制，返回结果
// 始终保持 calls 的原始顺序；取消或某批基础设施失败后不再启动后续批次。
func (runtime *Runtime) Schedule(
	ctx context.Context,
	calls []ai.ToolCall,
	availableTools ai.ToolDefinitions,
	observer EventObserver,
) ([]Event, error) {
	parallelSafe := availableTools.ParallelSafety()
	results := make([]Event, len(calls))
	for start := 0; start < len(calls); {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		end := start + 1
		if parallelSafe[calls[start].Name] {
			for end < len(calls) && parallelSafe[calls[end].Name] {
				end++
			}
		}
		if err := runtime.executeWave(ctx, calls, results, start, end, observer); err != nil {
			return nil, err
		}
		start = end
	}
	return results, nil
}

// Mode 返回本批工具调用的执行模式：serial、parallel 或 mixed。
func (runtime *Runtime) Mode(calls []ai.ToolCall, availableTools ai.ToolDefinitions) string {
	if len(calls) == 0 || runtime.maxParallel <= 1 {
		return "serial"
	}

	parallelSafe := availableTools.ParallelSafety()
	hasParallelWave := false
	hasSerialCall := false
	for start := 0; start < len(calls); {
		if !parallelSafe[calls[start].Name] {
			hasSerialCall = true
			start++
			continue
		}
		end := start + 1
		for end < len(calls) && parallelSafe[calls[end].Name] {
			end++
		}
		if end-start > 1 {
			hasParallelWave = true
		} else {
			hasSerialCall = true
		}
		start = end
	}

	if hasParallelWave && hasSerialCall {
		return "mixed"
	}
	if hasParallelWave {
		return "parallel"
	}
	return "serial"
}

func (runtime *Runtime) executeWave(
	ctx context.Context,
	calls []ai.ToolCall,
	results []Event,
	start int,
	end int,
	observer EventObserver,
) error {
	limit := runtime.maxParallel
	if limit <= 0 {
		limit = 1
	}
	if waveSize := end - start; limit > waveSize {
		limit = waveSize
	}

	semaphore := make(chan struct{}, limit)
	executionErrors := make([]error, end-start)
	var waitGroup sync.WaitGroup
	for index := start; index < end; index++ {
		call := calls[index]
		waitGroup.Add(1)
		go func(index int, call ai.ToolCall) {
			defer waitGroup.Done()
			select {
			case semaphore <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-semaphore }()
			if ctx.Err() != nil {
				return
			}
			results[index], executionErrors[index-start] = runtime.Execute(ctx, call, observer)
		}(index, call)
	}
	waitGroup.Wait()

	for _, err := range executionErrors {
		if err != nil {
			return err
		}
	}
	return ctx.Err()
}

func normalizeEndEvent(call ai.ToolCall, output ai.ToolOutput, err error) Event {
	isError := err != nil
	var errorCode pierrors.ErrorCode
	if isError {
		errorCode = pierrors.ErrorCodeOf(pierrors.ClassifyTool("tool execute", err))
	}
	if len(output.Content) == 0 {
		text := "(no output)"
		if isError {
			text = err.Error()
			var classified *pierrors.Error
			if errors.As(err, &classified) {
				text = classified.Err.Error()
			}
		}
		output.Content = ai.ContentBlocks{ai.TextBlock(text)}
	}
	output = output.LimitText(maxToolOutputBytes)
	return NewEndEvent(call, output, isError, errorCode)
}

const maxToolOutputBytes = 50 * 1024
