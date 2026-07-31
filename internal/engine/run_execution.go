package engine

import (
	"context"
	"fmt"
	"sync"

	logsdk "github.com/PycMono/go-logger-sdk"
	"github.com/PycMono/go-reagent/internal/schema"
)

// actionExecutionMode 检查一批工具调用能否并发执行，返回 serial、parallel 或 mixed，
// 供 Run 记录本轮 Action 采用的调度模式；它本身不执行工具。
func (e *AgentEngine) actionExecutionMode(
	calls []schema.ToolCall,
	definitions []schema.ToolDefinition,
) string {
	if len(calls) == 0 || e.MaxParallelTools <= 1 {
		return "serial"
	}
	parallelSafe := make(map[string]bool, len(definitions))
	for _, definition := range definitions {
		parallelSafe[definition.Name] = definition.ParallelSafe
	}

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

// executeToolCalls 执行模型在一次 Action 中请求的全部工具调用。
// 非并发安全工具串行执行，连续的并发安全工具组成并行波次；即使并发完成顺序不同，
// 返回的 Observation 仍与模型给出的工具调用顺序一致。
func (e *AgentEngine) executeToolCalls(
	ctx context.Context,
	calls []schema.ToolCall,
	definitions []schema.ToolDefinition,
	reporter Reporter,
) ([]schema.Message, error) {
	parallelSafe := make(map[string]bool, len(definitions))
	for _, definition := range definitions {
		parallelSafe[definition.Name] = definition.ParallelSafe
	}

	observations := make([]schema.Message, len(calls))
	for start := 0; start < len(calls); {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		if !parallelSafe[calls[start].Name] {
			observation, err := e.executeToolCall(ctx, start, calls[start], false, reporter)
			if err != nil {
				return nil, err
			}
			observations[start] = observation
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			start++
			continue
		}

		end := start + 1
		for end < len(calls) && parallelSafe[calls[end].Name] {
			end++
		}
		if err := e.executeParallelWave(ctx, calls, observations, start, end, reporter); err != nil {
			return nil, err
		}
		start = end
	}

	return observations, nil
}

// executeParallelWave 并发执行 calls[start:end] 中的工具调用，使用信号量把实际并发数
// 限制在 MaxParallelTools 以内，并等待这一波工具全部结束后再返回。
func (e *AgentEngine) executeParallelWave(
	ctx context.Context,
	calls []schema.ToolCall,
	observations []schema.Message,
	start int,
	end int,
	reporter Reporter,
) error {
	limit := e.MaxParallelTools
	if limit <= 0 {
		limit = 1
	}
	if waveSize := end - start; limit > waveSize {
		limit = waveSize
	}
	parallel := limit > 1

	semaphore := make(chan struct{}, limit)
	executionErrors := make([]error, end-start)
	var waitGroup sync.WaitGroup
	for index := start; index < end; index++ {
		call := calls[index]
		waitGroup.Add(1)
		go func(index int, call schema.ToolCall) {
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

			observation, err := e.executeToolCall(ctx, index, call, parallel, reporter)
			observations[index] = observation
			executionErrors[index-start] = err
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

// executeToolCall 通过 Registry 执行一个工具，在执行前后通知 Reporter，
// 再把工具输出转换成带 ToolCallID 的 Observation 消息供模型下一轮使用。
func (e *AgentEngine) executeToolCall(
	ctx context.Context,
	index int,
	call schema.ToolCall,
	parallel bool,
	reporter Reporter,
) (schema.Message, error) {
	commonFields := []logsdk.Fields{
		logsdk.Any("component", "engine"),
		logsdk.Any("tool_index", index),
		logsdk.Any("tool", call.Name),
		logsdk.Any("tool_call_id", call.ID),
		logsdk.Any("parallel", parallel),
	}
	mode := "串行"
	if parallel {
		mode = "并行"
	}
	logsdk.Info(ctx,
		fmt.Sprintf("  -> [Go-%d] 🛠️ 触发%s执行", index, mode),
		commonFields...,
	)
	observer := func(ctx context.Context, event schema.ToolEvent) {
		if reporter != nil {
			reporter.Report(ctx, schema.NewAgentToolEvent(event))
		}
	}
	result, executeErr := e.registry.Execute(ctx, call, observer)
	resultText, err := schema.TextContent(result.Content)
	if err != nil {
		resultText = fmt.Sprintf("tool result content error: %v", err)
	}
	if result.IsError {
		logsdk.Error(ctx,
			fmt.Sprintf("  -> [Go-%d] ❌ 工具执行失败", index),
			append(commonFields, logsdk.Any("result_bytes", len(resultText)))...,
		)
	} else {
		logsdk.Info(ctx,
			fmt.Sprintf("  -> [Go-%d] ✅ 工具执行成功", index),
			append(commonFields, logsdk.Any("result_bytes", len(resultText)))...,
		)
	}
	return schema.Message{
		Role:       schema.RoleTool,
		Content:    result.Content,
		ToolCallID: call.ID,
		ToolName:   call.Name,
		IsError:    result.IsError,
	}, executeErr
}
