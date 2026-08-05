package agent_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/PycMono/go-reagent/pi/agent"
	"github.com/PycMono/go-reagent/pi/ai"
)

type schedulerRegistry struct {
	started  chan ai.ToolCall
	finished chan ai.ToolCall
	gates    map[string]chan struct{}
	errors   map[string]error

	mu        sync.Mutex
	active    int
	maxActive int
	calls     []ai.ToolCall
}

func newSchedulerRegistry(calls []ai.ToolCall) *schedulerRegistry {
	gates := make(map[string]chan struct{}, len(calls))
	for _, call := range calls {
		gates[call.ID] = make(chan struct{})
	}
	return &schedulerRegistry{
		started:  make(chan ai.ToolCall, len(calls)),
		finished: make(chan ai.ToolCall, len(calls)),
		gates:    gates,
		errors:   make(map[string]error),
	}
}

func (r *schedulerRegistry) GetAvailableTools() []ai.ToolDefinition { return nil }

func (r *schedulerRegistry) Execute(
	ctx context.Context,
	call ai.ToolCall,
	observer agent.ToolEventObserver,
) (agent.ToolResult, error) {
	r.mu.Lock()
	r.active++
	if r.active > r.maxActive {
		r.maxActive = r.active
	}
	r.calls = append(r.calls, call)
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.active--
		r.mu.Unlock()
	}()

	r.started <- call
	select {
	case <-r.gates[call.ID]:
	case <-ctx.Done():
		return agent.ToolResult{}, ctx.Err()
	}
	r.finished <- call
	return agent.ToolResult{
		ToolCallID: call.ID,
		ToolName:   call.Name,
		Content:    []ai.ContentBlock{ai.TextBlock("result-" + call.ID)},
	}, r.errors[call.ID]
}

func (r *schedulerRegistry) closeAll() {
	for _, gate := range r.gates {
		select {
		case <-gate:
		default:
			close(gate)
		}
	}
}

func TestToolSchedulerRunsSafeCallsInWavesAroundExclusiveBarriers(t *testing.T) {
	calls := []ai.ToolCall{
		{ID: "read-1", Name: "read-a"},
		{ID: "read-2", Name: "read-b"},
		{ID: "write", Name: "write"},
		{ID: "read-3", Name: "read-c"},
		{ID: "unknown", Name: "missing"},
	}
	definitions := []ai.ToolDefinition{
		{Name: "read-a", ParallelSafe: true},
		{Name: "read-b", ParallelSafe: true},
		{Name: "write"},
		{Name: "read-c", ParallelSafe: true},
	}
	registry := newSchedulerRegistry(calls)
	t.Cleanup(registry.closeAll)
	scheduler := agent.NewScheduler(registry, 4)

	done := make(chan struct {
		results []agent.ToolResult
		err     error
	}, 1)
	go func() {
		results, err := scheduler.Schedule(context.Background(), calls, definitions, nil)
		done <- struct {
			results []agent.ToolResult
			err     error
		}{results: results, err: err}
	}()

	first := requireSchedulerStarted(t, registry.started)
	second := requireSchedulerStarted(t, registry.started)
	if got := map[string]bool{first.ID: true, second.ID: true}; !got["read-1"] || !got["read-2"] {
		t.Fatalf("first wave = %#v, want read-1 and read-2", got)
	}
	requireNoSchedulerStart(t, registry.started)
	close(registry.gates[first.ID])
	close(registry.gates[second.ID])
	requireSchedulerFinished(t, registry.finished)
	requireSchedulerFinished(t, registry.finished)

	barrier := requireSchedulerStarted(t, registry.started)
	if barrier.ID != "write" {
		t.Fatalf("barrier = %q, want write", barrier.ID)
	}
	requireNoSchedulerStart(t, registry.started)
	close(registry.gates[barrier.ID])
	requireSchedulerFinished(t, registry.finished)

	thirdRead := requireSchedulerStarted(t, registry.started)
	if thirdRead.ID != "read-3" {
		t.Fatalf("third wave = %q, want read-3", thirdRead.ID)
	}
	requireNoSchedulerStart(t, registry.started)
	close(registry.gates[thirdRead.ID])
	requireSchedulerFinished(t, registry.finished)

	unknown := requireSchedulerStarted(t, registry.started)
	if unknown.ID != "unknown" {
		t.Fatalf("unknown barrier = %q, want unknown", unknown.ID)
	}
	close(registry.gates[unknown.ID])
	requireSchedulerFinished(t, registry.finished)

	result := <-done
	if result.err != nil {
		t.Fatalf("Schedule() error = %v", result.err)
	}
	for index, call := range calls {
		if result.results[index].ToolCallID != call.ID {
			t.Fatalf("results[%d].ToolCallID = %q, want %q", index, result.results[index].ToolCallID, call.ID)
		}
	}
	if got := scheduler.Mode(calls, definitions); got != "mixed" {
		t.Fatalf("Mode() = %q, want mixed", got)
	}
}

func TestToolSchedulerBoundsConcurrencyAndKeepsInputResultOrder(t *testing.T) {
	calls := []ai.ToolCall{
		{ID: "one", Name: "read"},
		{ID: "two", Name: "read"},
		{ID: "three", Name: "read"},
		{ID: "four", Name: "read"},
	}
	definitions := []ai.ToolDefinition{{Name: "read", ParallelSafe: true}}
	registry := newSchedulerRegistry(calls)
	t.Cleanup(registry.closeAll)
	scheduler := agent.NewScheduler(registry, 2)

	done := make(chan struct {
		results []agent.ToolResult
		err     error
	}, 1)
	go func() {
		results, err := scheduler.Schedule(context.Background(), calls, definitions, nil)
		done <- struct {
			results []agent.ToolResult
			err     error
		}{results: results, err: err}
	}()

	first := requireSchedulerStarted(t, registry.started)
	second := requireSchedulerStarted(t, registry.started)
	requireNoSchedulerStart(t, registry.started)
	close(registry.gates[second.ID])
	requireSchedulerFinished(t, registry.finished)
	third := requireSchedulerStarted(t, registry.started)
	close(registry.gates[third.ID])
	requireSchedulerFinished(t, registry.finished)
	fourth := requireSchedulerStarted(t, registry.started)
	close(registry.gates[fourth.ID])
	requireSchedulerFinished(t, registry.finished)
	close(registry.gates[first.ID])
	requireSchedulerFinished(t, registry.finished)

	result := <-done
	if result.err != nil {
		t.Fatalf("Schedule() error = %v", result.err)
	}
	for index, call := range calls {
		if result.results[index].ToolCallID != call.ID {
			t.Fatalf("results[%d].ToolCallID = %q, want %q", index, result.results[index].ToolCallID, call.ID)
		}
	}
	registry.mu.Lock()
	maxActive := registry.maxActive
	registry.mu.Unlock()
	if maxActive != 2 {
		t.Fatalf("maximum active calls = %d, want 2", maxActive)
	}
	if got := scheduler.Mode(calls, definitions); got != "parallel" {
		t.Fatalf("Mode() = %q, want parallel", got)
	}
}

func TestToolSchedulerUsesSerialFallbackForNonpositiveLimit(t *testing.T) {
	calls := []ai.ToolCall{{ID: "one", Name: "read"}, {ID: "two", Name: "read"}}
	definitions := []ai.ToolDefinition{{Name: "read", ParallelSafe: true}}
	registry := newSchedulerRegistry(calls)
	t.Cleanup(registry.closeAll)
	scheduler := agent.NewScheduler(registry, 0)

	done := make(chan error, 1)
	go func() {
		_, err := scheduler.Schedule(context.Background(), calls, definitions, nil)
		done <- err
	}()
	first := requireSchedulerStarted(t, registry.started)
	requireNoSchedulerStart(t, registry.started)
	close(registry.gates[first.ID])
	requireSchedulerFinished(t, registry.finished)
	second := requireSchedulerStarted(t, registry.started)
	close(registry.gates[second.ID])
	requireSchedulerFinished(t, registry.finished)
	if err := <-done; err != nil {
		t.Fatalf("Schedule() error = %v", err)
	}
	if got := scheduler.Mode(calls, definitions); got != "serial" {
		t.Fatalf("Mode() = %q, want serial", got)
	}
}

func TestToolSchedulerCancellationPreventsLaterWaves(t *testing.T) {
	calls := []ai.ToolCall{{ID: "write", Name: "write"}, {ID: "read", Name: "read"}}
	definitions := []ai.ToolDefinition{{Name: "write"}, {Name: "read", ParallelSafe: true}}
	registry := newSchedulerRegistry(calls)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		registry.closeAll()
	})
	scheduler := agent.NewScheduler(registry, 2)

	done := make(chan error, 1)
	go func() {
		_, err := scheduler.Schedule(ctx, calls, definitions, nil)
		done <- err
	}()
	started := requireSchedulerStarted(t, registry.started)
	if started.ID != "write" {
		t.Fatalf("first call = %q, want write", started.ID)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Schedule() error = %v, want context.Canceled", err)
	}
	requireNoSchedulerStart(t, registry.started)
}

func TestToolSchedulerWaitsForStartedSiblingsBeforeReturningFirstError(t *testing.T) {
	wantErr := errors.New("stop scheduling")
	calls := []ai.ToolCall{{ID: "one", Name: "read"}, {ID: "two", Name: "read"}}
	definitions := []ai.ToolDefinition{{Name: "read", ParallelSafe: true}}
	registry := newSchedulerRegistry(calls)
	registry.errors["one"] = wantErr
	t.Cleanup(registry.closeAll)
	scheduler := agent.NewScheduler(registry, 2)

	done := make(chan error, 1)
	go func() {
		_, err := scheduler.Schedule(context.Background(), calls, definitions, nil)
		done <- err
	}()
	requireSchedulerStarted(t, registry.started)
	requireSchedulerStarted(t, registry.started)
	close(registry.gates["one"])
	requireSchedulerFinished(t, registry.finished)
	select {
	case err := <-done:
		t.Fatalf("Schedule() returned %v before sibling exited", err)
	case <-time.After(30 * time.Millisecond):
	}
	close(registry.gates["two"])
	requireSchedulerFinished(t, registry.finished)
	if err := <-done; !errors.Is(err, wantErr) {
		t.Fatalf("Schedule() error = %v, want %v", err, wantErr)
	}
}

func requireSchedulerStarted(t *testing.T, started <-chan ai.ToolCall) ai.ToolCall {
	t.Helper()
	select {
	case call := <-started:
		return call
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for tool to start")
		return ai.ToolCall{}
	}
}

func requireNoSchedulerStart(t *testing.T, started <-chan ai.ToolCall) {
	t.Helper()
	select {
	case call := <-started:
		t.Fatalf("unexpected tool start: %#v", call)
	case <-time.After(30 * time.Millisecond):
	}
}

func requireSchedulerFinished(t *testing.T, finished <-chan ai.ToolCall) ai.ToolCall {
	t.Helper()
	select {
	case call := <-finished:
		return call
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for tool to finish")
		return ai.ToolCall{}
	}
}
