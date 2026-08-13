package test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
)

type schedulerToolRuntime struct {
	started  chan ai.ToolCall
	finished chan ai.ToolCall
	gates    map[string]chan struct{}
	errors   map[string]error

	mu        sync.Mutex
	active    int
	maxActive int
	calls     []ai.ToolCall
}

func newSchedulerToolRuntime(calls []ai.ToolCall) *schedulerToolRuntime {
	gates := make(map[string]chan struct{}, len(calls))
	for _, call := range calls {
		gates[call.ID] = make(chan struct{})
	}
	return &schedulerToolRuntime{
		started:  make(chan ai.ToolCall, len(calls)),
		finished: make(chan ai.ToolCall, len(calls)),
		gates:    gates,
		errors:   make(map[string]error),
	}
}

func (r *schedulerToolRuntime) Definitions() []ai.ToolDefinition { return nil }

func (r *schedulerToolRuntime) Execute(
	ctx context.Context,
	call ai.ToolCall,
	observer pi.ToolEventObserver,
) (pi.ToolResult, error) {
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
		return pi.ToolResult{}, ctx.Err()
	}
	r.finished <- call
	return pi.ToolResult{
		ToolCallID: call.ID,
		ToolName:   call.Name,
		Content:    []ai.ContentBlock{ai.TextBlock("result-" + call.ID)},
	}, r.errors[call.ID]
}

func (r *schedulerToolRuntime) closeAll() {
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
	toolRuntime := newSchedulerToolRuntime(calls)
	t.Cleanup(toolRuntime.closeAll)
	scheduler := pi.NewScheduler(toolRuntime, 4)

	done := make(chan struct {
		results []pi.ToolResult
		err     error
	}, 1)
	go func() {
		results, err := scheduler.Schedule(context.Background(), calls, definitions, nil)
		done <- struct {
			results []pi.ToolResult
			err     error
		}{results: results, err: err}
	}()

	first := requireSchedulerStarted(t, toolRuntime.started)
	second := requireSchedulerStarted(t, toolRuntime.started)
	if got := map[string]bool{first.ID: true, second.ID: true}; !got["read-1"] || !got["read-2"] {
		t.Fatalf("first wave = %#v, want read-1 and read-2", got)
	}
	requireNoSchedulerStart(t, toolRuntime.started)
	close(toolRuntime.gates[first.ID])
	close(toolRuntime.gates[second.ID])
	requireSchedulerFinished(t, toolRuntime.finished)
	requireSchedulerFinished(t, toolRuntime.finished)

	barrier := requireSchedulerStarted(t, toolRuntime.started)
	if barrier.ID != "write" {
		t.Fatalf("barrier = %q, want write", barrier.ID)
	}
	requireNoSchedulerStart(t, toolRuntime.started)
	close(toolRuntime.gates[barrier.ID])
	requireSchedulerFinished(t, toolRuntime.finished)

	thirdRead := requireSchedulerStarted(t, toolRuntime.started)
	if thirdRead.ID != "read-3" {
		t.Fatalf("third wave = %q, want read-3", thirdRead.ID)
	}
	requireNoSchedulerStart(t, toolRuntime.started)
	close(toolRuntime.gates[thirdRead.ID])
	requireSchedulerFinished(t, toolRuntime.finished)

	unknown := requireSchedulerStarted(t, toolRuntime.started)
	if unknown.ID != "unknown" {
		t.Fatalf("unknown barrier = %q, want unknown", unknown.ID)
	}
	close(toolRuntime.gates[unknown.ID])
	requireSchedulerFinished(t, toolRuntime.finished)

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
	toolRuntime := newSchedulerToolRuntime(calls)
	t.Cleanup(toolRuntime.closeAll)
	scheduler := pi.NewScheduler(toolRuntime, 2)

	done := make(chan struct {
		results []pi.ToolResult
		err     error
	}, 1)
	go func() {
		results, err := scheduler.Schedule(context.Background(), calls, definitions, nil)
		done <- struct {
			results []pi.ToolResult
			err     error
		}{results: results, err: err}
	}()

	first := requireSchedulerStarted(t, toolRuntime.started)
	second := requireSchedulerStarted(t, toolRuntime.started)
	requireNoSchedulerStart(t, toolRuntime.started)
	close(toolRuntime.gates[second.ID])
	requireSchedulerFinished(t, toolRuntime.finished)
	third := requireSchedulerStarted(t, toolRuntime.started)
	close(toolRuntime.gates[third.ID])
	requireSchedulerFinished(t, toolRuntime.finished)
	fourth := requireSchedulerStarted(t, toolRuntime.started)
	close(toolRuntime.gates[fourth.ID])
	requireSchedulerFinished(t, toolRuntime.finished)
	close(toolRuntime.gates[first.ID])
	requireSchedulerFinished(t, toolRuntime.finished)

	result := <-done
	if result.err != nil {
		t.Fatalf("Schedule() error = %v", result.err)
	}
	for index, call := range calls {
		if result.results[index].ToolCallID != call.ID {
			t.Fatalf("results[%d].ToolCallID = %q, want %q", index, result.results[index].ToolCallID, call.ID)
		}
	}
	toolRuntime.mu.Lock()
	maxActive := toolRuntime.maxActive
	toolRuntime.mu.Unlock()
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
	toolRuntime := newSchedulerToolRuntime(calls)
	t.Cleanup(toolRuntime.closeAll)
	scheduler := pi.NewScheduler(toolRuntime, 0)

	done := make(chan error, 1)
	go func() {
		_, err := scheduler.Schedule(context.Background(), calls, definitions, nil)
		done <- err
	}()
	first := requireSchedulerStarted(t, toolRuntime.started)
	requireNoSchedulerStart(t, toolRuntime.started)
	close(toolRuntime.gates[first.ID])
	requireSchedulerFinished(t, toolRuntime.finished)
	second := requireSchedulerStarted(t, toolRuntime.started)
	close(toolRuntime.gates[second.ID])
	requireSchedulerFinished(t, toolRuntime.finished)
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
	toolRuntime := newSchedulerToolRuntime(calls)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		toolRuntime.closeAll()
	})
	scheduler := pi.NewScheduler(toolRuntime, 2)

	done := make(chan error, 1)
	go func() {
		_, err := scheduler.Schedule(ctx, calls, definitions, nil)
		done <- err
	}()
	started := requireSchedulerStarted(t, toolRuntime.started)
	if started.ID != "write" {
		t.Fatalf("first call = %q, want write", started.ID)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Schedule() error = %v, want context.Canceled", err)
	}
	requireNoSchedulerStart(t, toolRuntime.started)
}

func TestToolSchedulerWaitsForStartedSiblingsBeforeReturningFirstError(t *testing.T) {
	wantErr := errors.New("stop scheduling")
	calls := []ai.ToolCall{{ID: "one", Name: "read"}, {ID: "two", Name: "read"}}
	definitions := []ai.ToolDefinition{{Name: "read", ParallelSafe: true}}
	toolRuntime := newSchedulerToolRuntime(calls)
	toolRuntime.errors["one"] = wantErr
	t.Cleanup(toolRuntime.closeAll)
	scheduler := pi.NewScheduler(toolRuntime, 2)

	done := make(chan error, 1)
	go func() {
		_, err := scheduler.Schedule(context.Background(), calls, definitions, nil)
		done <- err
	}()
	requireSchedulerStarted(t, toolRuntime.started)
	requireSchedulerStarted(t, toolRuntime.started)
	close(toolRuntime.gates["one"])
	requireSchedulerFinished(t, toolRuntime.finished)
	select {
	case err := <-done:
		t.Fatalf("Schedule() returned %v before sibling exited", err)
	case <-time.After(30 * time.Millisecond):
	}
	close(toolRuntime.gates["two"])
	requireSchedulerFinished(t, toolRuntime.finished)
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
