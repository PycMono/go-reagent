# Structured Run Contract Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the one-shot string-only runtime API with a structured request/result contract that accepts caller-owned history, context, and current input and returns the messages created during one run.

**Architecture:** Keep the current internal package layout and existing Provider, Tool Registry, Scheduler, Workspace, Skills, and Fx composition. Add neutral run data to `internal/schema`, prepare a request in `internal/context`, keep per-run state local to `AgentLoop`, and have `internal/engine.runtime` map loop output into a caller-facing `RunResult`. Reporter ownership moves to each `Run` call.

**Tech Stack:** Go 1.26, standard library testing, Uber Fx, existing internal Provider/Tool abstractions.

## Global Constraints

- Preserve all pre-existing staged, modified, and untracked user files; never overwrite or revert unrelated changes.
- Keep all packages under their current paths; public SDK package migration is out of scope.
- Do not add Session, HistoryStore, MemoryStore, persistence, compaction, Usage, or StopReason behavior.
- Preserve existing coding tools, Workspace, Skill discovery, provider selection, and reporter event schemas.
- Follow red-green-refactor: every behavior change starts with a focused failing test whose failure is observed before production code changes.
- Do not create implementation commits in the current dirty worktree unless the user explicitly requests them; verification evidence replaces per-task commits in this execution.

---

### Task 1: Structured request data and context assembly

**Files:**
- Create: `internal/schema/run.go`
- Modify: `internal/context/run_context.go`
- Modify: `internal/context/run_context_test.go`

**Interfaces:**
- Produces: `schema.ContextBlock`, `schema.RunRequest`, `schema.RunResult`.
- Produces: `RunContext.Metadata map[string]string`.
- Changes: `RunContextFactory.Create(context.Context, schema.RunRequest, []schema.ToolDefinition) (RunContext, error)`.
- Preserves: current core prompt, AGENTS.md/Skill discovery, and required `read` tool behavior.

- [ ] **Step 1: Write the failing context-assembly test**

Add a test that constructs a request containing two context blocks with different priorities, one history message, and one current user input. Assert the prepared message order is core system, high-priority context, low-priority context, history, input; assert context messages use the literal `# Context: <name>\n<content>` rendering; mutate the original top-level slices/maps after `Create` and assert `RunContext` containers are unchanged.

- [ ] **Step 2: Run the focused test and verify red**

Run:

```bash
go test ./internal/context -run 'TestRunContextFactoryAssemblesStructuredRequest' -count=1
```

Expected: FAIL because `schema.RunRequest`, `schema.ContextBlock`, and the new `Create` signature do not exist.

- [ ] **Step 3: Implement the neutral run types and assembly path**

Create:

```go
type ContextBlock struct {
	Name     string            `json:"name"`
	Content  string            `json:"content"`
	Priority int               `json:"priority,omitempty"`
}

type RunRequest struct {
	RunID    string            `json:"run_id,omitempty"`
	History  []Message         `json:"history,omitempty"`
	Input    Message           `json:"input"`
	Context  []ContextBlock    `json:"context,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type RunResult struct {
	RunID       string    `json:"run_id,omitempty"`
	NewMessages []Message `json:"new_messages,omitempty"`
}
```

Update `RunContextFactory.Create` to validate the current input, clone the top-level request containers, stably sort a copied context slice by descending priority, render each block as a system message, and assemble messages in the specified order. Do not mutate request-owned slices or maps.

- [ ] **Step 4: Write validation tests and verify red**

Add table-driven tests covering nil Go context, canceled context, non-user input, empty input content, input tool fields, empty context name, and empty context content. Each test must assert the specific contract failure and must verify invalid input is rejected before provider execution is possible.

Run:

```bash
go test ./internal/context -run 'TestRunContextFactoryRejectsInvalidStructuredRequest' -count=1
```

Expected: FAIL for every validation branch not yet implemented.

- [ ] **Step 5: Implement minimal validation and verify green**

Use `schema.TextContent` to reject unsupported/empty input content, require `RoleUser`, reject tool fields on input, and trim only for validation and context headings. Preserve the current cancellation error wrapping and skills-to-`read` check.

Run:

```bash
go test ./internal/context -count=1
```

Expected: PASS.

### Task 2: AgentLoop message increments and partial results

**Files:**
- Modify: `internal/engine/agent_loop.go`
- Modify: `internal/engine/loop_test.go`
- Create: `internal/engine/run_messages_test.go`

**Interfaces:**
- Changes: `AgentLoop.Run(context.Context, context.RunContext, Reporter) ([]schema.Message, error)`.
- Produces: action assistant messages and tool-result messages in model execution order.
- Excludes: prepared input messages and internal thinking/synthetic transition messages.

- [ ] **Step 1: Write the failing direct-response result test**

Use a real `AgentLoop` with the existing fake provider and registry. Assert a final assistant response is returned as a one-element increment and is not merely emitted through Reporter.

Run:

```bash
go test ./internal/engine -run 'TestAgentLoopReturnsDirectAssistantIncrement' -count=1
```

Expected: FAIL because `AgentLoop.Run` does not return messages.

- [ ] **Step 2: Implement minimal loop result collection**

Create a local `newMessages` slice beside `contextHistory`. Append each validated action assistant message and each completed tool-result message to both the effective history and the appropriate result slice. Return a newly allocated result slice on every exit path.

- [ ] **Step 3: Write tool-order and thinking-exclusion tests**

Assert this exact result order for a tool run:

```text
assistant(tool call) -> tool(result) -> assistant(final)
```

With thinking enabled, assert planning assistant messages and synthetic action-transition user messages are absent from the returned increment.

Run:

```bash
go test ./internal/engine -run 'TestAgentLoopReturnsToolConversationInOrder|TestAgentLoopExcludesThinkingScaffoldingFromIncrement' -count=1
```

Expected: FAIL until all result branches are collected correctly.

- [ ] **Step 4: Write and implement the partial-result error behavior**

Script a provider to return an assistant tool call, allow its tool result to complete, then fail the next provider request. Assert `Run` returns the provider error together with the completed assistant tool-call message and tool-result message.

Run:

```bash
go test ./internal/engine -run 'TestAgentLoopReturnsCompletedMessagesWithProviderError' -count=1
```

Expected before implementation: FAIL because the error path discards messages. Expected after implementation: PASS.

- [ ] **Step 5: Verify the engine package**

Run:

```bash
go test ./internal/engine -count=1
```

Expected: PASS.

### Task 3: Runtime structured contract and per-run Reporter

**Files:**
- Modify: `internal/engine/runtime.go`
- Modify: `internal/engine/runtime_test.go`

**Interfaces:**
- Changes: `AgentRuntime.Run(context.Context, schema.RunRequest, Reporter) (schema.RunResult, error)`.
- Changes: `NewAgentRuntime(factory, loop, registry)` no longer captures Reporter.
- Consumes: structured context factory and AgentLoop message increments from Tasks 1 and 2.

- [ ] **Step 1: Write the failing runtime forwarding test**

Update the runtime fakes around the desired API and assert the exact `RunRequest`, available tool definitions, and per-call Reporter reach the factory/loop exactly once. Assert the result preserves `RunID` and loop-produced messages.

Run:

```bash
go test ./internal/engine -run 'TestAgentRuntimePreparesStructuredRequestAndReturnsIncrement' -count=1
```

Expected: FAIL because the runtime still accepts a string, captures Reporter at construction, and returns only an error.

- [ ] **Step 2: Implement the runtime contract**

Change the interface and concrete runtime to initialize `schema.RunResult{RunID: request.RunID}`, prepare the structured context, invoke the loop with the call-specific Reporter, and copy loop messages into `NewMessages`. Keep nil dependency checks.

- [ ] **Step 3: Write and implement preparation/loop partial-result tests**

Assert preparation failure returns the caller's `RunID` with no messages and prevents the loop. Assert loop failure returns the caller's `RunID`, completed loop messages, and the original wrapped error.

Run:

```bash
go test ./internal/engine -run 'TestAgentRuntimePreparationErrorPreservesRunID|TestAgentRuntimeLoopErrorPreservesIncrement' -count=1
```

Expected before implementation: FAIL. Expected after implementation: PASS.

- [ ] **Step 4: Verify runtime and loop tests together**

Run:

```bash
go test ./internal/engine -count=1
```

Expected: PASS.

### Task 4: Preserve the one-shot Fx application at the adapter boundary

**Files:**
- Modify: `internal/app/runner.go`
- Modify: `internal/app/runner_test.go`
- Modify: `internal/app/module_test.go`
- Modify: `internal/engine/register.go`
- Modify: `internal/engine/register_test.go`

**Interfaces:**
- Changes: `NewAgentRunner(engine.AgentRuntime, config.Prompt, engine.Reporter)`.
- Behavior: convert the configured string prompt into `schema.RunRequest{Input: schema.Message{Role: RoleUser, Content: []ContentBlock{TextBlock(prompt)}}}`.
- Behavior: pass the composed Fx Reporter into each runtime call.

- [ ] **Step 1: Write the failing AgentRunner adapter test**

Change the runtime test double to the structured API. Assert `AgentRunner` converts `config.Prompt("test prompt")` into exactly one user `Input`, passes an empty History/Context, and forwards the injected Reporter instance.

Run:

```bash
go test ./internal/app -run 'TestAgentRunnerBuildsStructuredRequestAndForwardsReporter' -count=1
```

Expected: FAIL because `AgentRunner` still calls `Run(ctx, string)`.

- [ ] **Step 2: Implement the application adapter**

Store the composed Reporter on `AgentRunner`, build the structured request immediately before each call, ignore the successful `RunResult`, and continue routing the returned error into the existing completion/shutdown logic.

- [ ] **Step 3: Update lifecycle and Fx graph tests**

Update runtime doubles and constructors in app/engine registration tests. Keep all existing assertions for cancellation, duplicate starts, shutdown exit codes, reporter ordering, scheduler defaults, and thinking defaults.

Run:

```bash
go test ./internal/app ./internal/engine -count=1
```

Expected: PASS.

### Task 5: Migrate remaining internal call sites and verify the repository

**Files:**
- Modify: `tests/integration/engine_skill_tool_test.go`
- Modify only as compilation requires: other tests returned by the old-signature search
- Modify: `README.md`

**Interfaces:**
- Consumes: final structured runtime contract.
- Preserves: existing integration behavior and current CLI task semantics.

- [ ] **Step 1: Find every old call and constructor**

Run:

```bash
rg -n '\.Run\([^\n]*"|NewAgentRuntime\(' --glob '*.go'
```

Classify each result as runtime, loop, or unrelated process-session behavior before editing it.

- [ ] **Step 2: Migrate integration calls without changing their assertions**

Replace string runtime calls with a `schema.RunRequest` containing a user `Input`, pass the existing Reporter or nil per call, and capture/ignore `RunResult` where the test is not about increments. Preserve all pre-existing user edits in dirty test files.

- [ ] **Step 3: Update README architecture and examples**

Document the structured input and incremental output contract and state explicitly that the runtime keeps state only during one run. Do not document public import paths, persistence, Usage, StopReason, or compaction as implemented.

- [ ] **Step 4: Format changed Go files**

Run `gofmt` only on Go files changed for this feature.

- [ ] **Step 5: Run focused race-sensitive tests**

Run:

```bash
go test -race ./internal/engine ./internal/app ./internal/context
```

Expected: PASS with no race reports.

- [ ] **Step 6: Run the complete suite**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 7: Audit the final diff**

Run:

```bash
git diff --check
git status --short
git diff -- internal/schema/run.go internal/context/run_context.go internal/engine/agent_loop.go internal/engine/runtime.go internal/app/runner.go
```

Confirm no unrelated user changes were reverted or included, no persistence/session behavior was added, and the implemented signatures match the approved design.
