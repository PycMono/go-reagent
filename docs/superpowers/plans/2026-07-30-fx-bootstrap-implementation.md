# Fx Bootstrap Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace manual bootstrap wiring with a complete Uber Fx dependency graph while preserving the current one-shot Agent behavior and enterprise WeChat Reporter output.

**Architecture:** A framework-aware `internal/app` package is the single composition root. Existing domain packages remain Fx-independent; constructors adapt Config, Provider, Registry, Reporter, Engine, Prompt, and Runner into Fx dependencies. A lifecycle adapter starts the one-shot Runner asynchronously and shuts Fx down with the correct exit code.

**Tech Stack:** Go 1.26, `go.uber.org/fx` v1.23.0, `go.uber.org/fx/fxtest`, standard Go tests and race detector.

## Global Constraints

- Keep `config`, `provider`, `tools`, `dispatch`, and `engine` free of Fx imports.
- Keep model selection, Thinking mode, bounded tool scheduling, Reporter messages, and local Webhook configuration unchanged.
- Reporter events remain unaggregated.
- `cmd/reagent/main.go` only initializes `go-logger-sdk` and runs the Fx App.
- Long-running Agent work must not block an Fx `OnStart` hook.
- On external shutdown, cancel and wait for the Runner before closing tool resources.
- Do not log or commit API keys or the enterprise WeChat Webhook URL.

---

### Task 1: Fx composition constructors

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`
- Create: `internal/app/providers.go`
- Create: `internal/app/providers_test.go`
- Modify: `cmd/reagent/main_test.go`

**Interfaces:**
- Produces: `type WorkDir string` and `type Prompt string`.
- Produces: `NewConfig() (*config.Config, error)`.
- Produces: `NewWorkDir() (WorkDir, error)`.
- Produces: `NewLLMProvider(*config.Config) (provider.LLMProvider, error)`.
- Produces: `NewRegistry(fx.Lifecycle, WorkDir) (tools.Registry, error)`.
- Produces: `NewReporter(*config.Config) (engine.Reporter, error)`.
- Produces: `NewAgentEngine(provider.LLMProvider, tools.Registry, WorkDir) Agent`, where `Agent` declares `Run(context.Context, string, engine.Reporter) error`.
- Produces: `NewPrompt() Prompt` with the existing `AGENT_PROMPT` behavior.

- [ ] **Step 1: Add Fx dependency**

Run: `go get go.uber.org/fx@v1.23.0`

Expected: `go.mod` lists Fx directly and its support modules indirectly.

- [ ] **Step 2: Write failing constructor tests**

Create `providers_test.go` that verifies:

- `NewConfig` honors a trimmed `CONFIG_PATH` and returns the selected configuration.
- `NewLLMProvider` creates the selected protocol implementation and rejects invalid current configuration.
- `NewRegistry` registers `read_file` and `edit_file`; after the Fx test lifecycle stops, tool execution fails because resources are closed.
- `NewReporter` returns Terminal-only for an empty Webhook and sends to an `httptest.Server` when configured.
- `NewPrompt` preserves the environment override and current default prompt.

Move bootstrap behavior assertions out of `cmd/reagent/main_test.go`; leave only the logger test there.

- [ ] **Step 3: Run the constructor tests and verify RED**

Run: `go test ./internal/app ./cmd/reagent`

Expected: FAIL because `internal/app` constructors do not exist.

- [ ] **Step 4: Implement the minimal constructors**

Move the existing bootstrap logic without changing behavior. `NewRegistry` appends an `OnStop` hook that closes tools in reverse construction order and logs a sanitized close error. `NewLLMProvider` logs platform ID, protocol, and model but never credentials.

- [ ] **Step 5: Run focused tests and verify GREEN**

Run: `go test -race ./internal/app ./cmd/reagent`

Expected: PASS with no races.

- [ ] **Step 6: Commit composition constructors**

```bash
git add go.mod go.sum internal/app/providers.go internal/app/providers_test.go cmd/reagent/main_test.go
git commit -m "refactor: provide agent dependencies with fx"
```

### Task 2: One-shot Runner and Fx lifecycle

**Files:**
- Create: `internal/app/runner.go`
- Create: `internal/app/runner_test.go`
- Create: `internal/app/module.go`
- Create: `internal/app/module_test.go`

**Interfaces:**
- Produces: `NewAgentRunner(Agent, engine.Reporter, Prompt) *AgentRunner`.
- Produces: `(*AgentRunner).Start(func(error)) error`, which starts exactly one background Run.
- Produces: `(*AgentRunner).Stop(context.Context) error`, which marks stopping, cancels the Run, and waits for completion.
- Produces: `RegisterAgentLifecycle(fx.Lifecycle, fx.Shutdowner, *AgentRunner)`.
- Produces: `var Module fx.Option` containing all providers and the lifecycle Invoke.

- [ ] **Step 1: Write failing Runner tests**

Use a fake `Agent` controlled by channels. Verify:

- `Start` invokes Agent with the configured Prompt and Reporter and invokes completion once.
- A successful run reports nil completion.
- An Agent error reaches completion unchanged.
- Calling `Start` twice returns an error and does not launch a second Run.
- `Stop` cancels the Agent Context and waits for its Goroutine.
- A Stop Context deadline returns that deadline error when the Agent ignores cancellation.

- [ ] **Step 2: Run Runner tests and verify RED**

Run: `go test ./internal/app -run 'TestAgentRunner|TestModule'`

Expected: FAIL because Runner and Module do not exist.

- [ ] **Step 3: Implement Runner synchronization**

Use a mutex-protected started/stopping state, one cancel function, and one done channel. Completion callbacks run only for normal self-completion; an externally stopping Runner suppresses redundant `Shutdowner.Shutdown` calls.

- [ ] **Step 4: Implement lifecycle adapter**

`OnStart` calls `runner.Start` and returns immediately. Completion logs non-cancellation errors and calls `shutdowner.Shutdown(fx.ExitCode(1))`; success calls `shutdowner.Shutdown()`. `OnStop` delegates to `runner.Stop(ctx)`.

- [ ] **Step 5: Validate the full Fx graph**

Use `fx.ValidateApp(Module, fx.WithLogger(func() fxevent.Logger { return fxevent.NopLogger }))` and assert no graph error. This validation must not load local configuration or call external services.

- [ ] **Step 6: Run focused race tests and verify GREEN**

Run: `go test -race ./internal/app`

Expected: PASS with no races or leaked Goroutines.

- [ ] **Step 7: Commit Runner and Module**

```bash
git add internal/app/runner.go internal/app/runner_test.go internal/app/module.go internal/app/module_test.go
git commit -m "refactor: manage agent run with fx lifecycle"
```

### Task 3: Thin entry point and documentation

**Files:**
- Modify: `cmd/reagent/main.go`
- Modify: `cmd/reagent/main_test.go`
- Modify: `README.md`

**Interfaces:**
- `main()` initializes the project logger, creates `fx.New(app.Module, fx.WithLogger(...))`, and calls `Run()`.
- `newApplicationLogger()` remains unchanged and tested.

- [ ] **Step 1: Replace manual bootstrap with Fx App**

Remove configuration, Provider, Registry, Reporter, Prompt, and Agent construction helpers from `cmd/reagent/main.go`. Configure `fxevent.NopLogger` so Fx does not mix dependency-graph logs into project JSON logs.

- [ ] **Step 2: Update README architecture**

Document `internal/app`, the Fx dependency graph, one-shot lifecycle, cancellation order, and the fact that core packages do not depend on Fx.

- [ ] **Step 3: Run complete verification**

Run: `gofmt -w cmd/reagent internal/app`

Run: `go test -race ./...`

Run: `go vet ./...`

Run: `git diff --check`

Expected: all commands succeed and unrelated `.idea`, `bot.md`, and `img.png` changes remain untouched.

- [ ] **Step 4: Run real one-shot smoke test**

Run with a fixed `AGENT_PROMPT` that requests one direct response and forbids tools. Expected: the Agent finishes, the Fx App shuts down by itself with exit code 0, terminal output is preserved, and the configured enterprise WeChat group receives unaggregated Thinking and Message events.

- [ ] **Step 5: Commit entry point and documentation**

```bash
git add cmd/reagent/main.go cmd/reagent/main_test.go README.md
git commit -m "refactor: bootstrap reagent with fx"
```
