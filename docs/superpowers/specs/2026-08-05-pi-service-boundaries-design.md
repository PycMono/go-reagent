# Pi SDK and Service Boundaries Design

## Goal

Restructure `go-reagent` as a business service that temporarily contains an
extractable Agent Harness under `pi/`. The repository remains one Go module
until the Pi API and behavior are stable enough to move to a separate
repository.

The resulting repository has two explicit ownership zones:

- `pi/` owns reusable AI protocols, the Agent runtime, default tools,
  AGENTS/Skills resource loading, observability, and the ready-to-use
  `New`/`Run`/`Close` facade.
- top-level service packages own process configuration, application lifecycle,
  conversations, MySQL persistence, delivery channels, migrations, and the
  bundled commands.

No package named `internal`, `sdk`, `reagent`, or `workspace` is introduced.

## Final Directory Model

```text
go-reagent/
├── pi/
│   ├── ai/
│   │   └── providers/{openai,anthropic}/
│   ├── agent/
│   ├── tools/
│   │   ├── read.go
│   │   ├── write.go
│   │   ├── edit.go
│   │   ├── apply_patch.go
│   │   ├── exec.go
│   │   ├── process.go
│   │   ├── apply_patch_parser.go
│   │   ├── filesystem.go
│   │   ├── path.go
│   │   ├── output.go
│   │   └── process_supervisor.go
│   ├── utils/
│   │   ├── shell.go
│   │   ├── child_process.go
│   │   ├── process_group_unix.go
│   │   └── process_group_windows.go
│   ├── observability/
│   │   └── tracker.go
│   ├── register.go
│   ├── run_context.go
│   ├── system_prompt.go
│   ├── skills.go
│   ├── skill_discovery.go
│   ├── skill_snapshot.go
│   ├── skill_prompt.go
│   ├── xml_text.go
│   └── errors/
├── config/
│   ├── config.go
│   ├── load.go
│   └── validate.go
├── application/
│   ├── bootstrap.go
│   ├── runner.go
│   └── prompt.go
├── conversation/
│   ├── bootstrap.go
│   ├── runner.go
│   └── store.go
├── persistence/mysql/
│   ├── bootstrap.go
│   ├── connection.go
│   ├── model.go
│   ├── codec.go
│   ├── store.go
│   ├── window.go
│   └── invocation.go
├── transport/
│   ├── bootstrap.go
│   ├── terminal.go
│   └── wecom.go
├── cmd/{reagent,ping}/
├── migrations/
├── tests/integration/
├── skills/
├── docs/
├── AGENTS.md
├── config.example.json
├── README.md
├── LICENSE
├── go.mod
└── go.sum
```

Tests remain next to their corresponding implementation. Existing historical
documents under `docs/superpowers/` remain as records and are not rewritten to
pretend they described the new layout.

## Pi Responsibilities

### `pi/ai`

Owns model-facing message types, content blocks, tool-call wire types, model
configuration, the unified client contract, provider selection, and the
official OpenAI and Anthropic adapters. It cannot depend on any Agent or
service package.

### `pi/agent`

Owns the reusable Agent loop, run contracts, events, reporters, tool contract,
registry, middleware, scheduler, schema validation, response validation, and
partial-result semantics. It may depend on `pi/ai` and cannot depend on
concrete default tools or service packages.

### `pi/tools`

Owns the six concrete default tools and helpers that belong specifically to
the Tool domain. Like Pi's coding-agent `core/tools` directory, it may contain
support files that are not standalone tools. Examples are patch parsing,
confined filesystem access, path validation, output accumulation, and process
supervision.

`filesystem.go` is a file in package `tools`, not a `filesystem` package.
`process_supervisor.go` is also a file in package `tools`, not a `process`
package.

### `pi/utils`

Owns lower-level process facilities whose responsibility is broader than a
single Tool implementation: shell selection, child-process lifecycle, and
platform-specific process-group control. `pi/tools` may depend on `pi/utils`;
the reverse dependency is forbidden.

### Pi root package

Package `pi` is the complete reusable facade. It owns:

- the synchronous, concurrency-safe `Agent`;
- `New`, `Run`, and `Close`;
- Pi-only model/runtime configuration;
- default Fx assembly;
- AGENTS.md and Skill discovery from a caller-selected root;
- system-prompt and run-context composition;
- stable public aliases and error codes.

There is no `sdk.go`. The facade remains in focused files such as `agent.go`,
`config.go`, and `bootstrap.go`.

## Service Responsibilities

- `config` owns business configuration and loading, including Bot,
  Conversation, and MySQL settings.
- `application` owns the executable service lifecycle, process inputs, and
  use-case composition.
- `conversation` owns conversation history orchestration and the persistence
  port.
- `persistence/mysql` implements the conversation port and invocation ledger.
- `transport` owns Terminal and WeCom reporting.
- `cmd/reagent` is the service composition entry point; `cmd/ping` remains an
  independent command.
- repository-root `AGENTS.md` and `skills/` are the bundled service Workspace
  resources. They are runtime input consumed through Pi's resource loader, not
  source-code dependencies of package `pi`.

## Configuration Boundary

Service configuration owns model-platform selection together with its business
adapter settings:

```go
type Config struct {
	CurrentPlatform string
	Platforms       []providers.Options
	Bot             BotConfig
	Conversation    ConversationConfig
	MySQL           MySQLConfig
}
```

Pi does not define a second configuration object. `pi.Register` consumes the single
`providers.Options` selected by the business layer. Configor keeps the existing
flattened `config.example.json` layout and environment behavior.

## Dependency Rules

```text
cmd/reagent
    ↓
application
    ├── config
    ├── conversation
    ├── persistence/mysql
    ├── transport
    └── pi
         ├── tools → utils
         ├── tools → agent
         ├── observability → ai
         └── agent → ai
```

Required invariants:

1. Nothing under `pi/` imports a service package.
2. `pi/ai` does not import `pi/agent` or any higher layer.
3. `pi/agent` does not import concrete tools, resource loading, persistence,
   transport, or application lifecycle.
4. `pi/utils` does not import `pi/tools`.
5. Conversation and MySQL concerns never enter `pi/agent.Agent`.
6. No Go source file exists under a directory named `internal` after the
   migration.

## Runtime Flow

The service loads its configuration and constructs the application graph.
That graph supplies the selected `providers.Options` and working root to Pi's
default registration. Pi creates the provider client, cost tracker, resource loader,
default tools, registry, scheduler, loop, and runtime.

For a service request:

1. `application` obtains the prompt and business conversation identifiers.
2. `conversation` optionally loads prior messages from its `Store`.
3. `pi/agent.Agent` builds the current AGENTS/Skills context and runs the Agent loop.
4. Agent events flow to a `transport` reporter supplied by the service path.
5. `conversation` persists the new messages and invocation metrics.

Direct Pi callers compose `pi.Register` in their Fx App and consume the same
`pi/agent.Runner` as the service. Pi itself remains stateless across Runs other
than resources owned by the caller's Fx lifecycle.

`pi/register.go` provides `pi/agent.Runner` inside the service Fx graph,
allowing `application` and `conversation` to pass a Reporter without a second
top-level Agent facade.

## Error and Lifecycle Behavior

- Existing Pi facade error codes remain stable after moving from the module
  root to package `pi`.
- `Run` preserves partial results when an error occurs after messages have
  already been produced.
- `Close` rejects new Runs, waits for admitted Runs, and releases Fx-owned
  filesystem and process resources once.
- Conversation persistence errors remain service errors and are not converted
  into Pi SDK error codes.
- File tools continue enforcing root confinement and symlink escape checks.
- Process tools continue terminating full process groups on supported
  platforms.

## Migration Map

| Current path | Final owner |
| --- | --- |
| `ai/**` | `pi/ai/**` |
| `agent/**` | `pi/agent/**` |
| reusable Agent runtime | `pi/agent/**` |
| stable Pi errors | `pi/errors/**` |
| business portions of root `config*.go` | `config/**` |
| `internal/tools/**` | `pi/tools/**` and `pi/utils/**` |
| `internal/workspace/**` | focused files in package `pi` |
| `internal/observability/**` | `pi/observability/**` |
| `internal/bootstrap/module.go` | `pi/register.go` |
| `internal/cli/app/**` | `application/**` |
| `internal/cli/conversation/**` | `conversation/**` |
| both MySQL subtrees | `persistence/mysql/**` |
| `internal/cli/dispatch/**` | `transport/**` |
| `internal/cli/module.go` | `application/register.go` |
| `internal/cli/config.go` | `config/**` and `application/prompt.go` |

Files that only aggregate Fx providers use `register.go`; runtime startup and
lifecycle use `runtime.go`. Forwarding packages are not retained.

## Verification

The migration is complete only when all of the following hold:

- `go test ./...` passes;
- all current unit and integration behaviors remain covered;
- package-boundary tests reject imports from `pi` into service packages;
- a repository scan finds no `internal` directory or old import path;
- a repository scan finds no root Go package left behind accidentally;
- `config.example.json` continues loading with its current shape;
- both `cmd/reagent` and `cmd/ping` build;
- Unix and Windows process-group files compile for their build targets;
- README and `docs/pi-architecture.md` describe the final public imports.

## Non-Goals

This refactor does not change model protocols, replace dependencies, redesign
Tool behavior, add new persistence features, create a second Go module, or
extract `pi/` into another repository. Extraction happens only after the API
and behavior stabilize.
