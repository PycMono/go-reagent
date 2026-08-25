package toolexec

import (
	"context"
	"sync"
	"time"

	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/harness/observability"
)

// Scheduler 负责调度模型在一次回复中发起的多个工具调用，不负责定时任务。
//
// 它按照工具定义中的 ParallelSafe 标记把调用拆成有序批次：连续且支持并发的调用
// 进入同一个并发批次；未标记为并发安全的调用（包括未找到定义的工具）单独形成一个
// 串行屏障，前一批全部结束后才会执行，后一批也必须等待它结束。
//
// 每个并发批次最多同时执行 maxParallel 个调用。虽然并发调用的完成顺序不确定，
// 返回结果仍与 calls 中的原始顺序一致。上下文取消或某一批次执行失败后，不再启动后续批次。
type Scheduler struct {
	// executor 负责查找并实际执行工具。
	executor Executor
	// maxParallel 是单个并发批次允许同时执行的最大工具数；非正数按 1 处理。
	maxParallel int
}

// NewScheduler 创建使用 executor 执行工具的调度器。
func NewScheduler(executor Executor, maxParallel int) *Scheduler {
	return &Scheduler{executor: executor, maxParallel: maxParallel}
}

// SubagentTool 是子代理工具的标记接口：实现它的工具在调度与 Loop
// 策略上享受特殊待遇（如串行屏障）。定义在本包避免反向依赖根包。
type SubagentTool interface {
	ai.Tool
	IsSubagentTool() bool
}

// IsSubagentTool 报告指定工具是否为已注册的子代理工具
// （按 Registry 条目类型断言，不按名字前缀；未知工具返回 false）。
func (s *Scheduler) IsSubagentTool(name string) bool {
	exec, ok := s.executor.(*executor)
	if !ok {
		return false
	}
	toolEntry, ok := exec.registry.lookup(name)
	if !ok {
		return false
	}
	subagent, ok := toolEntry.tool.(SubagentTool)
	return ok && subagent.IsSubagentTool()
}

// Schedule 按照 Scheduler 的批次规则执行 calls。
func (s *Scheduler) Schedule(
	ctx context.Context,
	calls []ai.ToolCall,
	definitions ai.ToolDefinitions,
	observer EventObserver,
) ([]Result, error) {
	parallelSafe := definitions.ParallelSafety()
	knownTools := make(map[string]bool, len(parallelSafe))
	for name := range parallelSafe {
		knownTools[name] = true
	}
	mode := s.Mode(calls, definitions)
	results := make([]Result, len(calls))
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
		if err := s.executeWave(ctx, calls, results, start, end, observer, mode, knownTools); err != nil {
			return nil, err
		}
		start = end
	}
	return results, nil
}

// Mode 返回本批工具调用的执行模式：serial、parallel 或 mixed。
func (s *Scheduler) Mode(calls []ai.ToolCall, definitions ai.ToolDefinitions) string {
	if len(calls) == 0 || s.maxParallel <= 1 {
		return "serial"
	}
	parallelSafe := definitions.ParallelSafety()
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

func (s *Scheduler) executeWave(
	ctx context.Context,
	calls []ai.ToolCall,
	results []Result,
	start int,
	end int,
	observer EventObserver,
	mode string,
	knownTools map[string]bool,
) error {
	limit := s.maxParallel
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
			// 信号量等待只进入 queue_duration Histogram，不创建 Queue Span。
			queuedAt := time.Now()
			select {
			case semaphore <- struct{}{}:
				recordToolQueue(ctx, call, mode, knownTools, nil, time.Since(queuedAt))
			case <-ctx.Done():
				recordToolQueue(ctx, call, mode, knownTools, ctx.Err(), time.Since(queuedAt))
				return
			}
			defer func() { <-semaphore }()
			if ctx.Err() != nil {
				return
			}
			results[index], executionErrors[index-start] = s.executor.Execute(ctx, call, observer)
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

// recordToolQueue 记录排队时延；未注册 Tool 的 Label 固定为 unknown。
func recordToolQueue(
	ctx context.Context,
	call ai.ToolCall,
	mode string,
	knownTools map[string]bool,
	err error,
	wait time.Duration,
) {
	tool := call.Name
	if !knownTools[call.Name] {
		tool = "unknown"
	}
	observability.RecordToolQueueDuration(ctx, tool, observability.ExecutionMode(mode), err, wait)
}
