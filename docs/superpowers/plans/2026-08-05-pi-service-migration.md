# Pi SDK and Service Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move the reusable Agent Harness into `pi/`, lift all business-service packages out of `internal/`, preserve existing behavior and configuration compatibility, and leave no `internal` directory in the repository.

**Architecture:** Migrate dependency-first: `ai` and `agent` move first, followed by reusable resources, tools, utilities, observability, and the `pi` facade. Business configuration, conversation, persistence, transport, and application lifecycle then move to top-level service packages; command entry points and documentation move last.

**Tech Stack:** Go 1.26, Fx, Configor, official OpenAI and Anthropic SDKs, go-logger-sdk, go-mysql-sdk, GORM, yaml.v3, jsonschema/v6, sqlmock.

## Global Constraints

- Preserve all unrelated user changes and the untracked `.superpowers/` directory.
- Do not create any directory named `internal`, `sdk`, `reagent`, or `workspace`.
- Keep one Go module; do not add a nested `go.mod` under `pi/`.
- Preserve the six default tools and all current Tool safety behavior.
- Preserve synchronous `pi/agent.Runner.Run` behavior and Fx-owned resource lifecycle.
- Preserve Configor and the current flattened `config.example.json` structure.
- Preserve Terminal and WeCom progress reporting through the lower-level `pi/agent.Runner` service graph.
- Preserve conversation partial-result persistence and invocation metrics.
- `pi/**` must not import `application`, `config`, `conversation`, `persistence`, or `transport`.
- Use narrow staging paths and do not stage `.superpowers/`.

---

## File and Interface Map

| Target | Responsibility |
| --- | --- |
| `pi/ai/**` | model messages, configuration, client protocol, providers |
| `pi/agent/**` | generic Agent loop, events, reporters, tools, scheduling |
| `pi/tools/**` | concrete default tools and Tool-domain helpers |
| `pi/utils/**` | shell, child-process, and process-group primitives |
| `pi/observability/**` | reusable AI client metering decorator |
| `pi/*.go` | public facade, bootstrap, resource loading, skills, prompt composition |
| `config/**` | service configuration and Configor loading |
| `application/**` | service Fx graph and one-shot lifecycle |
| `conversation/**` | conversation port and history orchestration |
| `persistence/mysql/**` | MySQL connection, conversation store, invocation ledger |
| `transport/**` | Terminal and WeCom reporters |
| `cmd/**` | executable composition roots |

The public runtime contract remains:

```go
package agent

type Runner interface {
	Run(context.Context, RunRequest, Reporter) (RunResult, error)
}
```

The service graph consumes the lower-level runtime:

```go
package agent

type Runner interface {
	Run(context.Context, RunRequest, Reporter) (RunResult, error)
}
```

### Task 1: Establish Baseline and Move the Foundation Packages

**Files:**
- Move: `ai/**` -> `pi/ai/**`
- Move: `agent/**` -> `pi/agent/**`
- Modify: every Go import of `github.com/PycMono/go-reagent/ai`
- Modify: every Go import of `github.com/PycMono/go-reagent/agent`

**Interfaces:**
- Consumes: all existing `ai` and `agent` public APIs.
- Produces: identical APIs at `github.com/PycMono/go-reagent/pi/ai` and `github.com/PycMono/go-reagent/pi/agent`.

- [ ] **Step 1: Record the clean behavioral baseline**

Run:

```bash
go test ./...
```

Expected: every package passes before relocation.

- [ ] **Step 2: Move both dependency-foundation directories**

Run:

```bash
mkdir -p pi
git mv ai pi/ai
git mv agent pi/agent
```

- [ ] **Step 3: Rewrite foundation imports mechanically**

Apply these exact replacements to tracked `.go` files:

```text
github.com/PycMono/go-reagent/ai
→ github.com/PycMono/go-reagent/pi/ai

github.com/PycMono/go-reagent/agent
→ github.com/PycMono/go-reagent/pi/agent
```

- [ ] **Step 4: Verify the moved packages and their current consumers**

Run:

```bash
go test ./pi/ai/... ./pi/agent/... ./internal/...
```

Expected: PASS with no imports of the old foundation paths.

- [ ] **Step 5: Commit the foundation move**

```bash
git add pi/ai pi/agent internal cmd tests *.go
git commit -m "refactor: move agent foundations under pi"
```

### Task 2: Move Pi Resources, Tools, Utilities, and Observability

**Files:**
- Move/rewrite: `internal/workspace/composer.go` -> `pi/system_prompt.go`
- Move/rewrite: `internal/workspace/run_context.go` -> `pi/run_context.go`
- Fold `internal/workspace/workspace.go` adapters into `pi/register.go`
- Move/rewrite: `internal/workspace/skill*.go` -> `pi/skill*.go` and `pi/skills.go`
- Move: `internal/workspace/xml_text.go` -> `pi/xml_text.go`
- Move: `internal/tools/*.go` -> `pi/tools/*.go` and `pi/utils/*.go`
- Move: `internal/observability/*.go` -> `pi/observability/*.go`
- Merge: `internal/{bootstrap,tools,workspace}/module.go` -> `pi/register.go`

**Interfaces:**
- Consumes: `pi/ai.Provider`, `pi/agent.Tool`, `pi/agent.ContextFactory`, Fx lifecycle.
- Produces: the default Pi Fx options and the existing six Tool registrations.

- [ ] **Step 1: Add a package-boundary test for Pi**

Extend `tests/integration/package_boundaries_test.go` with a test that runs
`go list -deps ./pi/...` and rejects dependency paths containing any of:

```go
forbidden := []string{
	"github.com/PycMono/go-reagent/application",
	"github.com/PycMono/go-reagent/config",
	"github.com/PycMono/go-reagent/conversation",
	"github.com/PycMono/go-reagent/persistence",
	"github.com/PycMono/go-reagent/transport",
}
```

The assertion body is:

```go
for _, dependency := range goListDependencies(t, modulePath+"/pi/...") {
	for _, prefix := range forbidden {
		if dependency == prefix || strings.HasPrefix(dependency, prefix+"/") {
			t.Fatalf("pi imports service dependency %s", dependency)
		}
	}
}
```

- [ ] **Step 2: Verify the new boundary test fails before Pi is complete**

Run:

```bash
go test ./tests/integration -run PackageBoundaries -count=1
```

Expected: FAIL because the final `pi/...` package graph does not exist yet.

- [ ] **Step 3: Move resource-loading code into focused Pi root files**

Use package `pi` for all moved files. Rename `Skill`'s source file to
`skills.go`, retain `WorkDir` as an ordinary type in `register.go`, and
update imports from `internal/workspace` to the Pi root package only where a
subpackage needs those public types.

- [ ] **Step 4: Move Tool-domain files**

Move concrete Tools and helpers to package `tools`:

```text
apply_patch.go
apply_patch_parser.go
edit.go
exec.go
output.go
process.go
process_supervisor.go
read.go
write.go
```

Split `internal/tools/workspace.go` without behavior changes. Define
`type Root string` in `pi/tools/filesystem.go` so package `tools` never imports
its parent package `pi` and therefore cannot create an import cycle:

```text
Workspace and guarded filesystem operations → pi/tools/filesystem.go
cleanRelativePath and path validation      → pi/tools/path.go
```

- [ ] **Step 5: Extract generic process primitives**

Create package `utils` with these exact public functions used by
`pi/tools`:

```go
func ShellInvocation(command string) (string, []string)
func NewChildProcess(command, workDir string, overrides map[string]string) (*exec.Cmd, error)
func ConfigureProcessGroup(command *exec.Cmd)
func KillProcessGroup(process *os.Process) error
```

Place platform implementations in `process_group_unix.go` and
`process_group_windows.go`. `ShellInvocation` is the current
`shellInvocation`; `NewChildProcess` creates the shell command, applies the
working directory, validates and appends environment overrides using the
current `processEnvironment` rules, and calls `ConfigureProcessGroup`.
`ProcessSupervisor.Start` is its caller.

- [ ] **Step 6: Merge default assembly into `pi/register.go`**

Define one exported Fx option:

```go
var Register = fx.Options(
	fx.Provide(
		newToolRoot,
		tools.NewWorkspace,
		tools.NewProcessSupervisor,
		fx.Annotate(tools.NewReadTool, fx.As(new(agent.Tool)), fx.ResultTags(`group:"agent_tools"`)),
		fx.Annotate(tools.NewEditTool, fx.As(new(agent.Tool)), fx.ResultTags(`group:"agent_tools"`)),
		fx.Annotate(tools.NewWriteTool, fx.As(new(agent.Tool)), fx.ResultTags(`group:"agent_tools"`)),
		fx.Annotate(tools.NewApplyPatchTool, fx.As(new(agent.Tool)), fx.ResultTags(`group:"agent_tools"`)),
		fx.Annotate(tools.NewExecTool, fx.As(new(agent.Tool)), fx.ResultTags(`group:"agent_tools"`)),
		fx.Annotate(tools.NewProcessTool, fx.As(new(agent.Tool)), fx.ResultTags(`group:"agent_tools"`)),
		newPromptComposer,
		newSkillLoader,
		fx.Annotate(NewRunContextFactory, fx.As(new(agent.ContextFactory))),
		newClient,
		newRegistry,
		newScheduler,
		newLoop,
		fx.Annotate(agent.New, fx.As(fx.Self()), fx.As(new(agent.Runner))),
	),
)
```

`Register` consumes `providers.Options` and `pi.WorkDir`. Define `type WorkDir string` in
`register.go`, which converts it to `tools.Root` before
constructing the guarded filesystem. `providers.Options` owns the normalization
required to construct one Provider; the business `config` package owns the
platform list and current-platform selection.

Delete the three old `module.go` files after all providers are represented in
`pi.Register`.

- [ ] **Step 7: Verify reusable Pi components**

Run:

```bash
go test ./pi/... ./tests/integration -run 'EngineSkillTool|PackageBoundaries' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit the reusable implementation move**

```bash
git add pi internal tests/integration/package_boundaries_test.go
git commit -m "refactor: assemble reusable pi runtime"
```

### Task 3: Finalize the Runtime Contract and Split Configuration

**Files:**
- Remove the module-root Agent facade and keep `pi/agent.Agent` as the only Agent type.
- Move the reusable Fx graph into `pi/register.go`.
- Move stable errors into `pi/errors/`; do not retain root type aliases.
- Move platform-list configuration and validation into `config/{config,load,platform,validate}.go`.
- Move: all corresponding root tests next to their new owners.

**Interfaces:**
- Produces: `pi.Register` and the single `pi/agent.Runner` contract.
- Produces: `config.Config`, `config.Load(path string) (*Config, error)`.

- [ ] **Step 1: Add flattened-config compatibility coverage**

Create `config/load_test.go` that writes the current JSON shape and asserts:

```go
if got.CurrentPlatform != "test" {
	t.Fatalf("current platform = %q", got.CurrentPlatform)
}
if got.Conversation.HistoryMessageLimit != 100 {
	t.Fatalf("history limit = %d", got.Conversation.HistoryMessageLimit)
}
```

- [ ] **Step 2: Move the facade and replace root imports**

Move facade files to package `pi` and replace root-package imports with
`github.com/PycMono/go-reagent/pi`. Preserve the exact public behavior and
error classification already covered by the facade tests.

- [ ] **Step 3: Make Pi registration consume one Provider configuration**

```go
var Register = fx.Options(/* providers and runtime graph */)
```

Do not define a Pi-level platform list, selection object, or second Agent
facade. Normalize the single Provider profile through `providers.Options`
before constructing the `pi/agent.Agent` in the Fx graph.

- [ ] **Step 4: Define service configuration with flattened decoding**

```go
package config

type Config struct {
	CurrentPlatform string
	Platforms       []providers.Options
	Bot             BotConfig
	Conversation    ConversationConfig
	MySQL           MySQLConfig
}
```

`Load` must decode the existing top-level `currentPlatform` and `platforms`
fields directly into `Config`; it must keep `bot`, `conversation`, and `mysql`
at their existing locations. Configor remains the only loader. Expose
these exact service constructors:

```go
func Load(path string) (*Config, error)
func NewFromEnvironment() (*Config, error)
func NewPlatform(config *Config) (providers.Options, error)
```

`NewFromEnvironment` reads `CONFIG_PATH`, defaults it to `config.json`, and
calls `Load`; `NewPlatform` returns the selected Provider profile. Platform-list
selection and validation stay in `config`, while `providers.Options` validates
the fields required to construct one Provider.

- [ ] **Step 5: Verify facade and configuration behavior**

Run:

```bash
go test ./pi/... ./config/...
```

Expected: PASS, including lifecycle, concurrency, error, and flattened-config
tests.

- [ ] **Step 6: Commit facade and configuration split**

```bash
git add pi config *.go internal cmd tests
git commit -m "refactor: expose pi facade and service config"
```

### Task 4: Lift Conversation and MySQL Packages

**Files:**
- Move: `internal/cli/conversation/*.go` -> `conversation/*.go`
- Move: `internal/cli/conversation/mysql/*.go` -> `persistence/mysql/*.go`
- Move: `internal/cli/driver/mysql/*.go` -> `persistence/mysql/*.go`
- Merge: both MySQL `module.go` files -> `persistence/mysql/bootstrap.go`
- Rename: conversation `module.go` -> `conversation/bootstrap.go`

**Interfaces:**
- Consumes: `pi/agent.Runner`, `pi/agent.Reporter`, `config.Config`.
- Produces: `conversation.Store`, `conversation.Runner`, and MySQL implementation.

- [ ] **Step 1: Move conversation domain files and tests**

Use package `conversation`. Replace `reagent.Config` with `config.Config` and
foundation imports with `pi/agent` and `pi/ai`. Preserve `RunRequest`, `Store`,
history window, partial result, and optimistic version behavior.

- [ ] **Step 2: Move and merge MySQL code**

Use package `mysql` for connection, models, codec, store, window, invocation,
and tests. Resolve the two existing `module.go` names by creating:

```go
var Module = fx.Options(
	fx.Provide(NewProvider, NewTransactionManager),
	fx.Provide(NewStore),
)
```

in `persistence/mysql/bootstrap.go`, preserving all existing constructor
arguments and transaction wiring.

- [ ] **Step 3: Verify domain and persistence tests**

Run:

```bash
go test ./conversation/... ./persistence/mysql/... ./tests/integration -run Conversation -count=1
```

Expected: PASS without a live database except tests already explicitly marked
as integration tests.

- [ ] **Step 4: Commit business data packages**

```bash
git add conversation persistence internal/cli/conversation internal/cli/driver tests
git commit -m "refactor: lift conversation persistence packages"
```

### Task 5: Lift Transport and Application Packages

**Files:**
- Move: `internal/cli/dispatch/*.go` -> `transport/*.go`
- Move: `internal/cli/app/*.go` -> `application/*.go`
- Split: `internal/cli/config.go` -> `config/load.go` and `application/prompt.go`
- Merge: `internal/cli/module.go` -> `application/register.go`

**Interfaces:**
- Consumes: `pi.Register`, `pi/agent.Runner`, `conversation.Runner`,
  `infrastructure.Register`, `config.Config`.
- Produces: complete service `application.Register` and transport Reporter.

- [ ] **Step 1: Move reporters into `transport`**

Use package `transport`; replace all Agent imports with `pi/agent`. Merge
`module.go` and `wecom_module.go` registration into `transport/register.go`
without changing reporter order (`terminal` remains order 100) or optional
WeCom behavior.

- [ ] **Step 2: Move lifecycle orchestration into `application`**

Use package `application`. Keep the existing non-blocking Fx `OnStart`, cancel
and wait behavior in `runner.go`. Move environment-derived prompt construction
to `prompt.go`.

- [ ] **Step 3: Assemble the service graph**

Define `application.Register` in `application/register.go` using:

```go
var Register = fx.Options(
	pi.Register,
	infrastructure.Register,
	conversation.Register,
	transport.Register,
	fx.Provide(config.NewFromEnvironment, config.NewPlatform, NewWorkDir, NewPrompt, NewAgentRunner),
	fx.Invoke(RegisterAgentLifecycle),
)
```

`NewWorkDir` returns `(pi.WorkDir, error)` from `os.Getwd`. The process path
resolution in `config.NewFromEnvironment` continues defaulting `CONFIG_PATH`
to `config.json`.

- [ ] **Step 4: Verify application and transport behavior**

Run:

```bash
go test ./application/... ./transport/... ./tests/integration -run 'RegistryLifecycle|ReporterDispatch|FxDependencyGraph' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit service orchestration**

```bash
git add application transport config internal/cli tests
git commit -m "refactor: lift service application and transport"
```

### Task 6: Update Commands, Remove `internal`, and Document Imports

**Files:**
- Modify: `cmd/reagent/*.go`
- Modify: `cmd/ping/*.go` only if imports require it
- Delete: empty `internal/` tree after all source moves
- Create: `docs/pi-architecture.md`
- Modify: `README.md`
- Modify: integration tests with old import paths

**Interfaces:**
- Consumes: `application.Module`.
- Produces: buildable commands and documented final package paths.

- [ ] **Step 1: Point the service command at the final graph**

`cmd/reagent/main.go` must construct:

```go
fx.New(
	application.Module,
	fx.WithLogger(func() fxevent.Logger { return fxevent.NopLogger }),
).Run()
```

- [ ] **Step 2: Update integration imports and assert forbidden paths**

Replace all remaining imports under
`github.com/PycMono/go-reagent/internal/`. Extend the package boundary test
with this tracked-path assertion:

```go
command := exec.Command("git", "ls-files")
output, err := command.Output()
if err != nil {
	t.Fatalf("git ls-files: %v", err)
}
for _, path := range strings.Fields(string(output)) {
	if strings.Contains("/"+path+"/", "/internal/") {
		t.Fatalf("tracked internal path remains: %s", path)
	}
}
```

- [ ] **Step 3: Verify the old tree is unused, then remove empty directories**

Run:

```bash
rg -n 'github.com/PycMono/go-reagent/(ai|agent|internal)(/|\")' --glob '*.go'
find internal -type f
```

Expected: both commands print no source files requiring migration. Remove only
the now-empty `internal` directories.

- [ ] **Step 4: Document the final public imports**

Create `docs/pi-architecture.md` with the final package responsibility and
dependency direction. Update README examples to import:

```go
"github.com/PycMono/go-reagent/pi"
```

and supply `providers.Options` to `pi.Register`.

- [ ] **Step 5: Build commands and run the full suite**

Run:

```bash
go test ./...
go build ./cmd/reagent ./cmd/ping
git diff --check
```

Expected: all commands exit zero.

- [ ] **Step 6: Check cross-platform process compilation**

Run:

```bash
GOOS=windows GOARCH=amd64 go test ./pi/utils ./pi/tools -run '^$'
```

Expected: both packages compile with the Windows process-group implementation.

- [ ] **Step 7: Commit the final migration**

```bash
git add README.md docs/pi-architecture.md cmd tests pi config application conversation persistence transport internal
git commit -m "refactor: complete pi service package migration"
```

## Final Acceptance

- [ ] `go test ./...` passes.
- [ ] `go build ./cmd/reagent ./cmd/ping` passes.
- [ ] Windows cross-compilation for `pi/tools` and `pi/utils` passes.
- [ ] `git diff --check` passes.
- [ ] `rg --files | rg '(^|/)internal/'` returns no matches.
- [ ] `rg -n 'github.com/PycMono/go-reagent/(ai|agent|internal)(/|\")' --glob '*.go'` returns no matches.
- [ ] `pi/**` has no imports of service packages.
- [ ] `config.example.json` loads unchanged.
- [ ] `.superpowers/` remains untracked and untouched.
