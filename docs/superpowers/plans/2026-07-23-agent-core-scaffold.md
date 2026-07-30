# Agent Core Scaffold Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create the requested Go package layout, implement the minimum testable Agent Main Loop contracts, and document the project in `README.md`.

**Architecture:** `schema` owns shared messages, `provider` abstracts model calls, `tools` abstracts registration and execution, and `engine` orchestrates provider and tool interactions. The command remains the composition root and the README describes only capabilities present in the repository.

**Tech Stack:** Go 1.26, Go standard library

## Global Constraints

- Keep the module path `go-reagent` because no remote repository path is available.
- Add no third-party dependencies.
- Use the exact package layout supplied by the user.
- The current directory is not a Git repository, so commit steps are omitted.

---

### Task 1: Shared contracts and Main Loop

**Files:**
- Create: `internal/schema/message.go`
- Create: `internal/provider/interface.go`
- Create: `internal/tools/registry.go`
- Create: `internal/engine/loop.go`
- Test: `internal/engine/loop_test.go`

**Interfaces:**
- Produces: `schema.Role`, `schema.Message`, `schema.ToolCall`
- Produces: `provider.LLMProvider.Generate(context.Context, []schema.Message, []schema.ToolDefinition) (*schema.Message, error)`
- Produces: `tools.Registry.GetAvailableTools() []schema.ToolDefinition`
- Produces: `tools.Registry.Execute(context.Context, schema.ToolCall) schema.ToolResult`
- Produces: `engine.NewAgentEngine(provider.LLMProvider, tools.Registry, string, bool) *engine.AgentEngine`
- Produces: `(*engine.AgentEngine).Run(context.Context, string) error`

- [x] **Step 1: Write failing Main Loop tests**

Create tests that verify a direct assistant response returns immediately and a tool call is executed, appended as a `user` observation message linked by `tool_call_id`, and sent back to the provider.

- [x] **Step 2: Run tests and verify RED**

Run: `GOCACHE=/tmp/go-reagent-build-cache go test ./internal/engine`

Expected: FAIL because the `engine`, `provider`, `schema`, and `tools` APIs do not exist yet.

- [x] **Step 3: Add the minimum contracts and implementation**

Implement shared roles/messages/tool calls, provider and tool interfaces, and a bounded loop that:

1. calls the provider with the accumulated messages;
2. returns when the response has no tool calls;
3. executes every requested tool and appends its result;
4. returns wrapped dependency errors;
5. stops after eight model rounds.

- [x] **Step 4: Run tests and verify GREEN**

Run: `GOCACHE=/tmp/go-reagent-build-cache go test ./internal/engine`

Expected: PASS with two tests.

- [x] **Step 5: Format the Go files**

Run: `gofmt -w internal/schema/message.go internal/provider/interface.go internal/tools/registry.go internal/engine/loop.go internal/engine/loop_test.go`

### Task 2: README and exact directory layout

**Files:**
- Create: `README.md`
- Remove empty directories: `internal/context`, `internal/feishu`, `internal/memory`

**Interfaces:**
- Consumes: the package names and behavior created in Task 1
- Produces: project overview, architecture flow, directory map, quick start, and roadmap

- [x] **Step 1: Create the README**

Document the project as a minimal Go Agent core, clearly distinguish implemented contracts/Main Loop from planned provider and tool implementations, show the exact directory tree, and include `go run ./cmd/reagent` plus `go test ./...`.

- [x] **Step 2: Remove obsolete empty package directories**

Run: `rmdir internal/context internal/feishu internal/memory`

Expected: all three empty directories are removed.

- [x] **Step 3: Verify documentation paths**

Run: `rg -n 'internal/(engine|provider|schema|tools)|go run ./cmd/reagent|go test ./...' README.md`

Expected: README contains every package and both commands.

- [x] **Step 4: Verify the complete repository**

Run: `GOCACHE=/tmp/go-reagent-build-cache go test ./...`

Expected: all packages pass, with no build failures.
