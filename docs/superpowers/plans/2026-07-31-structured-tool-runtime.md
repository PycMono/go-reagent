# Structured Tool Runtime Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the string-based Agent/tool pipeline with a text-only structured runtime that exposes exactly `read`, `edit`, `write`, `apply_patch`, `exec`, and `process`, while preserving guarded workspace access, stable tool scheduling, and Fx-managed lifecycle cleanup.

**Architecture:** `app.AgentRunner` calls one injected `engine.AgentRuntime`; the runtime prepares messages through `context.RunContextFactory` and delegates turns to `engine.AgentLoop`. Tool calls flow through `engine.ToolScheduler` into a middleware-backed `tools.Registry`, then concrete tools use one shared `tools.Workspace` or `tools.ProcessSupervisor`; structured events fan out through ordered Reporters.

**Tech Stack:** Go 1.26, Uber Fx 1.23, `github.com/santhosh-tekuri/jsonschema/v6` v6.0.2, the existing OpenAI v3 and Anthropic v1 Go SDKs, Go standard-library process management, and standard `testing`/`httptest`.

## Global Constraints

- Expose exactly `read`, `edit`, `write`, `apply_patch`, `exec`, and `process`; do not retain aliases for `read_file`, `edit_file`, or `write_file`.
- Keep all structured content text-only; do not add image content, PTY, sandbox, approval, host/node routing, `send-keys`, `submit`, or `paste`.
- Use `path + edits[]` with `oldText/newText` for `edit`; use camelCase fields and the approved action set for `exec/process`; keep `apply_patch.input` unchanged.
- Treat every file-tool path and `exec.workdir` as WorkDir-relative. File tools must not escape WorkDir; shell commands retain host permissions and WorkDir is not a sandbox.
- Emit streaming updates only from foreground `exec` stdout/stderr. Updates reach Terminal, are ignored by WeCom, and never enter model history.
- Normalize ordinary tool failures to `ToolResult.IsError=true`; return Context cancellation/deadline as Go errors so the Agent loop stops.
- Keep `ToolOutput.Details`, `ToolUpdate.Details`, and `ToolResult.Details` internal to the runtime; Provider adapters serialize only message content, tool calls, call IDs, tool names, and Claude's error flag.
- Mark only `read` as `ParallelSafe`; preserve exclusive barriers, the configured concurrency ceiling, and original Tool Call result order.
- Keep extension contracts under `internal`; Fx groups are `agent_tools`, `tool_middlewares`, and `reporters`.
- Sort Middleware and Reporter registrations by `Order`, then `Name`; reject duplicate tool names; sort model-visible tool definitions by `Name`.
- Use one injected `Workspace` wrapping `os.Root` and one injected `ProcessSupervisor`; Fx Stop must terminate process groups before closing Workspace.
- Preserve existing unrelated and staged worktree changes, especially `.idea/go-reagent.iml`; every implementation commit must use `git commit --only -- <task paths>` and must inspect those paths before committing.
- Keep package-local `register.go` ownership and make `internal/register.go` the only graph aggregation point used by `cmd/main.go`.
- Follow TDD for every task and leave the repository compiling at each task commit.

## File Responsibility Map

### Create

- `internal/schema/content.go`: content-block types and text conversion helpers.
- `internal/schema/event.go`: Tool output/update/result types, lifecycle events, and discriminated event constructors.
- `internal/schema/event_test.go`: JSON and constructor invariants for structured events.
- `internal/tools/tool.go`: Tool, handler, middleware, observer, and Fx registration contracts.
- `internal/tools/schema_validator.go`: compile each ToolDefinition input schema once and validate decoded JSON values.
- `internal/tools/schema_validator_test.go`: required/type/unknown-field validation coverage.
- `internal/tools/middleware.go`: ordered middleware chain, recovery, Context, logging, and 50 KiB output-limit implementations.
- `internal/tools/middleware_test.go`: ordering, panic, cancellation, empty output, and truncation tests.
- `internal/tools/workspace.go`: shared `os.Root`, path guard, safe file primitives, safe command working-directory resolution, and Close.
- `internal/tools/workspace_test.go`: absolute/volume/traversal/symlink escape and lifecycle tests.
- `internal/tools/read.go`: final `read` implementation and `ReadDetails`.
- `internal/tools/read_test.go`: final read protocol and pagination tests.
- `internal/tools/edit.go`: final batched `edit` implementation and `EditDetails`.
- `internal/tools/edit_test.go`: original-content matching, uniqueness, overlap, newline, mode, and single-write tests.
- `internal/tools/write.go`: final `write` implementation and `WriteDetails`.
- `internal/tools/write_test.go`: create/overwrite/idempotence tests.
- `internal/tools/process_supervisor.go`: session ownership, bounded stream log, stdin, action primitives, process-group termination, and Close.
- `internal/tools/process_supervisor_test.go`: concurrency, bounded log offsets, cleanup, and process-group tests.
- `internal/engine/tool_scheduler.go`: serial/parallel/mixed wave scheduling and stable result ordering.
- `internal/engine/tool_scheduler_test.go`: scheduler-only concurrency and order tests.
- `internal/engine/agent_loop.go`: Thinking/Action state machine, Provider calls, message history, events, and termination.
- `internal/engine/runtime.go`: application-facing AgentRuntime facade.
- `internal/engine/runtime_test.go`: preparation/loop delegation and cancellation tests.
- `internal/context/run_context.go`: `RunContextFactory` and initial message preparation.
- `internal/context/run_context_test.go`: skill discovery/read dependency and prompt-message tests.

### Rename and rewrite

- `internal/tools/read_file.go` -> `internal/tools/read.go`; `internal/tools/read_file_test.go` -> `internal/tools/read_test.go`.
- `internal/tools/edit_file.go` -> `internal/tools/edit.go`; `internal/tools/edit_file_test.go` -> `internal/tools/edit_test.go`.
- `internal/tools/write_file.go` -> `internal/tools/write.go`; `internal/tools/write_file_test.go` -> `internal/tools/write_test.go`.
- `internal/tools/process_manager.go` -> `internal/tools/process_supervisor.go`.
- `internal/engine/run_execution.go` -> `internal/engine/tool_scheduler.go`.
- `internal/engine/run_loop.go` -> `internal/engine/agent_loop.go`.

### Modify

- `go.mod`, `go.sum`: add only JSON Schema validation v6.0.2.
- `internal/schema/message.go`, `internal/schema/message_test.go`: RoleTool, block content, ToolName/IsError, ToolDefinition.Label.
- `internal/tools/registry.go`, `internal/tools/registry_test.go`: structured Registry pipeline and events.
- `internal/tools/apply_patch.go`, `internal/tools/apply_patch_parser.go`, `internal/tools/apply_patch_test.go`: shared Workspace and structured details.
- `internal/tools/exec.go`, `internal/tools/exec_test.go`: final camelCase protocol, streaming emitter, timeout/yield semantics.
- `internal/tools/process.go`, `internal/tools/process_test.go`, `internal/tools/process_command_test.go`: seven final process actions.
- `internal/tools/process_group_unix.go`, `internal/tools/process_group_windows.go`: preserve full process-group termination on all stop paths.
- `internal/tools/register.go`, `internal/tools/register_test.go`: Fx groups, shared resources, middleware, Registry, lifecycle.
- `internal/provider/openai.go`, `internal/provider/openai_test.go`, `internal/provider/claude.go`, `internal/provider/claude_test.go`: structured message mapping and invalid-message rejection.
- `internal/engine/reporter.go`, `internal/engine/reporter_test.go`: one `Report(context.Context, schema.AgentEvent)` interface and ordered panic-isolated fan-out.
- `internal/engine/terminal_reporter.go`, `internal/engine/terminal_reporter_test.go`: start/update/end/final rendering.
- `internal/engine/run_validation.go`, `internal/engine/run_diagnostics.go`, `internal/engine/loop_test.go`: adapt validation and loop coverage to block content and RoleTool.
- `internal/engine/register.go`, `internal/engine/register_test.go`: provide Scheduler, Loop, Runtime, terminal Reporter group, and ordered MultiReporter.
- `internal/context/composer.go`, `internal/context/composer_test.go`, `internal/context/skill_prompt.go`, `internal/context/skill_prompt_test.go`, `internal/context/register.go`, `internal/context/register_test.go`: block content, `read`, and RunContextFactory injection.
- `internal/dispatch/wecom.go`, `internal/dispatch/wecom_test.go`, `internal/dispatch/register.go`, `internal/dispatch/register_test.go`: event filtering and Reporter group contribution.
- `internal/app/runner.go`, `internal/app/runner_test.go`, `internal/app/register.go`, `internal/app/module_test.go`: depend only on AgentRuntime plus prompt.
- `internal/register.go`, `cmd/main.go`: aggregate and run the final graph.
- `tests/integration/engine_skill_tool_test.go`, `tests/integration/fx_dependency_graph_test.go`, `tests/integration/registry_lifecycle_test.go`, `tests/integration/reporter_dispatch_test.go`: final cross-package acceptance.
- `README.md`: six-tool protocol, runtime layering, events, and the shell non-sandbox warning.

### Delete after cutover

- `internal/tools/read_file.go`, `internal/tools/read_file_test.go`, `internal/tools/edit_file.go`, `internal/tools/edit_file_test.go`, `internal/tools/write_file.go`, `internal/tools/write_file_test.go`, `internal/tools/process_manager.go` after their renamed replacements are tracked.
- `internal/engine/engine.go`, `internal/engine/run_loop.go`, and `internal/engine/run_execution.go` after Runtime, AgentLoop, and ToolScheduler replace them.

---

### Task 1: Introduce structured content, Tool results, and Provider mappings

**Files:**
- Create: `internal/schema/content.go`
- Create: `internal/schema/event.go`
- Create: `internal/schema/event_test.go`
- Modify: `internal/schema/message.go`
- Modify: `internal/schema/message_test.go`
- Modify: `internal/provider/openai.go`
- Modify: `internal/provider/openai_test.go`
- Modify: `internal/provider/claude.go`
- Modify: `internal/provider/claude_test.go`
- Modify: `internal/context/composer.go`
- Modify: `internal/context/composer_test.go`
- Modify: `internal/engine/run_loop.go`
- Modify: `internal/engine/run_execution.go`
- Modify: `internal/engine/run_validation.go`
- Modify: `internal/engine/loop_test.go`
- Modify: `internal/engine/register_test.go`
- Modify: `internal/engine/reporter_test.go`
- Modify: `internal/tools/registry.go`
- Modify: `internal/tools/registry_test.go`
- Modify: `tests/integration/engine_skill_tool_test.go`

**Interfaces:**
- Consumes: existing `schema.ToolCall` and Provider SDK message builders.
- Produces: `schema.ContentBlock`, `schema.TextBlock(string)`, `schema.TextContent([]ContentBlock)`, `schema.RoleTool`, `schema.ToolOutput`, `schema.ToolUpdate`, `schema.ToolResult`, `schema.ToolEvent`, `schema.AgentEvent`, and event constructors.

- [ ] **Step 1: Write the failing content/message JSON tests**

Add table tests that require block JSON, RoleTool metadata, and Provider-native tool results:

```go
func TestToolMessageJSONContract(t *testing.T) {
    message := Message{
        Role: RoleTool, Content: []ContentBlock{TextBlock("denied")},
        ToolCallID: "call-1", ToolName: "read", IsError: true,
    }
    encoded, err := json.Marshal(message)
    if err != nil { t.Fatal(err) }
    const want = `{"role":"tool","content":[{"type":"text","text":"denied"}],"tool_call_id":"call-1","tool_name":"read","is_error":true}`
    if string(encoded) != want { t.Fatalf("json = %s", encoded) }
}

func TestAgentEventConstructorsSetDiscriminatedPayloads(t *testing.T) {
    call := ToolCall{ID: "call-1", Name: "exec", Arguments: json.RawMessage(`{"command":"pwd"}`)}
    start := NewToolStartEvent(call)
    if start.Type != AgentEventToolStart || start.Tool == nil || start.Tool.Phase != ToolEventStart {
        t.Fatalf("start = %#v", start)
    }
    message := NewMessageEvent(Message{Role: RoleAssistant, Content: []ContentBlock{TextBlock("done")}})
    if message.Type != AgentEventMessage || message.Message == nil || message.Tool != nil {
        t.Fatalf("message = %#v", message)
    }
}
```

- [ ] **Step 2: Write the failing Provider tool-result tests**

Add one fixture shared in shape by both Provider test files:

```go
message := schema.Message{
    Role: schema.RoleTool,
    Content: []schema.ContentBlock{schema.TextBlock("permission denied")},
    ToolCallID: "call-1",
    ToolName: "read",
    IsError: true,
}
```

Assert OpenAI receives `tool` + `tool_call_id`, Claude receives `tool_result` + `tool_use_id` + `is_error=true`, Details never appear in the encoded request, and invalid RoleTool messages without an ID are rejected.

- [ ] **Step 3: Run focused tests and verify the old string schema fails**

Run: `go test ./internal/schema ./internal/provider`

Expected: build failures for `ContentBlock`, `RoleTool`, event constructors, and slice-based `Message.Content`.

- [ ] **Step 4: Implement ContentBlock, Message, and text helpers**

Use these final types and helpers:

```go
type ContentType string
const ContentTypeText ContentType = "text"

type ContentBlock struct {
    Type ContentType `json:"type"`
    Text string      `json:"text"`
}

func TextBlock(text string) ContentBlock {
    return ContentBlock{Type: ContentTypeText, Text: text}
}

func TextContent(blocks []ContentBlock) (string, error) {
    var builder strings.Builder
    for _, block := range blocks {
        if block.Type != ContentTypeText { return "", fmt.Errorf("unsupported content type %q", block.Type) }
        builder.WriteString(block.Text)
    }
    return builder.String(), nil
}

type Message struct {
    Role Role `json:"role"`
    Content []ContentBlock `json:"content,omitempty"`
    ToolCalls []ToolCall `json:"tool_calls,omitempty"`
    ToolCallID string `json:"tool_call_id,omitempty"`
    ToolName string `json:"tool_name,omitempty"`
    IsError bool `json:"is_error,omitempty"`
}
```

- [ ] **Step 5: Implement Tool output/event types and ToolDefinition.Label**

Define `ToolOutput`, `ToolUpdate`, and `ToolResult` with `Content []ContentBlock` and `Details any`; ToolResult also carries `ToolCallID`, `ToolName`, and `IsError`. Define event constants `thinking`, `tool_start`, `tool_update`, `tool_end`, and `message`, plus Tool phases `start`, `update`, and `end`. Provide `NewToolStart(call) ToolEvent`, `NewToolUpdate(call, update) ToolEvent`, `NewToolEnd(call, result) ToolEvent`, `NewAgentToolEvent(event) AgentEvent`, `NewToolStartEvent(call) AgentEvent`, `NewToolUpdateEvent(call, update) AgentEvent`, `NewToolEndEvent(call, result) AgentEvent`, `NewThinkingEvent() AgentEvent`, and `NewMessageEvent(message) AgentEvent`. Constructors must be the only production call sites that assemble `AgentEvent`.

Extend ToolDefinition without changing its existing fields:

```go
type ToolDefinition struct {
    Name string `json:"name"`
    Label string `json:"label,omitempty"`
    Description string `json:"description"`
    InputSchema any `json:"input_schema"`
    ParallelSafe bool `json:"parallel_safe,omitempty"`
}
```

Add a JSON assertion that a non-empty Label serializes as `"label":"Read file"` and that an empty Label is omitted.

- [ ] **Step 6: Migrate current Context, Engine, Registry, and tests to block content**

Mechanically replace string message literals with `[]schema.ContentBlock{schema.TextBlock(text)}` and string reads with `schema.TextContent`; do not add a second legacy content field. Keep the current Registry interface during this task, but change it to populate `ToolResult.Content` from the existing Tool string and update its tests. This lets the whole repository compile without retaining `ToolResult.Output`; Task 3 replaces the Tool/Registry execution interfaces themselves.

- [ ] **Step 7: Implement strict Provider mappings**

Use one conversion helper in each adapter:

```go
text, err := schema.TextContent(message.Content)
if err != nil { return nil, fmt.Errorf("message content: %w", err) }

switch message.Role {
case schema.RoleTool:
    if message.ToolCallID == "" { return nil, errors.New("tool message requires tool_call_id") }
    // OpenAI: openai.ToolMessage(text, message.ToolCallID)
    // Claude: anthropic.NewToolResultBlock(message.ToolCallID, text, message.IsError)
case schema.RoleAssistant:
    if text == "" && len(message.ToolCalls) == 0 {
        return nil, errors.New("assistant message contains no content or tool calls")
    }
}
```

Keep `Details` outside `Message`, so neither Provider adapter has any path that can serialize it.

- [ ] **Step 8: Run focused schema/provider tests**

Run: `gofmt -w internal/schema internal/provider && go test ./internal/schema ./internal/provider`

Expected: PASS.

- [ ] **Step 9: Run the complete suite**

Run: `gofmt -w internal/schema internal/provider internal/context internal/engine tests/integration/engine_skill_tool_test.go && go test ./...`

Expected: PASS with all current behavior expressed through text blocks and genuine RoleTool messages.

- [ ] **Step 10: Commit the protocol cutover**

Commit only task paths:

```bash
git commit --only -- internal/schema internal/provider internal/context/composer.go internal/context/composer_test.go internal/engine/run_loop.go internal/engine/run_execution.go internal/engine/run_validation.go internal/engine/loop_test.go internal/engine/register_test.go internal/engine/reporter_test.go internal/tools/registry.go internal/tools/registry_test.go tests/integration/engine_skill_tool_test.go -m "refactor: add structured agent protocol"
```

---

### Task 2: Replace Reporter callbacks with ordered AgentEvent reporting

**Files:**
- Modify: `internal/engine/reporter.go`
- Modify: `internal/engine/reporter_test.go`
- Modify: `internal/engine/terminal_reporter.go`
- Modify: `internal/engine/terminal_reporter_test.go`
- Modify: `internal/engine/run_loop.go`
- Modify: `internal/engine/run_execution.go`
- Modify: `internal/dispatch/wecom.go`
- Modify: `internal/dispatch/wecom_test.go`
- Modify: `internal/dispatch/register.go`
- Modify: `internal/dispatch/register_test.go`
- Modify: `tests/integration/reporter_dispatch_test.go`

**Interfaces:**
- Consumes: Task 1 `schema.AgentEvent` constructors.
- Produces: `engine.Reporter.Report(context.Context, schema.AgentEvent)`, `engine.ReporterRegistration{Name, Order, Reporter}`, and deterministic `NewMultiReporter`.

- [ ] **Step 1: Write failing ordering and panic-isolation tests**

```go
func TestMultiReporterSortsAndIsolatesPanic(t *testing.T) {
    var got []string
    reporter := NewMultiReporter([]ReporterRegistration{
        {Name: "z", Order: 20, Reporter: reporterFunc(func(context.Context, schema.AgentEvent) { got = append(got, "z") })},
        {Name: "panic", Order: 10, Reporter: reporterFunc(func(context.Context, schema.AgentEvent) { panic("boom") })},
        {Name: "a", Order: 20, Reporter: reporterFunc(func(context.Context, schema.AgentEvent) { got = append(got, "a") })},
    })
    reporter.Report(context.Background(), schema.NewThinkingEvent())
    if diff := cmp.Diff([]string{"a", "z"}, got); diff != "" { t.Fatal(diff) }
}

type reporterFunc func(context.Context, schema.AgentEvent)
func (f reporterFunc) Report(ctx context.Context, event schema.AgentEvent) { f(ctx, event) }
```

- [ ] **Step 2: Write the failing Terminal event rendering test**

Use ordinary slice comparisons instead of adding `go-cmp`. Feed start, an `exec` update with `Details: map[string]any{"stream":"stderr", "bytes":4}`, successful end, failed end, and final message events; assert the writer contains the chunk and the correct status labels.

- [ ] **Step 3: Write the failing WeCom event filtering test**

Count requests in an `httptest.Server`; assert zero requests for thinking, tool_update, and successful tool_end, then one request each for tool_start, failed tool_end, and final assistant message.

- [ ] **Step 4: Run Reporter and Dispatch tests and verify callback APIs fail**

Run: `go test ./internal/engine ./internal/dispatch ./tests/integration -run 'Reporter|Terminal|WeCom'`

Expected: build failures because `Report`, `ReporterRegistration`, and event filtering do not exist.

- [ ] **Step 5: Implement the Reporter interface and registration type**

```go
type Reporter interface {
    Report(context.Context, schema.AgentEvent)
}

type ReporterRegistration struct {
    Name string
    Order int
    Reporter Reporter
}

func NewMultiReporter(registrations []ReporterRegistration) Reporter {
    filtered := append([]ReporterRegistration(nil), registrations...)
    slices.SortFunc(filtered, func(a, b ReporterRegistration) int {
        if order := cmp.Compare(a.Order, b.Order); order != 0 { return order }
        return cmp.Compare(a.Name, b.Name)
    })
    return &multiReporter{registrations: filtered}
}
```

- [ ] **Step 6: Implement deterministic, panic-isolated fan-out**

In `multiReporter.Report`, skip blank names/nil Reporters and wrap each individual call with a local deferred recovery so one panic cannot prevent later subscribers.

- [ ] **Step 7: Convert current Engine event call sites**

The current loop must call only:

```go
reporter.Report(ctx, schema.NewThinkingEvent())
reporter.Report(ctx, schema.NewToolStartEvent(call))
reporter.Report(ctx, schema.NewToolEndEvent(call, result))
reporter.Report(ctx, schema.NewMessageEvent(*actionResp))
```

Emit the message event only after confirming the Action response has no Tool Calls, so it represents the Agent's final response rather than intermediate tool-call narration.

- [ ] **Step 8: Implement Terminal and WeCom event switches**

Terminal switches on all five event types; it prints tool updates only when `event.Tool.Call.Name == "exec"`. WeCom switches on `tool_start`, failed `tool_end`, and assistant `message`, returning immediately for all other events.

- [ ] **Step 9: Verify Reporter migration**

Run: `gofmt -w internal/engine internal/dispatch tests/integration/reporter_dispatch_test.go && go test ./...`

Expected: PASS, with no `OnThinking`, `OnToolCall`, `OnToolResult`, or `OnMessage` symbols under `internal` or `tests`.

- [ ] **Step 10: Commit Reporter migration**

```bash
git commit --only -- internal/engine/reporter.go internal/engine/reporter_test.go internal/engine/terminal_reporter.go internal/engine/terminal_reporter_test.go internal/engine/run_loop.go internal/engine/run_execution.go internal/dispatch tests/integration/reporter_dispatch_test.go -m "refactor: unify agent lifecycle reporting"
```

---

### Task 3: Build the structured Registry, schema validation, and Middleware chain

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`
- Create: `internal/tools/tool.go`
- Create: `internal/tools/schema_validator.go`
- Create: `internal/tools/schema_validator_test.go`
- Create: `internal/tools/middleware.go`
- Create: `internal/tools/middleware_test.go`
- Modify: `internal/tools/registry.go`
- Modify: `internal/tools/registry_test.go`
- Modify: `internal/tools/read_file.go`
- Modify: `internal/tools/edit_file.go`
- Modify: `internal/tools/write_file.go`
- Modify: `internal/tools/apply_patch.go`
- Modify: `internal/tools/exec.go`
- Modify: `internal/tools/process.go`
- Modify: corresponding `internal/tools/*_test.go` files
- Modify: `internal/tools/register.go`
- Modify: `internal/tools/register_test.go`
- Modify: `tests/integration/registry_lifecycle_test.go`
- Modify: `internal/engine/run_execution.go`
- Modify: `internal/engine/loop_test.go`

**Interfaces:**
- Consumes: Task 1 Tool output/event types and Task 2 Reporter.
- Produces: final `tools.Tool`, `tools.UpdateEmitter`, `tools.ToolEventObserver`, `tools.Registry`, `tools.Handler`, `tools.Middleware`, `tools.MiddlewareRegistration`, and `tools.RegistryParams`.

- [ ] **Step 1: Add the JSON Schema v6.0.2 dependency**

Run: `go get github.com/santhosh-tekuri/jsonschema/v6@v6.0.2`

- [ ] **Step 2: Create the final-interface stub Tool**

```go
type stubTool struct {
    definition schema.ToolDefinition
    execute func(context.Context, json.RawMessage, UpdateEmitter) (schema.ToolOutput, error)
}

func (t *stubTool) Definition() schema.ToolDefinition { return t.definition }
func (t *stubTool) Execute(ctx context.Context, args json.RawMessage, emit UpdateEmitter) (schema.ToolOutput, error) {
    return t.execute(ctx, args, emit)
}
```

- [ ] **Step 3: Write failing schema-validation and Registry event tests**

Assert invalid JSON, missing required fields, wrong types, and unknown fields never call the Tool; emitted updates preserve call identity; ordinary errors return `IsError=true` with nil Go error; cancellation returns a non-nil Go error.

- [ ] **Step 4: Write failing Middleware behavior tests**

Assert middleware order is `Order` then `Name`, panic becomes an error result, empty output becomes `(no output)`, and content above 50 KiB is UTF-8-safe truncated.

- [ ] **Step 5: Run tools tests and verify final interfaces are missing**

Run: `go test ./internal/tools -run 'Registry|Middleware|Schema'`

Expected: build failures for `UpdateEmitter`, `MiddlewareRegistration`, `RegistryParams`, and the two-value Registry Execute result.

- [ ] **Step 6: Implement final Tool, Registry, and Middleware contracts**

```go
type UpdateEmitter func(schema.ToolUpdate)

type Tool interface {
    Definition() schema.ToolDefinition
    Execute(context.Context, json.RawMessage, UpdateEmitter) (schema.ToolOutput, error)
}

type ToolEventObserver func(context.Context, schema.ToolEvent)

type Registry interface {
    GetAvailableTools() []schema.ToolDefinition
    Execute(context.Context, schema.ToolCall, ToolEventObserver) (schema.ToolResult, error)
}

type Execution struct {
    Call schema.ToolCall
    Definition schema.ToolDefinition
    Tool Tool
    Observer ToolEventObserver
    ValidateArgs func(json.RawMessage) error
}

type Handler func(context.Context, Execution, UpdateEmitter) (schema.ToolOutput, error)
type Middleware func(Handler) Handler

type MiddlewareRegistration struct {
    Name string
    Order int
    Middleware Middleware
}

type RegistryParams struct {
    fx.In
    Tools []Tool `group:"agent_tools"`
    Middlewares []MiddlewareRegistration `group:"tool_middlewares"`
}
```

- [ ] **Step 7: Compile each Tool JSON Schema once**

For each definition, marshal `InputSchema`, decode it with `jsonschema.UnmarshalJSON`, register it under `urn:go-reagent:tool:<name>`, and compile once in `NewRegistry(params) (Registry, error)`. Validation decodes `call.Arguments` into `any`, rejects trailing JSON, and invokes `compiled.Validate(value)` before the concrete Tool.

- [ ] **Step 8: Implement and order the six built-in runtime stages**

Register the built-in runtime stages with stable metadata: `recovery` Order 10, `context` Order 20, `schema_validation` Order 30, `logging` Order 40, `output_limit` Order 50, and `event_forwarding` Order 60. Sort before composing in reverse so Order 10 is the outermost wrapper. The event-forwarding middleware wraps the concrete emitter and sends `schema.NewToolUpdate(execution.Call, update)` to `execution.Observer`; Registry itself sends start/end because those events surround the whole chain.

Recovery logs `debug.Stack()` internally and returns a generic ordinary error; Context checks `ctx.Err()` before and after the next Handler; logging records only tool name, call ID, phase, byte count, and status; output-limit truncates text on a valid UTF-8 boundary and records truncation in internal Details. It must never log full arguments, environment variables, or content.

- [ ] **Step 9: Implement Registry event and error normalization**

Registry execution order must be exact:

```go
if err := ctx.Err(); err != nil { return schema.ToolResult{}, err }
entry, ok := r.tools[call.Name]
if !ok { return errorResult(call, fmt.Errorf("tool %q is not registered", call.Name)), nil }
observe(ctx, schema.NewToolStart(call))
output, err := entry.handler(ctx, Execution{Call: call, Definition: entry.definition, Tool: entry.tool}, emit)
result := normalizeToolResult(call, output, err)
observe(ctx, schema.NewToolEnd(call, result))
if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) { return result, err }
return result, nil
```

Emit `tool_update` synchronously through `schema.NewToolUpdate(call, update)`. If an ordinary error has no output Content, use the error text as Content; if it has output Content, retain that Content and set `IsError=true`. For success, replace an empty Content slice with one `(no output)` block. Always populate ToolCallID and ToolName, retain Details, and cap the joined UTF-8 text at 50 KiB plus an explicit truncation marker.

- [ ] **Step 10: Adapt all six concrete Tools to the structured Execute signature**

Adapt all six concrete tools to the final `Execute` signature by wrapping their current string output in `schema.ToolOutput{Content: []schema.ContentBlock{schema.TextBlock(output)}}`; their final names and detailed payloads arrive in Tasks 4-8.

During this task, update `NewRuntimeRegistry` to call `NewRegistry(RegistryParams{Tools: tools, Middlewares: defaultMiddlewareRegistrations()})`; Task 11 replaces this explicit slice with Fx value groups. This is the only temporary assembly adapter and it is deleted in Task 11.

- [ ] **Step 11: Route Registry events to Reporter**

Remove Engine's direct start/end reporting and pass this observer into Registry:

```go
observer := func(ctx context.Context, event schema.ToolEvent) {
    if reporter != nil { reporter.Report(ctx, schema.NewAgentToolEvent(event)) }
}
result, err := registry.Execute(ctx, call, observer)
```

- [ ] **Step 12: Verify Registry and Middleware behavior**

Run: `gofmt -w internal/tools internal/engine && go test ./...`

Expected: PASS; start/update/end are emitted exactly once by Registry, and only cancellation/deadline interrupts Engine.

- [ ] **Step 13: Commit the structured Tool Runtime core**

```bash
git commit --only -- go.mod go.sum internal/tools internal/engine/run_execution.go internal/engine/loop_test.go tests/integration/registry_lifecycle_test.go -m "refactor: add structured tool registry"
```

---

### Task 4: Add the shared Workspace and migrate `read`/`write`

**Files:**
- Create: `internal/tools/workspace.go`
- Create: `internal/tools/workspace_test.go`
- Rename: `internal/tools/read_file.go` -> `internal/tools/read.go`
- Rename: `internal/tools/read_file_test.go` -> `internal/tools/read_test.go`
- Rename: `internal/tools/write_file.go` -> `internal/tools/write.go`
- Rename: `internal/tools/write_file_test.go` -> `internal/tools/write_test.go`
- Modify: `internal/tools/register.go`
- Modify: `internal/tools/register_test.go`
- Modify: `internal/context/skill_prompt.go`
- Modify: `internal/context/skill_prompt_test.go`

**Interfaces:**
- Consumes: `config.WorkDir`, Fx lifecycle, Task 3 Tool interface.
- Produces: `tools.Workspace`, `NewWorkspace`, `NewReadTool`, `ReadDetails`, `NewWriteTool`, and `WriteDetails`.

- [ ] **Step 1: Write failing Workspace escape tests**

Test `"/tmp/x"`, `"../x"`, `"C:\\x"`, `"\\\\server\\share"`, and an in-root symlink to an outside path against every relevant Workspace primitive.

- [ ] **Step 2: Write failing final `read` protocol tests**

Require `read.Definition().Name == "read"`, `ParallelSafe=true`, UTF-8/NUL/regular-file checks, 2000-line and 50 KiB pagination, and Details:

```go
type ReadDetails struct {
    Path string `json:"path"`
    Lines int `json:"lines"`
    Bytes int `json:"bytes"`
    Truncated bool `json:"truncated"`
    NextOffset int `json:"nextOffset,omitempty"`
}

type WriteDetails struct {
    Path string `json:"path"`
    Bytes int `json:"bytes"`
    Changed bool `json:"changed"`
}
```

- [ ] **Step 3: Write failing final `write` protocol tests**

Assert parent creation, full overwrite, same-content `Changed=false`, unchanged file mode on overwrite, invalid UTF-8/NUL rejection, and outside-symlink rejection.

- [ ] **Step 4: Run focused tests and verify old per-tool roots/names fail**

Run: `go test ./internal/tools -run 'Workspace|Read|Write'`

Expected: failures because Workspace and final constructors/names do not exist.

- [ ] **Step 5: Implement Workspace construction and lifecycle**

```go
type Workspace struct {
    path string
    root *os.Root
    closeOnce sync.Once
    closeErr error
}

func NewWorkspace(lifecycle fx.Lifecycle, workDir config.WorkDir) (*Workspace, error)
func (w *Workspace) Open(path string) (*os.File, error)
func (w *Workspace) OpenFile(path string, flag int, perm fs.FileMode) (*os.File, error)
func (w *Workspace) ReadFile(path string) ([]byte, error)
func (w *Workspace) MkdirAll(path string, perm fs.FileMode) error
func (w *Workspace) Remove(path string) error
func (w *Workspace) Rename(oldPath, newPath string) error
func (w *Workspace) ResolveDir(path string) (string, error)
func (w *Workspace) Close() error
```

- [ ] **Step 6: Implement the shared path guard and file primitives**

Every public method first calls one `cleanRelativePath`: trim, reject empty where the operation requires a file, reject `filepath.IsAbs`, `filepath.VolumeName`, drive-letter prefixes, UNC prefixes, and cleaned `..` prefixes. `os.Root` performs the final symlink-safe operation.

- [ ] **Step 7: Implement safe command-directory resolution**

`ResolveDir` evaluates the target, verifies it remains under `w.path` with `filepath.Rel`, and requires an existing directory.

- [ ] **Step 8: Rename and migrate `read` without a Tool Close method**

Construct the tool from `*Workspace`. Return page text and `ReadDetails`; keep inputs `path/offset/limit` and the exact continuation marker.

- [ ] **Step 9: Rename and migrate `write` without a Tool Close method**

Construct the tool from `*Workspace`. Validate UTF-8/NUL, compare existing bytes, write only on change, and return `WriteDetails`; inputs remain `path/content`.

- [ ] **Step 10: Update registration and Skill instructions**

Change Skill instructions to say `read` and keep the exact continuation marker `Use offset=N to continue`.

- [ ] **Step 11: Verify final read/write behavior**

Run: `gofmt -w internal/tools internal/context && go test ./internal/tools ./internal/context ./tests/integration`

Expected: PASS; Registry contains `read` and `write`, and no concrete file tool owns an `os.Root`.

- [ ] **Step 12: Commit Workspace/read/write migration**

```bash
git commit --only -- internal/tools/workspace.go internal/tools/workspace_test.go internal/tools/read.go internal/tools/read_test.go internal/tools/write.go internal/tools/write_test.go internal/tools/read_file.go internal/tools/read_file_test.go internal/tools/write_file.go internal/tools/write_file_test.go internal/tools/register.go internal/tools/register_test.go internal/context/skill_prompt.go internal/context/skill_prompt_test.go -m "refactor: share guarded workspace tools"
```

---

### Task 5: Upgrade `edit` to atomic batched replacements

**Files:**
- Rename: `internal/tools/edit_file.go` -> `internal/tools/edit.go`
- Rename: `internal/tools/edit_file_test.go` -> `internal/tools/edit_test.go`
- Modify: `internal/tools/register.go`
- Modify: `internal/tools/register_test.go`

**Interfaces:**
- Consumes: Task 4 Workspace and existing tolerant unique-match helpers.
- Produces: `NewEditTool(*Workspace) *EditTool`, final `edit` schema, `EditOperation`, and `EditDetails`.

- [ ] **Step 1: Write failing final input/output protocol tests**

```go
type EditOperation struct {
    OldText string `json:"oldText"`
    NewText *string `json:"newText"`
}

type EditDetails struct {
    Diff string `json:"diff"`
    Patch string `json:"patch"`
    Replacements int `json:"replacements"`
    FirstChangedLine int `json:"firstChangedLine"`
}
```

- [ ] **Step 2: Write failing original-content and uniqueness tests**

Test two non-overlapping edits against original content, a second edit that would match only the first edit's replacement, duplicate/non-unique oldText, and missing `newText`.

- [ ] **Step 3: Write failing overlap and one-write tests**

Test overlapping/nested ranges, CRLF preservation, permission preservation, and one preflight failure leaving bytes and mtime unchanged.

- [ ] **Step 4: Run edit tests and verify the single-replacement protocol fails**

Run: `go test ./internal/tools -run Edit`

Expected: failures for tool name `edit`, `edits[]`, camelCase fields, and overlap rejection.

- [ ] **Step 5: Implement final `path + edits[]` decoding and schema**

Decode with `DisallowUnknownFields`, require a non-empty `edits` array, require every `oldText`, and use `*string` for every required-but-possibly-empty `newText`.

- [ ] **Step 6: Plan all unique matches against original content**

Calculate every match against the same `originalContent`:

```go
matches := make([]plannedEdit, len(input.Edits))
for i, edit := range input.Edits {
    match, err := findUniqueTextMatch(originalContent, edit.OldText)
    if err != nil { return schema.ToolOutput{}, fmt.Errorf("edits[%d]: %w", i, err) }
    matches[i] = plannedEdit{index: i, match: match, replacement: replacementForMatch(originalContent, match, *edit.NewText)}
}
slices.SortFunc(matches, func(a, b plannedEdit) int { return cmp.Compare(a.match.start, b.match.start) })
for i := 1; i < len(matches); i++ {
    if matches[i].match.start < matches[i-1].match.end { return schema.ToolOutput{}, errors.New("edits 包含重叠或嵌套范围") }
}
for i := len(matches)-1; i >= 0; i-- {
    match := matches[i]
    updated = updated[:match.match.start] + match.replacement + updated[match.match.end:]
}
```

- [ ] **Step 7: Commit the planned edits with one file write**

Seek once, write all bytes once, truncate once, and preserve the original file descriptor/mode.

- [ ] **Step 8: Build EditDetails diff and patch**

Build `Diff` as a unified `--- a/<path>` / `+++ b/<path>` line diff and `Patch` as a valid `*** Begin Patch` / `*** Update File` patch for the final content.

- [ ] **Step 9: Return structured output and update tool registration**

Return human-readable content `Applied <N> edits to <path>` plus `EditDetails`. Set `FirstChangedLine` by counting newlines before the smallest original start offset. The definition must require `path` and `edits`, require `oldText/newText` inside every item, set both object levels to `additionalProperties:false`, and keep `ParallelSafe=false`.

- [ ] **Step 10: Verify edit migration**

Run: `gofmt -w internal/tools && go test ./internal/tools ./tests/integration`

Expected: PASS with no `old_text`, `new_text`, `EditFileTool`, or `edit_file` production symbols.

- [ ] **Step 11: Commit edit migration**

```bash
git commit --only -- internal/tools/edit.go internal/tools/edit_test.go internal/tools/edit_file.go internal/tools/edit_file_test.go internal/tools/register.go internal/tools/register_test.go -m "refactor: add batched edit tool"
```

---

### Task 6: Move `apply_patch` onto Workspace and structured Details

**Files:**
- Modify: `internal/tools/apply_patch.go`
- Modify: `internal/tools/apply_patch_parser.go`
- Modify: `internal/tools/apply_patch_test.go`
- Modify: `internal/tools/register.go`

**Interfaces:**
- Consumes: Task 4 Workspace and Task 3 Tool output.
- Produces: `NewApplyPatchTool(*Workspace) *ApplyPatchTool` and `ApplyPatchDetails{Operations, Files}`.

- [ ] **Step 1: Add failing structured Details tests**

```go
type ApplyPatchDetails struct {
    Operations int `json:"operations"`
    Files []string `json:"files"`
}
```

- [ ] **Step 2: Add failing operation and preflight tests**

Require Add/Update/Delete/Move and multi-file patches, sorted unique affected files, and malformed syntax/context/path conflict causing zero disk writes.

- [ ] **Step 3: Add failing Workspace-boundary tests**

Require path traversal/symlink rejection through Workspace and no Tool-level Close method.

- [ ] **Step 4: Run apply_patch tests and verify per-tool Root behavior fails**

Run: `go test ./internal/tools -run ApplyPatch`

Expected: failures for the Workspace constructor and structured Details.

- [ ] **Step 5: Replace direct `os.Root` ownership with Workspace primitives**

Keep the existing parse and in-memory staging sequence, but change all loads/commits to `Workspace.ReadFile`, `MkdirAll`, `OpenFile`, `Remove`, and `Rename`. Do not write any staged operation until every operation has parsed, loaded, and applied successfully in memory.

```go
func NewApplyPatchTool(workspace *Workspace) *ApplyPatchTool {
    return &ApplyPatchTool{workspace: workspace}
}
```

- [ ] **Step 6: Preserve full in-memory preflight before commits**

Keep parse, path validation, context matching, move-conflict checking, and staged content generation ahead of the first Workspace mutation.

- [ ] **Step 7: Return structured summary without claiming filesystem atomicity**

Return `Applied patch: <N> operation(s) across <M> file(s)` and `ApplyPatchDetails`. Preserve the documented limitation that commit-stage I/O failure can partially complete a multi-file patch; only preflight failure guarantees no writes.

- [ ] **Step 8: Verify apply_patch migration**

Run: `gofmt -w internal/tools && go test ./internal/tools -run ApplyPatch && go test ./...`

Expected: PASS and one shared Workspace owns all file handles.

- [ ] **Step 9: Commit apply_patch migration**

```bash
git commit --only -- internal/tools/apply_patch.go internal/tools/apply_patch_parser.go internal/tools/apply_patch_test.go internal/tools/register.go -m "refactor: structure apply patch output"
```

---

### Task 7: Replace ProcessManager with a concurrency-safe ProcessSupervisor

**Files:**
- Rename: `internal/tools/process_manager.go` -> `internal/tools/process_supervisor.go`
- Create: `internal/tools/process_supervisor_test.go`
- Modify: `internal/tools/process_group_unix.go`
- Modify: `internal/tools/process_group_windows.go`
- Modify: `internal/tools/process_command_test.go`
- Modify: `internal/tools/register.go`

**Interfaces:**
- Consumes: Task 4 `Workspace.ResolveDir` and Fx lifecycle.
- Produces: `ProcessSupervisor`, `ProcessStart`, `ProcessSnapshot`, `ProcessLog`, and session primitives used only by exec/process.

- [ ] **Step 1: Write failing stream and bounded-log tests**

Use a real short-lived Go/shell child process to cover stdout/stderr separation and a retained 50 KiB combined log with monotonic absolute offsets.

```go
type ProcessStart struct {
    Command string
    WorkDir string
    Env map[string]string
    Timeout time.Duration
    OnOutput func(stream string, chunk []byte)
}

type ProcessLog struct {
    Content string `json:"content"`
    Offset int64 `json:"offset"`
    NextOffset int64 `json:"nextOffset"`
    Truncated bool `json:"truncated"`
}

type ProcessSnapshot struct {
    SessionID string `json:"sessionId"`
    Status string `json:"status"`
    Command string `json:"command"`
    CWD string `json:"cwd"`
    Output string `json:"-"`
    ExitCode *int `json:"exitCode,omitempty"`
    Truncated bool `json:"truncated"`
}
```

- [ ] **Step 2: Write failing session action concurrency tests**

Cover concurrent `List/Poll/Log/Write/Kill/Clear/Remove` under `go test -race` and assert deterministic record retention/removal semantics.

- [ ] **Step 3: Write failing timeout, process-group, and Close tests**

Use real short-lived Go/shell child processes to cover timeout, Context cancellation, child process-group termination, idempotent Close, and Fx Stop clearing all records.

- [ ] **Step 4: Run supervisor tests and verify the Manager lacks final primitives**

Run: `go test ./internal/tools -run 'ProcessSupervisor|ProcessGroup|ProcessLog'`

Expected: failures for stream callbacks, log offsets, Clear, Remove, and Workspace-backed workdir resolution.

- [ ] **Step 5: Implement ProcessSupervisor state and public primitives**

```go
type ProcessSupervisor struct {
    workspace *Workspace
    mu sync.RWMutex
    sessions map[string]*processSession
    nextID uint64
    closed bool
}

func NewProcessSupervisor(lifecycle fx.Lifecycle, workspace *Workspace) (*ProcessSupervisor, error)
func (s *ProcessSupervisor) Start(context.Context, ProcessStart) (*processSession, error)
func (s *ProcessSupervisor) List() []ProcessSnapshot
func (s *ProcessSupervisor) Poll(context.Context, string, time.Duration) (ProcessSnapshot, error)
func (s *ProcessSupervisor) Log(string, int64, int) (ProcessLog, error)
func (s *ProcessSupervisor) Write(context.Context, string, *string, bool) (ProcessSnapshot, error)
func (s *ProcessSupervisor) Kill(context.Context, string) (ProcessSnapshot, error)
func (s *ProcessSupervisor) Clear() int
func (s *ProcessSupervisor) Remove(context.Context, string) error
func (s *ProcessSupervisor) Close() error
```

- [ ] **Step 6: Implement distinct stdout/stderr writers**

Attach distinct writers to `cmd.Stdout` and `cmd.Stderr`; each writer appends to the shared bounded log under a lock and then calls `OnOutput(stream, clonedChunk)` outside the lock.

- [ ] **Step 7: Implement bounded logs with absolute offsets**

Keep absolute `baseOffset/endOffset` so pagination remains meaningful after old bytes are evicted.

- [ ] **Step 8: Make every termination path kill the process group**

Timeout, Context cancellation, Kill, Remove, and Close must all call the existing OS-specific `killProcessGroup`. `Kill` retains the record, `Clear` removes only finished records, `Remove` kills if running and then deletes, and Close waits for termination before clearing the map.

- [ ] **Step 9: Run race-focused tests**

Run: `gofmt -w internal/tools && go test -race ./internal/tools -run 'ProcessSupervisor|ProcessGroup|ProcessLog'`

Expected: PASS with no data races and no remaining `ProcessManager` symbol.

- [ ] **Step 10: Commit ProcessSupervisor migration**

```bash
git commit --only -- internal/tools/process_supervisor.go internal/tools/process_supervisor_test.go internal/tools/process_manager.go internal/tools/process_group_unix.go internal/tools/process_group_windows.go internal/tools/process_command_test.go internal/tools/register.go -m "refactor: supervise background processes"
```

---

### Task 8: Implement final `exec` streaming and seven-action `process`

**Files:**
- Modify: `internal/tools/exec.go`
- Modify: `internal/tools/exec_test.go`
- Modify: `internal/tools/process.go`
- Modify: `internal/tools/process_test.go`
- Modify: `internal/tools/process_command_test.go`
- Modify: `internal/tools/register.go`
- Modify: `internal/tools/register_test.go`

**Interfaces:**
- Consumes: Task 7 ProcessSupervisor and Task 3 UpdateEmitter.
- Produces: approved exec/process input schemas, `ExecDetails`, `ProcessDetails`, and stdout/stderr ToolUpdate payloads.

- [ ] **Step 1: Write failing final exec schema/default tests**

Assert exact exec properties `command/workdir/env/yieldMs/background/timeout`, default `yieldMs=10000`, default timeout 120 seconds, maximum timeout 600 seconds, foreground completion, yield-to-background, explicit background, non-zero exit `IsError=true`, timeout/process-group kill, and stream updates:

```go
type StreamDetails struct {
    Stream string `json:"stream"`
    Bytes int `json:"bytes"`
}

type ExecDetails struct {
    Status string `json:"status"`
    SessionID string `json:"sessionId,omitempty"`
    ExitCode *int `json:"exitCode,omitempty"`
    Command string `json:"command"`
    CWD string `json:"cwd"`
    Truncated bool `json:"truncated"`
}
```

- [ ] **Step 2: Write failing exec streaming/lifecycle tests**

Assert foreground completion, yield-to-background, explicit background, non-zero exit `IsError=true`, timeout/process-group kill, stdout/stderr updates, and no updates after tool_end.

- [ ] **Step 3: Write failing final process action/schema tests**

Assert process actions `list/poll/log/write/kill/clear/remove`, camelCase `sessionId`, poll `timeout` 0..30000 milliseconds, log `offset/limit`, write `data/eof`, and rejection of `session_id`, `wait_ms`, and unknown actions.

- [ ] **Step 4: Run exec/process tests and verify old snake_case/action set fails**

Run: `go test ./internal/tools -run 'Exec|Process'`

Expected: failures for `yieldMs`, seconds-based timeout, seven actions, streaming, and final Details.

- [ ] **Step 5: Implement final exec argument decoding and JSON Schema**

```go
type execArgs struct {
    Command string `json:"command"`
    WorkDir string `json:"workdir,omitempty"`
    Env map[string]string `json:"env,omitempty"`
    YieldMS *int `json:"yieldMs,omitempty"`
    Background bool `json:"background,omitempty"`
    Timeout *int `json:"timeout,omitempty"`
}
```

- [ ] **Step 6: Implement exec foreground/background timing**

Apply seconds to `timeout`, milliseconds to `yieldMs`, start background immediately when requested, and return a running session after the yield timer fires.

- [ ] **Step 7: Implement gated exec streaming and error semantics**

Pass an `OnOutput` callback to Supervisor that calls `emit(schema.ToolUpdate{Content: []schema.ContentBlock{schema.TextBlock(string(chunk))}, Details: StreamDetails{Stream: stream, Bytes: len(chunk)}})` only while an `atomic.Bool` foreground gate is true. Explicit background starts with the gate false; a yielded command sets it false before returning, preventing updates after `tool_end`. If the session finishes before yield, return retained output and ExecDetails; if it remains running at yield, return the current snapshot/session ID as success. For a finished non-zero exit or tool-level timeout, return the output plus an ordinary error so Registry preserves Content and sets IsError; only parent Context cancellation/deadline is a control-flow error.

- [ ] **Step 8: Implement final process argument decoding and JSON Schema**

```go
type processArgs struct {
    Action string `json:"action"`
    SessionID string `json:"sessionId,omitempty"`
    Timeout *int `json:"timeout,omitempty"`
    Offset *int64 `json:"offset,omitempty"`
    Limit *int `json:"limit,omitempty"`
    Data *string `json:"data,omitempty"`
    EOF bool `json:"eof,omitempty"`
}

type ProcessDetails struct {
    Action string `json:"action"`
    SessionID string `json:"sessionId,omitempty"`
    Status string `json:"status,omitempty"`
    ExitCode *int `json:"exitCode,omitempty"`
    Offset int64 `json:"offset,omitempty"`
    NextOffset int64 `json:"nextOffset,omitempty"`
    Truncated bool `json:"truncated,omitempty"`
    Removed int `json:"removed,omitempty"`
    Sessions []ProcessSnapshot `json:"sessions,omitempty"`
}
```

- [ ] **Step 9: Implement all seven process action branches**

`list` returns snapshots; `poll` waits up to `timeout`; `log` returns the retained page and next offset; `write` requires data or eof; `kill` retains; `clear` returns removed count; `remove` terminates and deletes. Return `ProcessDetails{Action, SessionID, Status, ExitCode, Offset, NextOffset, Truncated}` and concise text content for every action.

- [ ] **Step 10: Verify stream/event/process behavior**

Run: `gofmt -w internal/tools && go test -race ./internal/tools -run 'Exec|Process|Registry' && go test ./...`

Expected: PASS; only exec invokes the emitter, and all old snake_case fields fail JSON Schema validation.

- [ ] **Step 11: Commit exec/process migration**

```bash
git commit --only -- internal/tools/exec.go internal/tools/exec_test.go internal/tools/process.go internal/tools/process_test.go internal/tools/process_command_test.go internal/tools/register.go internal/tools/register_test.go -m "refactor: finalize exec and process protocols"
```

---

### Task 9: Extract the pure ToolScheduler

**Files:**
- Rename: `internal/engine/run_execution.go` -> `internal/engine/tool_scheduler.go`
- Create: `internal/engine/tool_scheduler_test.go`
- Modify: `internal/engine/loop_test.go`
- Modify: `internal/engine/engine.go`

**Interfaces:**
- Consumes: `tools.Registry.Execute(ctx, call, observer) (schema.ToolResult, error)` and ToolDefinition.ParallelSafe.
- Produces: `NewToolScheduler(registry tools.Registry, maxParallel int) *ToolScheduler`, `Schedule`, and `Mode`.

- [ ] **Step 1: Write scheduler serial and exclusive-barrier tests**

Use a controlled Registry that blocks calls on channels. Verify consecutive read calls share a wave and every non-read call is an exclusive barrier.

```go
func (s *ToolScheduler) Schedule(
    ctx context.Context,
    calls []schema.ToolCall,
    definitions []schema.ToolDefinition,
    observer tools.ToolEventObserver,
) ([]schema.ToolResult, error)

func (s *ToolScheduler) Mode(calls []schema.ToolCall, definitions []schema.ToolDefinition) string
```

- [ ] **Step 2: Write scheduler limit/order/cancellation tests**

Verify maximum active calls never exceeds the configured limit, completion order may differ, result slice order always matches input, and cancellation returns without starting later waves.

- [ ] **Step 3: Run scheduler tests and verify methods remain coupled to AgentEngine**

Run: `go test ./internal/engine -run ToolScheduler`

Expected: build failures for `NewToolScheduler`, `Schedule`, and `Mode`.

- [ ] **Step 4: Implement definition lookup and wave partitioning**

Build a `map[string]bool` from definitions and scan calls into consecutive safe waves and one-call exclusive waves.

- [ ] **Step 5: Implement bounded parallel wave execution**

Use a semaphore for safe waves, assign results by original index, and return the first control-flow error after all already-started goroutines exit. Scheduler must not parse arguments, construct error Content, manage processes, translate Provider messages, or call Reporter directly.

- [ ] **Step 6: Delegate current loop execution to ToolScheduler**

Replace `AgentEngine.executeToolCalls`, `executeParallelWave`, and `executeToolCall` with one scheduler call and map each result to:

```go
schema.Message{
    Role: schema.RoleTool,
    Content: result.Content,
    ToolCallID: result.ToolCallID,
    ToolName: result.ToolName,
    IsError: result.IsError,
}
```

The observer wraps ToolEvent into AgentEvent and reports it; updates are never appended to history.

- [ ] **Step 7: Verify scheduling**

Run: `gofmt -w internal/engine && go test -race ./internal/engine`

Expected: PASS for serial, parallel, mixed, limit, cancellation, and stable order.

- [ ] **Step 8: Commit ToolScheduler extraction**

```bash
git commit --only -- internal/engine/tool_scheduler.go internal/engine/tool_scheduler_test.go internal/engine/run_execution.go internal/engine/loop_test.go internal/engine/engine.go -m "refactor: extract tool scheduler"
```

---

### Task 10: Split RunContextFactory, AgentLoop, and AgentRuntime

**Files:**
- Create: `internal/context/run_context.go`
- Create: `internal/context/run_context_test.go`
- Modify: `internal/context/register.go`
- Modify: `internal/context/register_test.go`
- Rename: `internal/engine/run_loop.go` -> `internal/engine/agent_loop.go`
- Create: `internal/engine/runtime.go`
- Create: `internal/engine/runtime_test.go`
- Modify: `internal/engine/run_validation.go`
- Modify: `internal/engine/run_diagnostics.go`
- Modify: `internal/engine/loop_test.go`
- Delete: `internal/engine/engine.go`

**Interfaces:**
- Consumes: PromptComposer, SkillLoader, Provider, Registry, ToolScheduler, and Reporter.
- Produces: `context.RunContextFactory.Create`, `engine.AgentLoop.Run`, and application-facing `engine.AgentRuntime.Run`.

- [ ] **Step 1: Write failing RunContextFactory tests**

Require skill discovery, diagnostics, `read` dependency checking, system/user initial messages, and cancellation.

```go
type RunContext struct {
    Messages []schema.Message
    Tools []schema.ToolDefinition
}

type RunContextFactory struct { composer *PromptComposer; skillLoader *SkillLoader }
func NewRunContextFactory(composer *PromptComposer, skillLoader *SkillLoader) *RunContextFactory
func (f *RunContextFactory) Create(context.Context, string, []schema.ToolDefinition) (RunContext, error)

type AgentRuntime interface {
    Run(context.Context, string) error
}
```

- [ ] **Step 2: Write failing AgentLoop tests**

Prove Thinking uses no tools, Action receives sorted definitions, final assistant content reports one message event, Tool results become RoleTool history, and cancellation stops.

- [ ] **Step 3: Write failing AgentRuntime facade tests**

Use factory and loop fakes to prove preparation occurs exactly once before loop execution and preparation errors prevent the loop call.

- [ ] **Step 4: Run Context/Engine tests and verify responsibilities are still coupled**

Run: `go test ./internal/context ./internal/engine -run 'RunContext|AgentLoop|AgentRuntime'`

Expected: build failures for the three new runtime boundaries.

- [ ] **Step 5: Implement RunContextFactory skill discovery and `read` prerequisite**

Move skill discovery and diagnostics from Engine into `Create`. If the discovered snapshot has skills and definitions lack `read`, return `发现可用 Agent Skills，但 Registry 未挂载 read`.

- [ ] **Step 6: Implement RunContextFactory initial messages**

Compose the system prompt and return system/user messages as text blocks plus a cloned definition slice.

- [ ] **Step 7: Implement AgentLoop state and Provider phases**

```go
type AgentLoop struct {
    provider provider.LLMProvider
    scheduler *ToolScheduler
    enableThinking bool
}

func NewAgentLoop(p provider.LLMProvider, scheduler *ToolScheduler, enableThinking bool) *AgentLoop
func (l *AgentLoop) Run(ctx context.Context, runContext ctxpkg.RunContext, reporter Reporter) error
```

Move Thinking/Action state, validation, history, event delivery, and termination into AgentLoop; delegate tool calls only through Scheduler.

- [ ] **Step 8: Implement the thin Runtime facade**

```go

type runtime struct {
    factory *ctxpkg.RunContextFactory
    loop *AgentLoop
    registry tools.Registry
    reporter Reporter
}

func NewAgentRuntime(factory *ctxpkg.RunContextFactory, loop *AgentLoop, registry tools.Registry, reporter Reporter) AgentRuntime
func (r *runtime) Run(ctx context.Context, prompt string) error
```

Runtime obtains definitions from Registry, calls factory once, then calls Loop.

- [ ] **Step 9: Enforce final-only message events and delete AgentEngine**

When an Action response contains Tool Calls, append it to history but do not emit an Agent message event; emit `NewMessageEvent` only for the no-Tool-Call response that terminates the loop. Delete AgentEngine and do not retain an adapter constructor.

- [ ] **Step 10: Verify the runtime split**

Run: `gofmt -w internal/context internal/engine && go test ./internal/context ./internal/engine ./tests/integration`

Expected: PASS, with no business caller invoking `NewPromptComposer`, `NewSkillLoader`, or any concrete Tool constructor.

- [ ] **Step 11: Commit the runtime split**

```bash
git commit --only -- internal/context internal/engine/agent_loop.go internal/engine/runtime.go internal/engine/runtime_test.go internal/engine/run_loop.go internal/engine/run_validation.go internal/engine/run_diagnostics.go internal/engine/loop_test.go internal/engine/engine.go -m "refactor: split agent runtime responsibilities"
```

---

### Task 11: Wire Tools, Middleware, Reporters, and Runtime through Fx groups

**Files:**
- Modify: `internal/tools/register.go`
- Modify: `internal/tools/register_test.go`
- Modify: `internal/context/register.go`
- Modify: `internal/context/register_test.go`
- Modify: `internal/engine/register.go`
- Modify: `internal/engine/register_test.go`
- Modify: `internal/dispatch/register.go`
- Modify: `internal/dispatch/register_test.go`
- Modify: `internal/app/runner.go`
- Modify: `internal/app/runner_test.go`
- Modify: `internal/app/register.go`
- Modify: `internal/app/module_test.go`
- Modify: `internal/register.go`
- Modify: `cmd/main.go`
- Modify: `tests/integration/fx_dependency_graph_test.go`
- Modify: `tests/integration/registry_lifecycle_test.go`

**Interfaces:**
- Consumes: Tasks 3-10 final constructors.
- Produces: complete `internal.Register`, Fx value groups `agent_tools`, `tool_middlewares`, `reporters`, and an AgentRunner that depends only on AgentRuntime and Prompt.

- [ ] **Step 1: Write failing Fx graph population tests**

Populate `engine.AgentRuntime`, `tools.Registry`, `*tools.Workspace`, and `*tools.ProcessSupervisor` from `internal.Register`. Assert definitions are exactly sorted as `apply_patch, edit, exec, process, read, write`.

- [ ] **Step 2: Write failing duplicate and stable-order group tests**

Assert duplicate Tool names fail app construction and reversed group input still produces middleware/reporter Order+Name order.

- [ ] **Step 3: Write failing Fx lifecycle cleanup test**

Start a long-running background child, stop Fx, and assert the process group exits, Supervisor sessions clear, and Workspace operations report closed.

- [ ] **Step 4: Run registration/integration tests and verify old direct assembly fails**

Run: `go test ./internal/tools ./internal/context ./internal/engine ./internal/dispatch ./internal/app ./tests/integration -run 'Register|DependencyGraph|Lifecycle'`

Expected: failures because groups and AgentRuntime wiring are not complete.

- [ ] **Step 5: Register six Tools into `agent_tools`**

Use Fx annotations of this exact shape:

```go
fx.Annotate(NewReadTool, fx.As(new(Tool)), fx.ResultTags(`group:"agent_tools"`))
fx.Annotate(NewEditTool, fx.As(new(Tool)), fx.ResultTags(`group:"agent_tools"`))
fx.Annotate(NewWriteTool, fx.As(new(Tool)), fx.ResultTags(`group:"agent_tools"`))
fx.Annotate(NewApplyPatchTool, fx.As(new(Tool)), fx.ResultTags(`group:"agent_tools"`))
fx.Annotate(NewExecTool, fx.As(new(Tool)), fx.ResultTags(`group:"agent_tools"`))
fx.Annotate(NewProcessTool, fx.As(new(Tool)), fx.ResultTags(`group:"agent_tools"`))
```

- [ ] **Step 6: Register shared resources and `tool_middlewares`**

Provide Workspace before ProcessSupervisor; each constructor appends its own lifecycle hook so Fx reverse stop order closes Supervisor first. Provide the six Task 3 `MiddlewareRegistration` values into `tool_middlewares`, then provide `NewRegistry(RegistryParams)`.

- [ ] **Step 7: Register Terminal and optional WeCom into `reporters`**

Engine contributes terminal registration `{Name:"terminal", Order:100}`. Dispatch contributes zero or one WeCom registration `{Name:"wecom", Order:200}` through a flattened group:

```go
fx.Annotate(NewReporterRegistrations, fx.ResultTags(`group:"reporters,flatten"`))
```

- [ ] **Step 8: Register MultiReporter, Scheduler, Loop, and Runtime**

Engine collects `[]ReporterRegistration` with `fx.In`, provides MultiReporter, ToolScheduler, AgentLoop, and AgentRuntime. Context provides RunContextFactory.

- [ ] **Step 9: Change AgentRunner to depend only on Runtime and Prompt**

Use:

```go
type AgentRunner struct {
    runtime engine.AgentRuntime
    prompt config.Prompt
    // existing lifecycle state
}

func NewAgentRunner(runtime engine.AgentRuntime, prompt config.Prompt) *AgentRunner
```

The runner goroutine calls only `r.runtime.Run(ctx, string(r.prompt))`.

- [ ] **Step 10: Aggregate the final root graph and fixed runtime defaults**

Root `internal.Register` aggregates config, context, provider, tools, dispatch, engine, and app; main keeps only `reagentinternal.Register` plus logger configuration.

Use package-local Fx wrappers `newRegisteredToolScheduler(registry) *ToolScheduler` with `defaultMaxParallelTools=4` and `newRegisteredAgentLoop(provider, scheduler) *AgentLoop` with Thinking enabled, rather than injecting unqualified `int` or `bool` values into the graph.

- [ ] **Step 11: Verify Fx lifecycle and graph construction**

Run: `gofmt -w internal cmd tests/integration && go test ./...`

Expected: PASS; graph construction has no direct concrete Tool/PromptComposer/SkillLoader creation in app, engine, or main.

- [ ] **Step 12: Commit all registration cutovers**

```bash
git commit --only -- internal/tools/register.go internal/tools/register_test.go internal/context/register.go internal/context/register_test.go internal/engine/register.go internal/engine/register_test.go internal/dispatch/register.go internal/dispatch/register_test.go internal/app internal/register.go cmd/main.go tests/integration/fx_dependency_graph_test.go tests/integration/registry_lifecycle_test.go -m "refactor: wire structured runtime with fx"
```

---

### Task 12: Complete integration acceptance and remove the old protocol

**Files:**
- Modify: `tests/integration/engine_skill_tool_test.go`
- Modify: `tests/integration/registry_lifecycle_test.go`
- Modify: `tests/integration/reporter_dispatch_test.go`
- Modify: `README.md`
- Modify: all affected `internal/**/*_test.go` files found by the old-symbol scan

**Interfaces:**
- Consumes: final runtime graph from Task 11.
- Produces: cross-package proof of the six-tool contract, progressive skill reading, event routing, lifecycle cleanup, and documentation of the security boundary.

- [ ] **Step 1: Update progressive Skill integration to final `read`/RoleTool behavior**

Update the real-tool skill test to request `read`, find only RoleTool observations, and assert no Skill body enters the System Prompt.

- [ ] **Step 2: Update lifecycle integration for background process cleanup**

Run a background command, stop Fx, and verify its process group exits and session records are cleared.

- [ ] **Step 3: Update Reporter integration for stream filtering**

Send an exec update and prove Terminal receives it while WeCom gets no request, then send start/failure/final events and prove WeCom receives exactly three requests.

- [ ] **Step 4: Add the exact final Registry contract assertion**

Add an exact registry assertion:

```go
want := []string{"apply_patch", "edit", "exec", "process", "read", "write"}
if diff := slices.Compare(toolNames(registry.GetAvailableTools()), want); diff != 0 {
    t.Fatalf("tool names = %v, want %v", toolNames(registry.GetAvailableTools()), want)
}
for _, old := range []string{"read_file", "edit_file", "write_file"} {
    result, err := registry.Execute(context.Background(), schema.ToolCall{ID: "old", Name: old, Arguments: json.RawMessage(`{}`)}, nil)
    if err != nil || !result.IsError { t.Fatalf("old tool %q remained callable", old) }
}
```

- [ ] **Step 5: Run the old-contract scan and remove production leftovers**

Run:

```bash
rg -n 'read_file|edit_file|write_file|old_text|new_text|timeout_ms|yield_ms|session_id|wait_ms|RoleUser.*ToolCallID|OnThinking|OnToolCall|OnToolResult|OnMessage|ProcessManager|AgentEngine' internal cmd README.md tests
```

Expected: matches remain only in negative compatibility assertions and historical design/plan documents. Remove every production match and update test fixture names that are not deliberately asserting rejection.

- [ ] **Step 6: Rewrite README runtime and tool-protocol sections**

Document the architecture chain, exact six tool names and fields, `edit.edits[]`, exec seconds vs yield milliseconds, seven process actions, text-only events, and Terminal/WeCom update behavior.

- [ ] **Step 7: Document WorkDir and shell security boundaries**

Document the WorkDir file guard and this explicit warning:

```text
WorkDir 只限制 exec 的启动目录，不是文件系统沙箱。命令继承 go-reagent 进程的宿主权限，仍可主动访问工作区外文件和网络。
```

- [ ] **Step 8: Run package and integration tests after cleanup**

Run: `gofmt -w internal cmd tests && go test -count=1 ./...`

Expected: PASS with no compatibility aliases and exactly six definitions.

- [ ] **Step 9: Commit acceptance/docs cleanup**

Inspect `git diff -- README.md` and preserve any unrelated README changes before committing.

```bash
git commit --only -- README.md internal tests/integration -m "docs: finalize structured tool runtime"
```

---

### Task 13: Run final correctness, race, vet, and diff verification

**Files:**
- Modify only files that a verification command proves require formatting or correctness fixes.

**Interfaces:**
- Consumes: completed Tasks 1-12.
- Produces: release-quality verification evidence with a clean task-scoped diff.

- [ ] **Step 1: Run the uncached complete test suite**

Run: `go test -count=1 ./...`

Expected: PASS.

- [ ] **Step 2: Run the race detector**

Run: `go test -race ./...`

Expected: PASS, especially ProcessSupervisor, Registry, ToolScheduler, Terminal, and MultiReporter concurrency tests.

- [ ] **Step 3: Run static analysis and whitespace verification**

Run: `go vet ./... && git diff --check`

Expected: both commands exit 0.

- [ ] **Step 4: Audit the final dependency and protocol boundaries**

Run:

```bash
go list -deps ./... >/dev/null
rg -n 'New(Read|Edit|Write|ApplyPatch|Exec|Process)Tool|NewPromptComposer|NewSkillLoader' internal/app internal/engine cmd
rg -n --glob '!**/*_test.go' 'read_file|edit_file|write_file|timeout_ms|yield_ms|session_id|wait_ms' internal cmd README.md
```

Expected: dependency listing succeeds; the constructor scan returns no business-layer direct construction; the old-protocol scan returns no production/documentation matches.

- [ ] **Step 5: Review the scoped diff and commit any proven verification fixes**

Run: `git status --short && git diff --stat && git diff --check`

If Step 1-4 required code changes, commit only those exact paths with a message describing the concrete correction. If no changes were required, do not create an empty commit. Leave `.idea/go-reagent.iml` and all unrelated user changes untouched.
