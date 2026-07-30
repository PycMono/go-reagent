# Tool Execution Scheduler Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Execute explicitly safe ToolCalls in bounded parallel waves while treating every unsafe or unknown tool as an ordered execution barrier.

**Architecture:** Add conservative `ParallelSafe` scheduling metadata to `ToolDefinition`, mark only `read_file` safe, and let AgentEngine partition each validated ToolCall list into consecutive safe waves separated by exclusive calls. Safe waves use `sync.WaitGroup` plus a semaphore capped by `MaxParallelTools`; every Observation is stored at its original ToolCall index before deterministic aggregation.

**Tech Stack:** Go 1.26 standard library (`context`, `sync`, channels), existing Provider/Registry/Schema abstractions, Go `testing`, `go vet`, and race detector.

## Global Constraints

- Internal imports remain `github.com/PycMono/go-reagent/internal/...`.
- Provider and Registry source files remain unchanged.
- Provider requests continue to expose only tool name, description, and input Schema.
- `ParallelSafe` defaults to false; unknown and unmarked tools are exclusive barriers.
- Only `read_file` is marked parallel-safe in the current repository.
- `NewAgentEngine` keeps its existing signature and defaults `MaxParallelTools` to 4.
- `MaxParallelTools <= 0` means serial execution, never unlimited execution.
- ToolCall IDs are validated as a complete batch before any tool starts.
- Observation order always matches ToolCall order, regardless of physical completion order.
- A tool error does not cancel siblings; parent Context cancellation stops later work.
- No third-party dependency is added.
- The workspace has no Git metadata, so commit steps are unavailable; each task ends with a fresh verification checkpoint.

## File Map

- Modify `internal/schema/message.go`: add harness scheduling metadata.
- Modify `internal/schema/message_test.go`: protect true/false JSON behavior.
- Modify `internal/tools/read_file.go`: declare the tool safe for concurrent calls.
- Modify `internal/tools/read_file_test.go`: protect the safety declaration.
- Modify `internal/engine/loop.go`: add bounded wave scheduling and stable aggregation.
- Modify `internal/engine/loop_test.go`: make fakes race-safe and test concurrency, bounds, barriers, errors, order, and cancellation.
- Modify `cmd/reagent/main.go`: turn on Thinking and provide a real parallel-read task.
- Modify `README.md`: document scheduling semantics and current defaults.

---

### Task 1: Add conservative scheduling metadata

**Files:**
- Modify: `internal/schema/message_test.go`
- Modify: `internal/schema/message.go`
- Modify: `internal/tools/read_file_test.go`
- Modify: `internal/tools/read_file.go`

**Interfaces:**
- Produces: `ToolDefinition.ParallelSafe bool` with JSON name `parallel_safe` and `omitempty`.
- Preserves: all existing ToolDefinition fields and Provider conversion behavior.
- Produces: `ReadFileTool.Definition().ParallelSafe == true`.

- [ ] **Step 1: Write failing ToolDefinition metadata tests**

Keep the existing false/default JSON assertion unchanged so a default-exclusive tool omits the field. Add a literal assertion for an explicitly safe definition:

```go
parallelDefinition := schema.ToolDefinition{
    Name:         "read_file",
    Description:  "read a file",
    InputSchema:  map[string]any{"type": "object"},
    ParallelSafe: true,
}
parallelJSON, err := json.Marshal(parallelDefinition)
if err != nil {
    t.Fatalf("marshal parallel ToolDefinition: %v", err)
}
if got, want := string(parallelJSON), `{"name":"read_file","description":"read a file","input_schema":{"type":"object"},"parallel_safe":true}`; got != want {
    t.Fatalf("parallel ToolDefinition JSON = %s, want %s", got, want)
}
```

- [ ] **Step 2: Run the Schema test and verify RED**

Run:

```bash
GOCACHE=/tmp/go-reagent-build-cache go test -run '^TestToolProtocolTypesExposeHarnessMetadata$' ./internal/schema
```

Expected: compile failure because `ToolDefinition.ParallelSafe` does not exist.

- [ ] **Step 3: Add the minimal Schema field**

Extend only `ToolDefinition`:

```go
// ParallelSafe 表示 Harness 可以在同一波次中并发执行该工具；默认 false。
ParallelSafe bool `json:"parallel_safe,omitempty"`
```

- [ ] **Step 4: Run the Schema test and verify GREEN**

Run the Step 2 command again.

Expected: PASS, including the unchanged assertion that `false` is omitted.

- [ ] **Step 5: Write the failing read_file safety assertion**

In `TestReadFileToolDefinition`, add:

```go
if !definition.ParallelSafe {
    t.Fatal("read_file must be marked parallel-safe")
}
```

This test catches accidentally routing `read_file` through the exclusive branch.

- [ ] **Step 6: Run the read_file Definition test and verify RED**

Run:

```bash
GOCACHE=/tmp/go-reagent-build-cache go test -run '^TestReadFileToolDefinition$' ./internal/tools
```

Expected: FAIL with `read_file must be marked parallel-safe`.

- [ ] **Step 7: Mark read_file safe and verify GREEN**

Set `ParallelSafe: true` in `ReadFileTool.Definition`, then rerun the Step 6 command.

Expected: PASS.

---

### Task 2: Prove parallel execution and deterministic Observation order

**Files:**
- Modify: `internal/engine/loop_test.go`
- Modify: `internal/engine/loop.go`

**Interfaces:**
- Produces: `AgentEngine.MaxParallelTools int`.
- `NewAgentEngine` sets `MaxParallelTools: 4` without changing its parameters.
- Adds private batch helpers; no new exported function is required.

- [ ] **Step 1: Make the existing fake Registry race-safe**

Add `sync.Mutex` to `fakeRegistry`, protect `calls`, compute the call count while locked, and invoke `afterExecute` after releasing the lock. Add a snapshot helper:

```go
func (r *fakeRegistry) Calls() []schema.ToolCall {
    r.mu.Lock()
    defer r.mu.Unlock()
    return append([]schema.ToolCall(nil), r.calls...)
}
```

Replace direct `len(registry.calls)` assertions with `len(registry.Calls())`. Do not lock around callback execution or tool results.

- [ ] **Step 2: Add a channel-controlled Registry test utility**

The utility exercises the real Engine scheduling boundary rather than mocking timing:

```go
type controlledRegistry struct {
    definitions []schema.ToolDefinition
    started     chan schema.ToolCall
    finished    chan schema.ToolCall
    gates       map[string]chan struct{}
    results     map[string]schema.ToolResult
}

func (r *controlledRegistry) GetAvailableTools() []schema.ToolDefinition {
    return append([]schema.ToolDefinition(nil), r.definitions...)
}

func (r *controlledRegistry) Execute(ctx context.Context, call schema.ToolCall) schema.ToolResult {
    select {
    case r.started <- call:
    case <-ctx.Done():
        return schema.ToolResult{ToolCallID: call.ID, Output: ctx.Err().Error(), IsError: true}
    }
    select {
    case <-r.gates[call.ID]:
    case <-ctx.Done():
        return schema.ToolResult{ToolCallID: call.ID, Output: ctx.Err().Error(), IsError: true}
    }
    r.finished <- call
    if result, ok := r.results[call.ID]; ok {
        return result
    }
    return schema.ToolResult{ToolCallID: call.ID, Output: call.Name}
}
```

Use buffered `started`/`finished` channels and one gate per call. Test helpers must close all still-open gates and wait for the Run goroutine before reporting a timeout, so a failed RED test does not leak goroutines.

- [ ] **Step 3: Write a failing concurrency-and-order test**

Return three `ParallelSafe: true` ToolCalls followed by a final assistant response. Start `Run` in a goroutine and require all three calls to appear on `started` before opening any gate. A serial engine can only report one start and therefore fails this assertion.

Release and wait for completion in order `call-3`, `call-2`, `call-1`. After `Run` returns, assert the second Provider request contains Observations in original order:

```go
for index, wantID := range []string{"call-1", "call-2", "call-3"} {
    observation := provider.requests[1][3+index]
    if observation.ToolCallID != wantID {
        t.Fatalf("observation %d ID = %q, want %q", index, observation.ToolCallID, wantID)
    }
}
```

- [ ] **Step 4: Run the concurrency test and verify RED**

Run:

```bash
GOCACHE=/tmp/go-reagent-build-cache go test -run '^TestAgentEngineExecutesParallelSafeToolsConcurrentlyInCallOrder$' ./internal/engine
```

Expected: FAIL because the serial loop does not start calls 2 and 3 before call 1 is released.

- [ ] **Step 5: Implement the minimal safe-wave executor**

Add:

```go
const defaultMaxParallelTools = 4

type AgentEngine struct {
    // existing fields...
    MaxParallelTools int
}
```

Build `parallelSafeByName` from the current `availableTools`. Replace the inline serial loop with a private method that preallocates `observationMsgs`, scans consecutive safe calls, and invokes `executeParallelWave` for each safe range. Keep unsafe calls serial.

Each wave uses a WaitGroup and semaphore. Every worker writes only `observationMsgs[index]`. Extract one `executeToolCall` helper for common Registry invocation, logging, and Message construction:

```go
func (e *AgentEngine) executeToolCall(ctx context.Context, index int, call schema.ToolCall) schema.Message {
    result := e.registry.Execute(ctx, call)
    // existing success/error logging
    return schema.Message{Role: schema.RoleUser, Content: result.Output, ToolCallID: call.ID}
}
```

- [ ] **Step 6: Run focused and existing Engine tests and verify GREEN**

Run:

```bash
GOCACHE=/tmp/go-reagent-build-cache go test -count=1 ./internal/engine
```

Expected: PASS.

---

### Task 3: Enforce bounds, barriers, errors, and cancellation

**Files:**
- Modify: `internal/engine/loop_test.go`
- Modify: `internal/engine/loop.go`

**Interfaces:**
- Preserves Task 2 public API.
- Effective safe-wave concurrency is `min(max(1, MaxParallelTools), waveSize)`.

- [ ] **Step 1: Write a failing bounded-concurrency test**

Set `engine.MaxParallelTools = 2` and return four safe calls. Require two starts, verify no third call enters while both gates remain closed, release one of the first calls, and then require exactly one additional start. Finish all calls and assert success.

Use a short receive timeout only as a deadlock/negative-assertion guard; do not compare total execution duration.

- [ ] **Step 2: Write a failing exclusive-barrier test**

Return this ordered batch and matching Definitions:

```text
read-1 (safe), read-2 (safe), write (unsafe), read-3 (safe), missing (unknown)
```

Assert these phases with gates:

1. `read-1` and `read-2` start, while `write` does not;
2. after both reads finish, only `write` starts;
3. after `write` finishes, `read-3` starts;
4. after `read-3` finishes, `missing` starts and returns an error Observation.

The controlled Registry should return an explicit unknown-tool error for `missing`; its absence from Definitions is what makes it a scheduler barrier.

- [ ] **Step 3: Update cancellation coverage for queued safe calls**

Change `TestAgentEngineStopsToolBatchAfterCancellation` so both definitions are `ParallelSafe: true` and set `engine.MaxParallelTools = 1`. The first fake execution cancels Context. Assert `Run` wraps `context.Canceled` and the second call never enters Registry.

- [ ] **Step 4: Add sibling-error coverage**

In a two-call safe wave, make one result `IsError: true` and the other successful. Release both and assert both Observations reach the next Provider request in ToolCall order, with the error text preserved.

- [ ] **Step 5: Run the new policy tests and verify RED**

Run:

```bash
GOCACHE=/tmp/go-reagent-build-cache go test -run '^TestAgentEngine(BoundsParallelTools|UsesExclusiveToolsAsBarriers|StopsToolBatchAfterCancellation|CompletesParallelSiblingsAfterToolError)$' ./internal/engine
```

Expected: one or more policy assertions fail until semaphore acquisition, barriers, and cancellation checks are complete.

- [ ] **Step 6: Complete the scheduler policy**

For every safe worker:

```go
select {
case semaphore <- struct{}{}:
case <-ctx.Done():
    return
}
defer func() { <-semaphore }()
if ctx.Err() != nil {
    return
}
observationMsgs[index] = e.executeToolCall(ctx, index, call)
```

After each `wg.Wait`, return `ctx.Err()` before scanning the next range. Check Context immediately before and after each unsafe Registry execution. Treat a missing Definition as unsafe through a false map lookup. Tool `IsError` affects logging/Observation only and never cancels Context.

- [ ] **Step 7: Add and verify `MaxParallelTools <= 0` serial fallback**

Use two safe gated calls with `MaxParallelTools = 0`. Require only the first call to start before its gate opens, then the second. This catches accidental interpretation of zero as unlimited.

- [ ] **Step 8: Run all Engine tests with race detection and verify GREEN**

Run:

```bash
GOCACHE=/tmp/go-reagent-build-cache go test -race -count=1 ./internal/engine
```

Expected: PASS with no race report.

---

### Task 4: Update the runnable demonstration and documentation

**Files:**
- Modify: `cmd/reagent/main.go`
- Modify: `README.md`

**Interfaces:**
- Preserves config-based Provider construction, Registry setup, and `AGENT_PROMPT` override.
- Changes only the default Engine settings and fallback task.

- [ ] **Step 1: Update the CLI demonstration**

Construct the Engine with Thinking enabled:

```go
eng := engine.NewAgentEngine(llmProvider, registry, workDir, true)
```

Replace the fallback Prompt with a task that references only existing, non-secret files:

```go
prompt = `请同时调用 read_file 工具读取当前工作区的 README.md、go.mod 和 cmd/reagent/main.go，
然后综合说明这三个文件分别定义了什么内容。`
```

Do not read `config.json`, create demo files, or register tools absent from the repository.

- [ ] **Step 2: Update README behavior descriptions**

Revise the core flow so ToolCalls pass through “按 ParallelSafe 分波 -> 安全波有界并发 -> 独占工具屏障 -> 稳定聚合 Observation”。Document:

- `ParallelSafe` defaults false;
- `read_file` is currently the only safe tool;
- default maximum is 4 and nonpositive values are serial;
- same-wave calls must be semantically independent;
- dependent work remains separate model turns;
- current CLI enables Thinking and requests three parallel reads.

Update the current-capabilities list and add a completed bounded-scheduler roadmap item. Keep config and Provider documentation unchanged.

- [ ] **Step 3: Run CLI and package regression tests**

Run:

```bash
GOCACHE=/tmp/go-reagent-build-cache go test -count=1 ./cmd/reagent ./internal/schema ./internal/tools ./internal/engine
```

Expected: PASS.

---

### Task 5: Full verification

**Files:**
- Verify all repository Go files and the updated README/spec/plan.

**Interfaces:**
- Verifies Schema -> Tool Definition -> Engine scheduling -> Provider follow-up integration.

- [ ] **Step 1: Format touched Go files**

Run:

```bash
gofmt -w internal/schema/message.go internal/schema/message_test.go internal/tools/read_file.go internal/tools/read_file_test.go internal/engine/loop.go internal/engine/loop_test.go cmd/reagent/main.go
```

Expected: exit code 0.

- [ ] **Step 2: Run static analysis**

Run:

```bash
GOCACHE=/tmp/go-reagent-build-cache go vet ./...
```

Expected: no diagnostics.

- [ ] **Step 3: Run the complete race-enabled suite**

Run outside the restricted network sandbox if required by existing Provider tests that create `httptest` listeners:

```bash
go test -race -count=1 ./...
```

Expected: all packages PASS with no race report.

- [ ] **Step 4: Verify formatting and scope**

Run:

```bash
gofmt -l cmd internal
rg -n 'github.com/yourname|MaxParallelTools|ParallelSafe' --glob '*.go' .
```

Expected: `gofmt` has no output; search finds only project imports absent and the intended scheduler declarations/usages. Manually confirm Provider and Registry source files, `go.mod`, and `go.sum` did not change.
