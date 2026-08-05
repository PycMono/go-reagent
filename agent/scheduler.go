package agent

import (
	"context"
	"errors"
	"sync"

	"github.com/PycMono/go-reagent/ai"
)

// Scheduler partitions tool calls into ordered waves and executes parallel-safe waves with a bound.
type Scheduler struct {
	registry    Registry
	maxParallel int
}

// NewScheduler creates a scheduler backed by registry.
func NewScheduler(registry Registry, maxParallel int) *Scheduler {
	return &Scheduler{registry: registry, maxParallel: maxParallel}
}

// Schedule executes consecutive parallel-safe calls together and treats every other call as a barrier.
func (s *Scheduler) Schedule(
	ctx context.Context,
	calls []ai.ToolCall,
	definitions []ai.ToolDefinition,
	observer ToolEventObserver,
) ([]ToolResult, error) {
	if ctx == nil {
		return nil, errors.New("tool scheduler: context is required")
	}
	if s == nil || s.registry == nil {
		return nil, errors.New("tool scheduler: registry is required")
	}

	parallelSafe := definitionSafety(definitions)
	results := make([]ToolResult, len(calls))
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
		if err := s.executeWave(ctx, calls, results, start, end, observer); err != nil {
			return nil, err
		}
		start = end
	}
	return results, nil
}

// Mode describes whether a batch is serial, parallel, or a mixture of both scheduling forms.
func (s *Scheduler) Mode(calls []ai.ToolCall, definitions []ai.ToolDefinition) string {
	if len(calls) == 0 || s == nil || s.maxParallel <= 1 {
		return "serial"
	}
	parallelSafe := definitionSafety(definitions)
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

func definitionSafety(definitions []ai.ToolDefinition) map[string]bool {
	parallelSafe := make(map[string]bool, len(definitions))
	for _, definition := range definitions {
		parallelSafe[definition.Name] = definition.ParallelSafe
	}
	return parallelSafe
}

func (s *Scheduler) executeWave(
	ctx context.Context,
	calls []ai.ToolCall,
	results []ToolResult,
	start int,
	end int,
	observer ToolEventObserver,
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
			select {
			case semaphore <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-semaphore }()
			if ctx.Err() != nil {
				return
			}
			results[index], executionErrors[index-start] = s.registry.Execute(ctx, call, observer)
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
