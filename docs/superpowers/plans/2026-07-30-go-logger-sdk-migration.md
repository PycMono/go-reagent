# go-logger-sdk Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Replace every production use of Go's standard `log` package with `github.com/PycMono/go-logger-sdk` v1.0.5 and emit structured JSON operational logs.

**Architecture:** Configure the SDK package-level logger once during bootstrap with module `go-reagent`, then call SDK package functions directly from Bootstrap, Engine, and Registry. Keep the two `fmt.Printf` calls that deliver model thinking and final answers to the terminal. Use a small `internal/logtest` recorder only from tests to verify event levels and structured fields without depending on SDK output internals.

**Tech Stack:** Go 1.26, `github.com/PycMono/go-logger-sdk` v1.0.5, Go testing

## Global Constraints

- Output operational logs as JSON with `module=go-reagent`.
- Accept the v1.0.5 caller defect; use `component` and event-specific fields for location.
- Call `logsdk.SetLogger` exactly once during startup before concurrent work begins.
- Preserve `Fatal` exit behavior and never log API keys, authorization headers, or complete platform configuration.
- Preserve the two user-facing `fmt.Printf` calls for model thinking and final replies.
- Do not add a production logging wrapper around the SDK.
- Do not change provider, tool, configuration, or scheduling behavior.

---

### Task 1: Migrate Engine lifecycle and tool execution logs

**Files:**
- Create: `internal/logtest/recorder.go`
- Modify: `internal/engine/loop_test.go`
- Modify: `internal/engine/loop.go`

**Interfaces:**
- Consumes: SDK `Logger`, `Fields`, `SetLogger`, `Info`, `Error`, and `Any`.
- Produces: `logtest.Recorder` implementing all six SDK `Logger` methods and Engine JSON events with `component=engine`.

- [x] **Step 1: Create the reusable recording Logger**

Create `internal/logtest/recorder.go` with this complete API:

```go
package logtest

import (
	"context"
	"sync"

	logsdk "github.com/PycMono/go-logger-sdk"
)

type Event struct {
	Level   string
	Message string
	Fields  logsdk.Fields
}

type Recorder struct {
	mu     sync.Mutex
	events []Event
}

func (r *Recorder) record(level, message string, fields ...logsdk.Fields) {
	merged := logsdk.N()
	for _, group := range fields {
		for key, value := range group {
			merged[key] = value
		}
	}
	r.mu.Lock()
	r.events = append(r.events, Event{Level: level, Message: message, Fields: merged})
	r.mu.Unlock()
}

func (r *Recorder) Debug(_ context.Context, message string, fields ...logsdk.Fields) { r.record("debug", message, fields...) }
func (r *Recorder) Info(_ context.Context, message string, fields ...logsdk.Fields)  { r.record("info", message, fields...) }
func (r *Recorder) Warn(_ context.Context, message string, fields ...logsdk.Fields)  { r.record("warn", message, fields...) }
func (r *Recorder) Error(_ context.Context, message string, fields ...logsdk.Fields) { r.record("error", message, fields...) }
func (r *Recorder) Fatal(_ context.Context, message string, fields ...logsdk.Fields) { r.record("fatal", message, fields...) }
func (r *Recorder) Panic(_ context.Context, message string, fields ...logsdk.Fields) { r.record("panic", message, fields...) }

func (r *Recorder) Find(level, message string) (Event, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, event := range r.events {
		if event.Level == level && event.Message == message {
			return event, true
		}
	}
	return Event{}, false
}
```

- [x] **Step 2: Write the failing Engine structured-log test**

Add `github.com/PycMono/go-reagent/internal/logtest` and the SDK import to `loop_test.go`, then add:

```go
func TestAgentEngineEmitsStructuredLifecycleLogs(t *testing.T) {
	recorder := &logtest.Recorder{}
	logsdk.SetLogger(recorder)
	t.Cleanup(func() {
		logsdk.SetLogger(logsdk.NewLogrus(logsdk.Options{LogFormat: "json", Module: "go-reagent"}))
	})

	provider := &fakeProvider{responses: []*schema.Message{{Role: schema.RoleAssistant, Content: "done"}}}
	agentEngine := engine.NewAgentEngine(provider, &fakeRegistry{}, "/workspace", false)
	if err := agentEngine.Run(context.Background(), "hello"); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	started, ok := recorder.Find("info", "Agent 引擎启动")
	if !ok || started.Fields["component"] != "engine" || started.Fields["work_dir"] != "/workspace" || started.Fields["thinking_enabled"] != false {
		t.Fatalf("start event = %#v, found = %v", started, ok)
	}
	turn, ok := recorder.Find("info", "Agent 轮次开始")
	if !ok || turn.Fields["turn"] != 1 {
		t.Fatalf("turn event = %#v, found = %v", turn, ok)
	}
	phase, ok := recorder.Find("info", "Action 阶段开始")
	if !ok || phase.Fields["phase"] != "action" || phase.Fields["turn"] != 1 {
		t.Fatalf("phase event = %#v, found = %v", phase, ok)
	}
}
```

- [x] **Step 3: Run the Engine test and verify RED**

Run: `GOCACHE=/tmp/go-reagent-build-cache go test -run '^TestAgentEngineEmitsStructuredLifecycleLogs$' ./internal/engine`

Expected: FAIL because the Engine still writes through standard `log` and the recorder has no matching events.

- [x] **Step 4: Replace every Engine standard-log call**

Remove the standard `log` import, import the SDK as `logsdk`, and use these exact event messages and fields:

```go
logsdk.Info(ctx, "Agent 引擎启动",
	logsdk.Any("component", "engine"),
	logsdk.Any("work_dir", e.WorkDir),
	logsdk.Any("thinking_enabled", e.EnableThinking),
)
logsdk.Info(ctx, "Agent 轮次开始", logsdk.Any("component", "engine"), logsdk.Any("turn", turnCount))
logsdk.Info(ctx, "Thinking 阶段开始", logsdk.Any("component", "engine"), logsdk.Any("turn", turnCount), logsdk.Any("phase", "thinking"))
logsdk.Info(ctx, "Action 阶段开始", logsdk.Any("component", "engine"), logsdk.Any("turn", turnCount), logsdk.Any("phase", "action"))
logsdk.Info(ctx, "模型未请求调用工具，任务完成", logsdk.Any("component", "engine"), logsdk.Any("turn", turnCount))
logsdk.Info(ctx, "调度工具调用", logsdk.Any("component", "engine"), logsdk.Any("turn", turnCount), logsdk.Any("tool_count", len(actionResp.ToolCalls)))
```

In `executeToolCall`, map start/error/success to `工具执行开始`, `工具执行失败`, and `工具执行成功`. Every event includes `component`, `tool_index`, `tool`, and `tool_call_id`; start also includes `arguments`, error includes `result`, and success includes `result_bytes`. Use `Error` only for the failure event. Leave both `fmt.Printf` calls unchanged.

- [x] **Step 5: Verify the Engine migration**

Run:

```bash
gofmt -w internal/logtest/recorder.go internal/engine/loop.go internal/engine/loop_test.go
GOCACHE=/tmp/go-reagent-build-cache go test -count=1 ./internal/engine
! rg -n '"log"|\blog\.' internal/engine --glob '*.go'
```

Expected: PASS and no standard-log match.

- [x] **Step 6: Commit Engine migration**

```bash
git add internal/logtest/recorder.go internal/engine/loop.go internal/engine/loop_test.go
git commit -m "refactor: migrate engine logs to logger sdk"
```

### Task 2: Migrate Registry registration and panic logs

**Files:**
- Modify: `internal/tools/registry_test.go`
- Modify: `internal/tools/registry.go`

**Interfaces:**
- Consumes: `logtest.Recorder` from Task 1 and the SDK package-level Logger.
- Produces: Registry events `工具注册成功` and `工具执行 panic` with safe structured fields.

- [x] **Step 1: Write the failing Registry structured-log test**

Add the SDK and `internal/logtest` imports, then add:

```go
func TestRegistryEmitsStructuredRegistrationAndPanicLogs(t *testing.T) {
	recorder := &logtest.Recorder{}
	logsdk.SetLogger(recorder)
	t.Cleanup(func() {
		logsdk.SetLogger(logsdk.NewLogrus(logsdk.Options{LogFormat: "json", Module: "go-reagent"}))
	})

	registry := NewRegistry()
	if err := registry.Register(&stubTool{name: "panic", panicOnExecute: true}); err != nil {
		t.Fatal(err)
	}
	_ = registry.Execute(context.Background(), schema.ToolCall{ID: "call-panic", Name: "panic"})

	registered, ok := recorder.Find("info", "工具注册成功")
	if !ok || registered.Fields["component"] != "registry" || registered.Fields["tool"] != "panic" {
		t.Fatalf("registration event = %#v, found = %v", registered, ok)
	}
	panicked, ok := recorder.Find("error", "工具执行 panic")
	if !ok || panicked.Fields["component"] != "registry" || panicked.Fields["tool"] != "panic" || panicked.Fields["stack"] == nil {
		t.Fatalf("panic event = %#v, found = %v", panicked, ok)
	}
}
```

- [x] **Step 2: Run the Registry test and verify RED**

Run: `GOCACHE=/tmp/go-reagent-build-cache go test -run '^TestRegistryEmitsStructuredRegistrationAndPanicLogs$' ./internal/tools`

Expected: FAIL because Registry still writes through standard `log`.

- [x] **Step 3: Replace Registry standard-log calls**

Remove the standard `log` import, import the SDK, and replace the two events exactly:

```go
logsdk.Info(context.Background(), "工具注册成功",
	logsdk.Any("component", "registry"),
	logsdk.Any("tool", name),
)

logsdk.Error(ctx, "工具执行 panic",
	logsdk.Any("component", "registry"),
	logsdk.Any("tool", call.Name),
	logsdk.Any("stack", debug.Stack()),
)
```

Do not record the recovered panic value.

- [x] **Step 4: Verify the Registry migration**

Run:

```bash
gofmt -w internal/tools/registry.go internal/tools/registry_test.go
GOCACHE=/tmp/go-reagent-build-cache go test -count=1 ./internal/tools
! rg -n '"log"|\blog\.' internal/tools --glob '*.go'
```

Expected: PASS and no standard-log match.

- [x] **Step 5: Commit Registry migration**

```bash
git add internal/tools/registry.go internal/tools/registry_test.go
git commit -m "refactor: migrate registry logs to logger sdk"
```

### Task 3: Configure Bootstrap, direct dependency, and documentation

**Files:**
- Modify: `cmd/reagent/main_test.go`
- Modify: `cmd/reagent/main.go`
- Modify: `go.mod`
- Modify: `go.sum` if `go mod tidy` changes it
- Modify: `README.md`

**Interfaces:**
- Consumes: SDK `Options`, `NewLogrus`, `SetLogger`, `Info`, `Error`, `Fatal`, `Any`, and `Err`.
- Produces: a JSON default logger configured before startup work and a direct SDK module dependency.

- [x] **Step 1: Write the failing Bootstrap JSON-output test**

Add `encoding/json` and `io` imports and this test to `cmd/reagent/main_test.go`:

```go
func TestNewApplicationLoggerEmitsJSONWithProjectModule(t *testing.T) {
	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	originalStdout := os.Stdout
	os.Stdout = writeEnd
	t.Cleanup(func() {
		os.Stdout = originalStdout
		_ = readEnd.Close()
		_ = writeEnd.Close()
	})

	logger := newApplicationLogger()
	logger.Info(context.Background(), "logger ready")
	if err := writeEnd.Close(); err != nil {
		t.Fatal(err)
	}
	encoded, err := io.ReadAll(readEnd)
	if err != nil {
		t.Fatal(err)
	}

	var event map[string]any
	if err := json.Unmarshal(encoded, &event); err != nil {
		t.Fatalf("log output = %q, error = %v", encoded, err)
	}
	if event["module"] != "go-reagent" || event["msg"] != "logger ready" || event["level"] != "info" {
		t.Fatalf("log event = %#v", event)
	}
}
```

- [x] **Step 2: Run the Bootstrap test and verify RED**

Run: `GOCACHE=/tmp/go-reagent-build-cache go test -run '^TestNewApplicationLoggerEmitsJSONWithProjectModule$' ./cmd/reagent`

Expected: FAIL to compile because `newApplicationLogger` does not exist.

- [x] **Step 3: Configure the logger and replace Bootstrap log calls**

Remove the standard `log` import, import the SDK, and add:

```go
func newApplicationLogger() logsdk.Logger {
	return logsdk.NewLogrus(logsdk.Options{LogFormat: "json", Module: "go-reagent"})
}
```

At the start of `main`, create `ctx := context.Background()` and immediately call:

```go
logsdk.SetLogger(newApplicationLogger())
```

Use `Fatal` for existing fatal paths with `component=bootstrap` and `logsdk.Err(err)`. Emit platform success with `Info`, fields `component`, `platform_id`, `protocol`, and `model`. Emit closer failure with `Error` and `logsdk.Err(err)`. Pass `ctx` into `eng.Run`; never log credentials or the complete platform object.

- [x] **Step 4: Promote the SDK to a direct dependency and document logging**

Move this exact requirement into the first `require` block without changing its version:

```text
github.com/PycMono/go-logger-sdk v1.0.5
```

Run `go mod tidy`. Add a README logging subsection stating that operational logs are JSON on stdout, contain `module=go-reagent` plus structured component fields, and coexist with plain-text model output. Document that API keys and authorization headers are not logged.

- [x] **Step 5: Run complete acceptance verification**

Run:

```bash
gofmt -w cmd/reagent/main.go cmd/reagent/main_test.go
! rg -n '"log"|\blog\.(Print|Printf|Println|Fatal|Fatalf)' cmd internal --glob '*.go'
test "$(rg -n 'fmt\.Printf' internal/engine/loop.go | wc -l | tr -d ' ')" = "2"
GOCACHE=/tmp/go-reagent-build-cache go test -count=1 ./...
GOCACHE=/tmp/go-reagent-build-cache go test -race -count=1 ./...
GOCACHE=/tmp/go-reagent-build-cache go vet ./...
git diff --check
```

Expected: every command passes; production standard-log usage is zero and exactly two user-output `fmt.Printf` calls remain.

- [x] **Step 6: Commit Bootstrap and documentation migration**

```bash
git add cmd/reagent/main.go cmd/reagent/main_test.go go.mod go.sum README.md docs/superpowers/plans/2026-07-30-go-logger-sdk-migration.md
git commit -m "refactor: adopt logger sdk across reagent"
```
