# Fx Register Aggregation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move Fx object registration into the owning internal packages and expose one `internal.Register` entry point to `cmd/main.go`.

**Architecture:** `config` provides process-level values, infrastructure packages provide their own runtime objects, `engine` exposes an `Agent`, and `app` owns only the runner lifecycle. The root `internal` package aggregates package-local `Register` options without constructing objects itself.

**Tech Stack:** Go 1.26, Uber Fx 1.23, standard `testing`, existing OpenAI/Anthropic adapters and local tool Registry.

## Global Constraints

- Preserve the existing Thinking/Action loop, tool set, Reporter output, exit codes, configuration variables and shutdown behavior.
- Do not add `internal/schema/register.go`; `schema` remains Fx-free.
- Do not split the flat `internal/tools` package into subpackages.
- Preserve existing unrelated and staged worktree changes; do not commit implementation changes unless the user explicitly asks.
- Keep `NewAgentEngine`, but require callers to inject `PromptComposer` and `SkillLoader`; it must not construct Context collaborators internally.

---

### Task 1: Add process configuration registration

**Files:**
- Create: `internal/config/register.go`
- Create: `internal/config/register_test.go`
- Modify: `internal/app/providers_test.go`

**Interfaces:**
- Produces: `config.Register fx.Option`, `config.WorkDir`, `config.Prompt`, `config.NewConfig()`, `config.NewWorkDir()`, `config.NewPrompt()`.
- Consumes: existing `config.Load(path string)`.

- [ ] **Step 1: Move constructor behavior tests into config**

Add tests covering trimmed `CONFIG_PATH`, current working directory, `AGENT_PROMPT`, and the existing default prompt. The assertions must retain the current values:

```go
func TestNewPromptUsesEnvironmentOverrideAndDefault(t *testing.T) {
    t.Setenv("AGENT_PROMPT", "custom prompt")
    if got := NewPrompt(); got != Prompt("custom prompt") {
        t.Fatalf("NewPrompt() = %q", got)
    }
    t.Setenv("AGENT_PROMPT", "")
    if got := string(NewPrompt()); !strings.Contains(got, "ping.go") || !strings.Contains(got, "git 提交") {
        t.Fatalf("default prompt = %q", got)
    }
}
```

- [ ] **Step 2: Run the config tests and confirm the new API is missing**

Run: `go test ./internal/config`

Expected: build failure for undefined `Register`, `WorkDir`, `Prompt`, or constructors.

- [ ] **Step 3: Implement config/register.go**

Provide the three constructors with Fx:

```go
type WorkDir string
type Prompt string

var Register = fx.Options(
    fx.Provide(NewConfig, NewWorkDir, NewPrompt),
)
```

`NewConfig` must trim `CONFIG_PATH` and default to `config.json`; `NewWorkDir` must return `os.Getwd()`; `NewPrompt` must preserve the current environment override and default text.

- [ ] **Step 4: Remove the migrated config/prompt tests from app and verify config**

Run: `go test ./internal/config ./internal/app`

Expected: both packages pass after references are updated in later tasks or app failures are limited to the intentionally moved types.

---

### Task 2: Register Context collaborators

**Files:**
- Create: `internal/context/register.go`
- Create: `internal/context/register_test.go`

**Interfaces:**
- Consumes: `config.WorkDir`.
- Produces: `context.Register`, `*context.PromptComposer`, `*context.SkillLoader`.

- [ ] **Step 1: Write a failing Fx population test**

```go
func TestRegisterProvidesWorkspaceContextComponents(t *testing.T) {
    var composer *PromptComposer
    var loader *SkillLoader
    app := fxtest.New(t,
        fx.Supply(config.WorkDir(t.TempDir())),
        Register,
        fx.Populate(&composer, &loader),
    )
    app.RequireStart()
    defer app.RequireStop()
    if composer == nil || loader == nil {
        t.Fatal("Register did not provide context components")
    }
}
```

- [ ] **Step 2: Run the test and verify it fails**

Run: `go test ./internal/context -run TestRegisterProvidesWorkspaceContextComponents`

Expected: build failure because `Register` is undefined.

- [ ] **Step 3: Add context.Register**

Use package-local wrappers so Fx can convert the strong work directory type:

```go
var Register = fx.Options(
    fx.Provide(newPromptComposer, newSkillLoader),
)

func newPromptComposer(workDir config.WorkDir) *PromptComposer {
    return NewPromptComposer(string(workDir))
}

func newSkillLoader(workDir config.WorkDir) *SkillLoader {
    return NewSkillLoader(string(workDir))
}
```

- [ ] **Step 4: Run Context tests**

Run: `go test ./internal/context`

Expected: PASS.

---

### Task 3: Register Provider and Dispatch objects

**Files:**
- Create: `internal/provider/register.go`
- Create: `internal/provider/register_test.go`
- Create: `internal/dispatch/register.go`
- Create: `internal/dispatch/register_test.go`
- Modify: `tests/integration/reporter_dispatch_test.go`
- Modify: `internal/app/providers_test.go`

**Interfaces:**
- Consumes: `*config.Config`.
- Produces: `provider.Register`, `provider.LLMProvider`, `dispatch.Register`, `engine.Reporter`.

- [ ] **Step 1: Move Provider construction tests to provider**

Test that a configured OpenAI-compatible platform produces a non-nil `LLMProvider`, while a missing current platform returns an error containing its ID.

- [ ] **Step 2: Move Reporter construction tests to dispatch**

Test both an empty WeCom URL and an `httptest.Server` webhook. Update the integration test to call `dispatch.NewReporter` instead of `app.NewReporter`.

- [ ] **Step 3: Run tests and verify missing constructors**

Run: `go test ./internal/provider ./internal/dispatch ./tests/integration`

Expected: build failures for `provider.NewLLMProvider` and `dispatch.NewReporter`.

- [ ] **Step 4: Implement provider.Register**

Move the existing app constructor and its structured initialization log into `provider/register.go`:

```go
var Register = fx.Options(fx.Provide(NewLLMProvider))

func NewLLMProvider(cfg *config.Config) (LLMProvider, error)
```

The function must call `cfg.Current()` and then the existing `provider.New(Options{...})` factory without logging the API key.

- [ ] **Step 5: Implement dispatch.Register**

Move Reporter composition into `dispatch/register.go`:

```go
var Register = fx.Options(fx.Provide(NewReporter))

func NewReporter(cfg *config.Config) (engine.Reporter, error)
```

Return `engine.NewTerminalReporter()` when the webhook is empty; otherwise return `engine.NewMultiReporter(terminal, weCom)`.

- [ ] **Step 6: Run Provider and Dispatch tests**

Run: `go test ./internal/provider ./internal/dispatch ./tests/integration`

Expected: PASS.

---

### Task 4: Make tools.Register own the complete tool runtime

**Files:**
- Modify: `internal/tools/register.go`
- Create: `internal/tools/register_test.go`
- Modify: `tests/integration/registry_lifecycle_test.go`

**Interfaces:**
- Consumes: `fx.Lifecycle`, `config.WorkDir`.
- Produces: `tools.Register`, `tools.Registry`, six registered tool definitions.

- [ ] **Step 1: Point the integration test at tools.NewRuntimeRegistry**

Replace `app.NewRegistry(lifecycle, app.WorkDir(...))` with:

```go
registry, err := tools.NewRuntimeRegistry(lifecycle, config.WorkDir(t.TempDir()))
```

Keep the assertions for all six names and for read/exec/process failures after lifecycle stop.

- [ ] **Step 2: Run the integration test and verify the API is missing**

Run: `go test ./tests/integration -run TestNewRegistryRegistersToolsAndClosesThemOnStop`

Expected: build failure because `tools.NewRuntimeRegistry` is undefined.

- [ ] **Step 3: Implement the runtime Registry constructor**

Define:

```go
var Register = fx.Options(fx.Provide(NewRuntimeRegistry))

func NewRuntimeRegistry(lifecycle fx.Lifecycle, workDir config.WorkDir) (Registry, error)
```

Move the current construction sequence from `app.NewRegistry`: create read, edit, write, patch and ProcessManager; register read/edit/write/patch/exec/process; immediately close prior resources on partial failure; append one Fx OnStop hook.

- [ ] **Step 4: Keep reverse-order close behavior local to tools**

Move `toolClosers []io.Closer` and its reverse `Close() error` implementation into `tools/register.go`. Keep `errors.Join` behavior and existing log fields.

- [ ] **Step 5: Verify tools and integration tests**

Run: `go test ./internal/tools ./tests/integration`

Expected: PASS.

---

### Task 5: Register Engine and App lifecycle

**Files:**
- Modify: `internal/engine/engine.go`
- Create: `internal/engine/register.go`
- Create: `internal/engine/register_test.go`
- Create: `internal/app/register.go`
- Modify: `internal/app/runner.go`
- Modify: `internal/app/runner_test.go`
- Modify: `internal/app/module_test.go`
- Delete: `internal/app/module.go`

**Interfaces:**
- Consumes in Engine: `provider.LLMProvider`, `tools.Registry`, `config.WorkDir`, `*context.PromptComposer`, `*context.SkillLoader`.
- Produces from Engine: `engine.Agent`.
- Consumes in App: `engine.Agent`, `engine.Reporter`, `config.Prompt`.
- Produces from App: `*app.AgentRunner` and lifecycle hooks.

- [ ] **Step 1: Add tests for the Engine Agent interface and Register graph**

Define the interface in production as:

```go
type Agent interface {
    Run(ctx context.Context, userPrompt string, reporter Reporter) error
}
```

Test `engine.Register` with stub `LLMProvider` and `tools.Registry`, a supplied `config.WorkDir`, `context.Register`, and `fx.Populate` into an `engine.Agent`.

- [ ] **Step 2: Run the Engine register test and verify it fails**

Run: `go test ./internal/engine -run TestRegisterProvidesAgent`

Expected: build failure because `Agent` or `Register` is undefined.

- [ ] **Step 3: Implement injected Engine construction**

Change `NewAgentEngine` to require the already-created Context components:

```go
func NewAgentEngine(
    llmProvider provider.LLMProvider,
    registry tools.Registry,
    composer *context.PromptComposer,
    skillLoader *context.SkillLoader,
    workDir string,
    enableThinking bool,
) *AgentEngine
```

Add a package-local Fx constructor that supplies those injected components:

```go
func newRegisteredAgentEngine(
    llmProvider provider.LLMProvider,
    registry tools.Registry,
    workDir config.WorkDir,
    composer *ctxpkg.PromptComposer,
    skillLoader *ctxpkg.SkillLoader,
) Agent
```

Set `EnableThinking: true` and `MaxParallelTools: defaultMaxParallelTools`.

- [ ] **Step 4: Replace app-owned dependency types**

Change `AgentRunner` fields and constructor to use `engine.Agent` and `config.Prompt`. Update app tests to supply those exact types.

- [ ] **Step 5: Add app.Register**

```go
var Register = fx.Options(
    fx.Provide(NewAgentRunner),
    fx.Invoke(RegisterAgentLifecycle),
)
```

Update lifecycle tests to use `Register` with stubbed `engine.Agent`, `engine.Reporter` and `config.Prompt`.

- [ ] **Step 6: Remove app.Module and verify Engine/App**

Run: `go test ./internal/engine ./internal/app`

Expected: PASS.

---

### Task 6: Aggregate modules and switch main

**Files:**
- Modify: `internal/register.go`
- Modify: `cmd/main.go`
- Modify: `tests/integration/fx_dependency_graph_test.go`
- Delete: `internal/app/providers.go`
- Delete: `internal/app/providers_test.go`

**Interfaces:**
- Consumes: every package-local `Register` except schema.
- Produces: root `internal.Register` used by `main`.

- [ ] **Step 1: Change the integration graph test to validate internal.Register**

Import the root package with an explicit alias and require it to populate an `engine.Agent`; the population target prevents an empty `fx.Options()` from passing validation:

```go
var agent engine.Agent
if err := fx.ValidateApp(
    reagentinternal.Register,
    fx.Populate(&agent),
    fx.WithLogger(func() fxevent.Logger { return fxevent.NopLogger }),
); err != nil {
    t.Fatalf("ValidateApp() error = %v", err)
}
```

- [ ] **Step 2: Run the graph test and verify the empty root Register fails expectations**

Run: `go test ./tests/integration -run TestModuleDependencyGraphIsValid`

Expected: failure until the root option aggregates all required package registrations.

- [ ] **Step 3: Implement internal.Register**

Aggregate in dependency-readable order:

```go
var Register = fx.Options(
    config.Register,
    ctxpkg.Register,
    provider.Register,
    tools.Register,
    dispatch.Register,
    engine.Register,
    app.Register,
)
```

- [ ] **Step 4: Switch cmd/main.go**

Replace `app.Module` with `reagentinternal.Register`. Keep logger initialization and `fxevent.NopLogger` unchanged.

- [ ] **Step 5: Remove app/providers.go after all tests have moved**

Confirm no references remain:

Run: `rg -n 'app\.(NewConfig|NewWorkDir|NewLLMProvider|NewRegistry|NewReporter|NewAgentEngine|NewPrompt)|app\.Module|\b(NewConfig|NewWorkDir|NewLLMProvider|NewRegistry|NewReporter|NewAgentEngine|NewPrompt)\b' internal/app tests cmd`

Expected: no stale app constructor or `app.Module` references.

- [ ] **Step 6: Run entry point and integration package tests**

Run: `go test ./cmd ./tests/integration`

Expected: PASS.

---

### Task 7: Documentation and final verification

**Files:**
- Modify: `README.md`
- Verify: all changed Go files and tests.

**Interfaces:**
- Consumes: completed module graph.
- Produces: accurate repository documentation and verification evidence.

- [ ] **Step 1: Update the architecture documentation**

Document `cmd -> internal.Register -> package Register` and remove statements that describe `internal/app.Module` as the sole composition root. Add the package-local `register.go` files to the layout where useful.

- [ ] **Step 2: Format all changed Go files**

Run: `gofmt -w cmd/main.go internal/register.go internal/app/register.go internal/app/runner.go internal/app/*_test.go internal/config/register.go internal/config/register_test.go internal/context/register.go internal/context/register_test.go internal/dispatch/register.go internal/dispatch/register_test.go internal/engine/engine.go internal/engine/register.go internal/engine/register_test.go internal/provider/register.go internal/provider/register_test.go internal/tools/register.go internal/tools/register_test.go tests/integration/*_test.go`

Expected: no output.

- [ ] **Step 3: Run the full test suite**

Run: `go test ./...`

Expected: every package, including `tests/integration`, passes.

- [ ] **Step 4: Run static analysis**

Run: `go vet ./...`

Expected: exit code 0 with no diagnostics.

- [ ] **Step 5: Check formatting and final ownership boundaries**

Run: `git diff --check`

Run: `rg -n 'var Register = fx\.Options' internal --glob 'register.go'`

Expected: no diff errors; Register definitions exist in root, app, config, context, dispatch, engine, provider and tools, and not in schema.

- [ ] **Step 6: Inspect the final worktree without committing**

Run: `git status --short`

Expected: implementation files plus preserved pre-existing changes are visible; no unrelated file is staged or modified by this task.
