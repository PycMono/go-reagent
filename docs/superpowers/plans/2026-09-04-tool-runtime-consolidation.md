# Tool Runtime Consolidation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the `Registry → Executor → Scheduler` object chain with `Registry → Runtime` while preserving tool execution, visibility, events, errors, and bounded scheduling behavior.

**Architecture:** `Registry` remains the startup-time mutable and runtime-time frozen tool catalog. A single `toolexec.Runtime` owns the Registry reference, middleware snapshot, and parallelism limit; its methods cover both single-call execution and batch scheduling. Production code is consolidated into `registry.go` and `runtime.go`; scheduling tests remain focused in `scheduler_test.go`.

**Tech Stack:** Go 1.26, `pi/ai`, `pi/middleware`, Go testing package.

**Spec:** `docs/superpowers/specs/2026-09-04-tool-runtime-consolidation-design.md`

## Global Constraints

- Keep `Registry` separate and preserve Register, Rollback, Freeze, Definitions, Lookup, and schema-validation semantics.
- Keep `availableTools` as the per-run visibility and scheduling-policy subset; never replace it with the Registry's complete definitions.
- Preserve middleware order, start/update/end events, error normalization, cancellation, serial barriers, maximum parallelism, and result ordering.
- Remove the public `Executor`, `Scheduler`, `NewExecutor`, and `NewScheduler` API; this source-breaking change is explicitly approved.
- Preserve unrelated working-tree changes and do not create an implementation commit that would absorb pre-existing edits in overlapping files.

---

### Task 1: Consolidate execution and scheduling into Runtime

**Files:**
- Modify: `pi/toolexec/event_test.go`
- Create: `pi/toolexec/scheduler_test.go`
- Modify: `pi/toolexec/runtime.go`
- Delete: `pi/toolexec/scheduler.go`
- Modify: `pi/agent.go`
- Modify: `pi/loop.go`
- Modify: `pi/subagent.go`
- Modify: `docs/sdk-architecture.md`

**Interfaces:**
- Consumes: `NewRegistry([]ai.Tool) (*Registry, error)`, `middleware.Handler`, `ai.ToolDefinitions`, `EventObserver`.
- Produces: `NewRuntime(*Registry, []middleware.Handler, int) *Runtime`, `(*Runtime).Definitions`, `(*Runtime).Execute`, `(*Runtime).Schedule`, `(*Runtime).Mode`, and `(*Runtime).IsSubagentTool`.

- [x] **Step 1: Write the failing Runtime API test**

Add this test to `pi/toolexec/event_test.go` so the desired API is exercised through both execution paths:

```go
func TestRuntimeOwnsExecutionAndScheduling(t *testing.T) {
	registry, err := NewRegistry([]ai.Tool{registryTestTool("read")})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	runtime := NewRuntime(registry, nil, 2)
	call := ai.ToolCall{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{}`)}

	direct, err := runtime.Execute(context.Background(), call, nil)
	if err != nil || direct.Phase != EventEnd || direct.IsError {
		t.Fatalf("Execute() event = %#v, error = %v", direct, err)
	}
	scheduled, err := runtime.Schedule(
		context.Background(), []ai.ToolCall{call}, runtime.Definitions(), nil,
	)
	if err != nil || len(scheduled) != 1 || scheduled[0].Call.ID != call.ID {
		t.Fatalf("Schedule() events = %#v, error = %v", scheduled, err)
	}
}
```

- [x] **Step 2: Run the test and verify RED**

Run:

```bash
GOCACHE=/tmp/go-reagent-go-cache go test ./pi/toolexec -run TestRuntimeOwnsExecutionAndScheduling -count=1
```

Expected: build failure containing `undefined: NewRuntime`.

- [x] **Step 3: Define the unified Runtime and constructor**

In `pi/toolexec/runtime.go`, replace `Executor` and `NewExecutor` with:

```go
// Runtime owns registered-tool execution and bounded batch scheduling.
type Runtime struct {
	registry    *Registry
	handlers    []middleware.Handler
	maxParallel int
}

func NewRuntime(registry *Registry, handlers []middleware.Handler, maxParallel int) *Runtime {
	handlers = append([]middleware.Handler{}, handlers...)
	handlers = append(handlers, middleware.ExecuteTool)
	return &Runtime{registry: registry, handlers: handlers, maxParallel: maxParallel}
}
```

Rename the receivers of `Definitions` and `Execute` from `*Executor` to `*Runtime`. Keep `normalizeEndEvent` and `maxToolOutputBytes` unchanged.

- [x] **Step 4: Move Scheduler behavior onto Runtime and remove scheduler.go**

Move the scheduling declarations and implementations into `pi/toolexec/runtime.go`, then delete `pi/toolexec/scheduler.go`:

```go
// Delete type Scheduler and NewScheduler.
func (runtime *Runtime) IsSubagentTool(name string) bool
func (runtime *Runtime) Schedule(
	ctx context.Context,
	calls []ai.ToolCall,
	availableTools ai.ToolDefinitions,
	observer EventObserver,
) ([]Event, error)
func (runtime *Runtime) Mode(calls []ai.ToolCall, availableTools ai.ToolDefinitions) string
func (runtime *Runtime) executeWave(
	ctx context.Context,
	calls []ai.ToolCall,
	results []Event,
	start int,
	end int,
	observer EventObserver,
	mode string,
	knownTools map[string]bool,
) error
```

Use `runtime.registry.lookup`, `runtime.maxParallel`, and `runtime.Execute` directly. Preserve the existing wave partition, semaphore, queue metrics, cancellation checks, and indexed result writes without algorithm changes.

- [x] **Step 5: Migrate all consumers atomically**

Apply these exact type and construction changes:

```go
// pi/agent.go
toolRuntime *toolexec.Runtime

toolRuntime := toolexec.NewRuntime(
	registry,
	append(middleware.Defaults(), opts.ExtraHandlers...),
	defaultMaxParallelTools,
)
loop := NewLoop(provider, toolRuntime, opts.Compaction,
	WithLoopProviderIdentity(opts.Platform.ID, opts.Platform.Model),
	WithLoopDetection(opts.LoopDetection),
)

// pi/loop.go
toolRuntime *toolexec.Runtime

func NewLoop(
	provider ai.Provider,
	toolRuntime *toolexec.Runtime,
	compaction harness.CompactionConfig,
	options ...LoopOption,
) *Loop
```

Replace `l.scheduler.Mode`, `l.scheduler.Schedule`, and `l.scheduler.IsSubagentTool` with the corresponding `l.toolRuntime` calls.

In `pi/subagent.go`, change `ToolRuntime` fields to `*toolexec.Runtime`, remove child `NewScheduler` construction, and pass the shared Runtime directly to each child `NewLoop`.

In `docs/sdk-architecture.md`, replace remaining `*toolexec.Executor` and `*toolexec.Scheduler` references with `*toolexec.Runtime` where they describe the current API.

- [x] **Step 6: Rename tests, restore scheduling contracts, and verify old API removal**

Rename `TestExecutor...` tests in `pi/toolexec/event_test.go` to `TestRuntime...` and construct with `NewRuntime(registry, handlers, 2)`.

Add direct Runtime scheduling tests in `pi/toolexec/scheduler_test.go` that use controlled tools and gates to verify parallel waves around serial barriers, the maximum active call count, and indexed result ordering.

Run:

```bash
rg -n 'type (Executor|Scheduler) struct|NewExecutor|NewScheduler|\*toolexec\.(Executor|Scheduler)|l\.scheduler' pi docs/sdk-architecture.md --glob '*.go' --glob '*.md'
```

Expected: no matches.

- [x] **Step 7: Run focused tests and verify GREEN**

Run:

```bash
gofmt -w pi/toolexec/runtime.go pi/toolexec/scheduler.go pi/toolexec/event_test.go pi/agent.go pi/loop.go pi/subagent.go
GOCACHE=/tmp/go-reagent-go-cache go test ./pi/... -count=1
```

Expected: all `pi/...` packages pass.

- [x] **Step 8: Verify repository formatting and broader regression state**

Run:

```bash
git diff --check
GOCACHE=/tmp/go-reagent-go-cache go test ./... -count=1
```

Expected: `git diff --check` passes. Record the full-suite result exactly; if `conversation.TestRegisteredConversationGraphStartsDisabledWithoutMySQL` still fails because Fx cannot provide `*chat.Service`, report it as the pre-existing out-of-scope failure and do not modify conversation wiring.

- [x] **Step 9: Leave implementation changes uncommitted for review**

Run:

```bash
git status --short
git diff -- pi/toolexec/runtime.go pi/toolexec/scheduler.go pi/toolexec/event_test.go pi/agent.go pi/loop.go pi/subagent.go docs/sdk-architecture.md
```

Expected: only inspect and report the relevant changes. Do not stage or commit these overlapping files because the working tree already contains user-owned edits.

## Post-implementation adjustment

After implementation, the user explicitly requested deletion of
`pi/toolexec/event_test.go`, `pi/toolexec/scheduler_test.go`, and
`pi/toolexec/registry_test.go`. Do not recreate or relocate those tests; validate the
resulting tree with the remaining `pi/...` suite.
