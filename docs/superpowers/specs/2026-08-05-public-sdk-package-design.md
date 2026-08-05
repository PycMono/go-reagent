# Public SDK Package Design

## Goal

Reorganize go-reagent around Pi's layered package model and expose a stable Go SDK for upper-layer business systems:

```text
ai -> agent -> reagent
```

- `ai` owns model-facing protocols and provider implementations.
- `agent` owns the reusable Agent runtime mechanism.
- the module-root `reagent` package owns the complete, configured default Agent SDK.
- the bundled CLI, conversation persistence, MySQL wiring, and output channels remain private product adapters.

The business caller remains responsible for loading conversation history before a run and persisting `RunResult.NewMessages` after a run. The SDK is stateless across runs, supports concurrent calls on one long-lived Agent instance, and returns synchronously without exposing progress events through the root package.

## Design Principles

The package boundaries follow Pi's design principles rather than its TypeScript directory names verbatim:

1. The model layer does not depend on Agent orchestration or product behavior.
2. The Agent core defines reusable execution mechanisms without embedding workspace, persistence, CLI, or channel policy.
3. The root `reagent` package assembles a complete default product from the lower layers.
4. Persistence, message delivery, and CLI lifecycle remain orthogonal to the Agent core.
5. Package visibility represents a compatibility contract, not merely source organization.
6. Existing third-party libraries remain the sole implementation for capabilities they already provide.

The root SDK is intentionally narrower than Pi's extension model. `reagent.New` does not accept caller-supplied Provider, Tool, Reporter, or Store implementations. The lower-level `ai` and `agent` packages remain importable, Pi-style foundation packages, but the complete root SDK does not offer replacement hooks for its default components.

## Scope

This change includes:

- public `ai` and `agent` foundation packages;
- a public module-root `reagent` package;
- migration of the current internal model, message, run, tool, and engine contracts;
- a shared private Fx composition module;
- relocation of workspace and default-tool product implementations;
- relocation of the bundled CLI's conversation, MySQL, dispatch, and process-lifecycle code;
- a stable public error-code enum and wrapped error type;
- public SDK, concurrency, lifecycle, and package-boundary tests;
- README and architecture documentation updates.

## Non-Goals

This change does not:

- add a second configuration parser or a JSON-only parser;
- change the existing JSON, YAML, TOML, Configor, or environment-overlay behavior;
- replace the OpenAI or Anthropic Go SDKs;
- replace Fx dependency injection or lifecycle management;
- replace `go-logger-sdk`;
- replace `go-mysql-sdk`, its transaction manager, GORM, or SQL migrations;
- add Provider-specific error compatibility mappings;
- add a public Reporter or progress-stream API to the root package;
- add public Tool, Provider, or Store injection to `reagent.New`;
- move conversation loading or persistence into the SDK run path;
- add retries, queues, session locking, summaries, compaction, or distributed coordination;
- retain old internal packages through aliases, deprecated forwarding functions, or duplicate types.

## Target Package Layout

```text
go-reagent/
├── ai/
│   ├── content.go
│   ├── message.go
│   ├── model.go
│   ├── client.go
│   ├── factory.go
│   ├── errors.go
│   ├── providers/
│   │   ├── openai/
│   │   │   ├── client.go
│   │   │   ├── convert.go
│   │   │   └── client_test.go
│   │   └── anthropic/
│   │       ├── client.go
│   │       ├── convert.go
│   │       └── client_test.go
│   └── *_test.go
├── agent/
│   ├── agent.go
│   ├── run.go
│   ├── loop.go
│   ├── tool.go
│   ├── registry.go
│   ├── middleware.go
│   ├── scheduler.go
│   ├── validation.go
│   ├── event.go
│   └── *_test.go
├── reagent.go
├── config.go
├── types.go
├── error_code.go
├── error.go
├── bootstrap.go
├── internal/
│   ├── bootstrap/
│   │   └── module.go
│   ├── workspace/
│   │   ├── workspace.go
│   │   ├── run_context.go
│   │   ├── composer.go
│   │   ├── skill.go
│   │   ├── skill_discovery.go
│   │   ├── skill_snapshot.go
│   │   ├── skill_prompt.go
│   │   ├── xml_text.go
│   │   └── *_test.go
│   ├── tools/
│   │   ├── workspace.go
│   │   ├── read.go
│   │   ├── write.go
│   │   ├── edit.go
│   │   ├── apply_patch.go
│   │   ├── apply_patch_parser.go
│   │   ├── exec.go
│   │   ├── process.go
│   │   ├── process_supervisor.go
│   │   ├── process_group_unix.go
│   │   ├── process_group_windows.go
│   │   └── *_test.go
│   └── cli/
│       ├── module.go
│       ├── app/
│       │   ├── runner.go
│       │   ├── register.go
│       │   └── *_test.go
│       ├── driver/
│       │   └── mysql/
│       │       ├── connection.go
│       │       ├── register.go
│       │       └── *_test.go
│       ├── conversation/
│       │   ├── runner.go
│       │   ├── store.go
│       │   ├── register.go
│       │   ├── *_test.go
│       │   └── mysql/
│       │       ├── model.go
│       │       ├── codec.go
│       │       ├── window.go
│       │       ├── store.go
│       │       ├── register.go
│       │       └── *_test.go
│       └── dispatch/
│           ├── terminal.go
│           ├── wecom.go
│           ├── register.go
│           └── *_test.go
├── cmd/
│   ├── reagent/
│   │   ├── main.go
│   │   └── main_test.go
│   └── ping/
│       └── main.go
├── skills/
├── migrations/
├── tests/integration/
└── docs/
```

The layout is a target ownership model, not a requirement to split every file before its responsibility is clear. Existing focused files may retain their names when moved. No empty placeholder package is created.

## Dependency Direction

The required dependency direction is:

```text
ai
^
|
agent
^
|
reagent root package
├── internal/bootstrap
├── internal/workspace
└── internal/tools
^
|
├── upper-layer business applications
└── internal/cli
    ^
    |
 cmd/reagent
```

Rules:

- `ai` must not import `agent`, the root package, workspace, tools, persistence, dispatch, or CLI packages.
- `agent` may import `ai` but must not import root-product workspace, built-in tools, persistence, dispatch, or CLI packages.
- `internal/workspace` and `internal/tools` implement product policy against public lower-level contracts.
- the root package exposes the complete default SDK and owns no conversation state.
- `internal/cli` may consume the shared private bootstrap graph and add CLI-only dependencies.
- `cmd/reagent` contains process entry code only.

## Package Responsibilities

### `ai`

`ai` corresponds to Pi's model foundation package. It owns:

- `Role`, `ContentBlock`, `Message`, `ToolCall`, and `ToolDefinition`;
- model identity and capability data;
- `PlatformConfig` and the supported protocol constants;
- the unified model-generation client contract;
- OpenAI and Anthropic request/response conversion;
- provider selection from the selected platform configuration;
- model-facing errors without Agent, workspace, or persistence policy.

The existing OpenAI and Anthropic implementations move without replacing their official SDK clients.

### `agent`

`agent` corresponds to Pi's Agent core. It owns:

- `RunRequest`, `ContextBlock`, and `RunResult`;
- the model/tool loop;
- Tool contracts, Registry, Middleware, and Scheduler;
- response and tool-call validation;
- partial-result semantics;
- the low-level event and Reporter contract needed by product assembly and the bundled CLI.

The root package does not re-export the Reporter or event API and does not accept one in `Agent.Run`. The bundled CLI may use the lower-level event contract to preserve Terminal and WeCom progress reporting.

### Root `reagent`

The module root corresponds to Pi's complete coding-agent package while retaining a domain-neutral name. It owns:

- `LoadConfig`;
- the public `Config` and existing nested configuration types;
- `New` and the long-lived public `Agent` facade;
- synchronous, stateless `Agent.Run`;
- `Agent.Close`;
- root aliases for the common `ai` and `agent` request/message types;
- stable SDK error codes;
- composition of the default AI client, workspace, tools, and Agent runtime.

### `internal/workspace`

This package owns product policy that the generic Agent core must not dictate:

- the bound process workspace;
- `AGENTS.md` validation and loading;
- Skill discovery, eligibility, snapshots, and progressive prompt rendering;
- system-prompt composition;
- external Context, History, and Input assembly.

The current behavior remains: the workspace is resolved from the process working directory when the Agent is constructed, and AGENTS/Skills are rediscovered for each run.

### `internal/tools`

This package owns the default product tools:

- confined workspace access;
- `read`, `write`, `edit`, and `apply_patch`;
- `exec` and background `process`;
- process supervision and platform-specific process-group handling.

Tool contracts and generic execution machinery move to `agent`; concrete default tools remain private to the root product.

### `internal/cli`

This subtree owns only the bundled command product:

- one-shot process lifecycle;
- optional conversation-history loading and storage;
- MySQL connection and transaction wiring;
- Terminal and WeCom delivery;
- CLI environment variables and exit behavior.

None of these responsibilities enters the root SDK's `Run` path.

## Existing Dependency Preservation

The migration must preserve these dependencies and their responsibilities:

| Capability | Existing dependency | Target owner |
| --- | --- | --- |
| Configuration loading | `github.com/jinzhu/configor` | root `config.go` |
| OpenAI protocol | `github.com/openai/openai-go/v3` | `ai/providers/openai` |
| Anthropic protocol | `github.com/anthropics/anthropic-sdk-go` | `ai/providers/anthropic` |
| JSON Schema validation | `github.com/santhosh-tekuri/jsonschema/v6` | `agent` tool runtime |
| Dependency injection/lifecycle | `go.uber.org/fx` | private bootstrap and CLI modules |
| Logging | `github.com/PycMono/go-logger-sdk` | all current logging call sites after relocation |
| Skill YAML | `gopkg.in/yaml.v3` | `internal/workspace` |
| MySQL provider | `github.com/PycMono/go-mysql-sdk` | `internal/cli/driver/mysql` |
| MySQL transactions | `github.com/PycMono/go-mysql-sdk/transaction` | `internal/cli/driver/mysql` |
| ORM | `gorm.io/gorm` | `internal/cli/conversation/mysql` |
| SQL mocking | `github.com/DATA-DOG/go-sqlmock` | MySQL tests |
| GORM MySQL test driver | `gorm.io/driver/mysql` | MySQL tests |

The refactor must not replace these libraries with custom equivalents or add a second library for the same capability.

## Configuration Contract

Configuration loading remains file-path based and continues through Configor:

```go
func LoadConfig(path string) (*Config, error)
```

The implementation retains the current sequence:

```text
configor.Load
    -> normalizeAndValidate
    -> return Config
```

The existing `Config`, `PlatformConfig`, `BotConfig`, `ConversationConfig`, `MySQLConfig`, and associated field tags remain the migration source of truth. `PlatformConfig` and the supported protocol constants move to `ai`; the root package aliases `PlatformConfig`, and root `Config.Platforms` uses that alias. `Config.Current` therefore returns the same root alias without making `ai` depend on root configuration. The existing JSON, YAML, TOML, example fallback, environment overlay, and shell environment override behavior must not change.

The bundled CLI continues to resolve `CONFIG_PATH`, defaulting to `config.json`, before calling `LoadConfig`. The SDK does not guess a path or read `CONFIG_PATH` inside `New`.

`New` accepts a loaded configuration:

```go
func New(config *Config) (*Agent, error)
```

`New` defensively copies and validates the configuration before using it so caller mutation after construction cannot change a running Agent. It does not decode configuration bytes or maintain a second configuration representation.

## Public Root API

The root SDK exposes:

```go
func LoadConfig(path string) (*Config, error)
func New(config *Config) (*Agent, error)

func (a *Agent) Run(
	ctx context.Context,
	request RunRequest,
) (RunResult, error)

func (a *Agent) Close(ctx context.Context) error
```

The root package aliases common lower-level types so normal business callers need only one import:

```go
type Role = ai.Role
type Message = ai.Message
type ContentBlock = ai.ContentBlock
type ToolCall = ai.ToolCall
type PlatformConfig = ai.PlatformConfig

type RunRequest = agent.RunRequest
type RunResult = agent.RunResult
type ContextBlock = agent.ContextBlock
```

It provides small message-construction helpers:

```go
func TextBlock(text string) ContentBlock
func UserMessage(text string) Message
func SystemMessage(text string) Message
```

The root API does not re-export or accept:

- `ai.Client` or Provider implementations;
- Tool, Registry, or Middleware implementations;
- Agent Loop or Scheduler implementations;
- Reporter or lifecycle events;
- Workspace loaders;
- Conversation Stores.

## Run Contract

The public request remains:

```go
type RunRequest struct {
	RunID    string
	History  []Message
	Input    Message
	Context  []ContextBlock
	Metadata map[string]string
}
```

The public result remains:

```go
type RunResult struct {
	RunID       string
	NewMessages []Message
}
```

Business callers own user and conversation identifiers. The SDK does not add `UserID` or `ConversationID` to `RunRequest`; callers may place opaque identifiers in `Metadata` when useful for tracing.

The SDK does not persist History or NewMessages. `NewMessages` contains only messages created by the current action/tool loop and remains safe for caller persistence without duplicating system context, external context, History, Input, or internal thinking scaffolding.

## Runtime Data Flow

```text
business loads History
        -> reagent.Agent.Run
        -> validate RunRequest
        -> rediscover AGENTS.md and Skills
        -> compose system/workspace context
        -> order and append external Context
        -> append History
        -> append current Input
        -> agent runtime
            -> ai client generation
            -> tool registry and scheduler
            -> repeat until final assistant message
        -> return RunResult.NewMessages
        -> business persists messages
```

The SDK does not perform history queries, message persistence, session locking, database transactions, or channel delivery.

## Shared Fx Composition

The SDK and CLI share one private Fx graph:

```text
internal/bootstrap.Module
├── configuration-derived AI client
├── workspace services
├── default tools and Registry
├── Agent runtime
└── process lifecycle resources
```

The root `bootstrap.go` creates and starts an Fx App from this module, populates the internal runtime handle, and stores the App on the public Agent facade for later shutdown.

The bundled CLI creates one Fx App containing both:

```text
internal/bootstrap.Module
+ internal/cli.Module
```

This prevents nested Fx Apps and duplicate component construction. The private module is not exposed through the root SDK, so callers cannot add, remove, or replace default components through `reagent.New`.

## Error Contract

### Stable error codes

The root package defines a string enum in `error_code.go`:

```go
type ErrorCode string

const (
	ErrorCodeUnknown          ErrorCode = "unknown"
	ErrorCodeConfigLoad       ErrorCode = "config_load_failed"
	ErrorCodeConfigInvalid    ErrorCode = "config_invalid"
	ErrorCodeInitialization   ErrorCode = "initialization_failed"
	ErrorCodeRequestInvalid   ErrorCode = "request_invalid"
	ErrorCodeWorkspaceInvalid ErrorCode = "workspace_invalid"
	ErrorCodeAIGeneration     ErrorCode = "ai_generation_failed"
	ErrorCodeToolRuntime      ErrorCode = "tool_runtime_failed"
	ErrorCodeCanceled         ErrorCode = "canceled"
	ErrorCodeDeadlineExceeded ErrorCode = "deadline_exceeded"
	ErrorCodeClosed           ErrorCode = "agent_closed"
	ErrorCodeInternal         ErrorCode = "internal"
)
```

String values are part of the public compatibility contract and are suitable for logs, HTTP/RPC responses, storage, and cross-language clients.

### Wrapped error

`error.go` defines:

```go
type Error struct {
	Code ErrorCode
	Op   string
	Err  error
}

func (e *Error) Error() string
func (e *Error) Unwrap() error
func ErrorCodeOf(err error) ErrorCode
```

Public boundaries classify an error once while preserving its original cause through `Unwrap`. `errors.Is` continues to recognize `context.Canceled`, `context.DeadlineExceeded`, and `ErrClosed`; `errors.As` can obtain the structured SDK error.

The SDK does not maintain a mapping table for OpenAI- or Anthropic-specific error codes. Provider failures receive the coarse stable code `ai_generation_failed`, while the original official-SDK error remains in the unwrap chain.

### Partial results

Run retains normal Go partial-result semantics:

- failure before execution returns the request RunID and no new messages;
- failure after completed action/tool messages returns those messages together with an error;
- the caller should persist a non-empty `NewMessages` slice even when `err != nil` if its business transaction policy allows it;
- no persistence, retry, or resume behavior is implied by a partial result.

Ordinary tool execution errors remain `ToolResult{IsError: true}` messages and are returned to the model. Only a tool-runtime infrastructure failure that cannot continue the loop receives `tool_runtime_failed` and terminates the Run.

## Concurrency Contract

One long-lived `*reagent.Agent` supports concurrent `Run` calls.

Guarantees:

- every Run owns an independent message history;
- caller-owned Request slices and maps are not mutated;
- each result owns an independently allocated `NewMessages` slice;
- model clients are reused through their official SDK implementations;
- the Tool Registry is immutable after initialization;
- the existing Process Supervisor remains synchronized;
- workspace AGENTS and Skills are rediscovered for every Run;
- canceling one Run does not cancel another;
- the SDK does not serialize calls sharing a business ConversationID.

Conversation-level serialization, optimistic conflicts, retries, and queueing remain business responsibilities.

## Close Contract

`Agent.Close(ctx)`:

1. atomically rejects new Runs with `ErrClosed`;
2. waits for already-started Runs to finish;
3. invokes Fx Stop Hooks;
4. terminates remaining background tool processes;
5. respects the caller's shutdown deadline;
6. is idempotent and returns the first close result on later calls.

Closing one Run's context never closes the Agent. A closed Agent cannot be restarted.

## CLI, Conversation, MySQL, and Dispatch

The bundled CLI retains its current optional conversation flow:

```text
CLI input
    -> conversation Runner loads bounded History
    -> shared runtime Run
    -> conversation Runner persists Input + NewMessages
    -> Terminal/WeCom dispatch
```

The MySQL implementation remains split by responsibility:

- `internal/cli/driver/mysql` wraps `go-mysql-sdk`, its Provider, transaction Manager, connection pool, and lifecycle;
- `internal/cli/conversation/mysql` owns GORM models, message codecs, safe history windows, optimistic version checks, and Store behavior.

The migration does not merge these responsibilities, use raw `database/sql` in place of the SDK, introduce AutoMigrate, or change the migration SQL.

The root SDK remains synchronous and does not accept a Reporter. The lower `agent` package retains the event contract required for the bundled CLI to preserve its existing Terminal and WeCom progress behavior.

## Migration Mapping

| Current path | Target path |
| --- | --- |
| `internal/schema/content.go` | `ai/content.go` |
| `internal/schema/message.go` | `ai/message.go` |
| `ToolDefinition` from `internal/schema/message.go` | `ai` |
| `internal/schema/event.go` | `agent/event.go`, referencing `ai` message/tool types |
| `internal/schema/run.go` | `agent/run.go` |
| `internal/provider` | `ai` and `ai/providers/*` |
| `internal/engine/agent_loop.go` | `agent/loop.go` |
| `internal/engine/tool_scheduler.go` | `agent/scheduler.go` |
| generic tool contracts and runtime | `agent` |
| concrete default tools | `internal/tools` |
| `internal/context` | `internal/workspace` |
| `internal/app` | `internal/cli/app` |
| `internal/conversation` | `internal/cli/conversation` |
| `internal/driver/mysql` | `internal/cli/driver/mysql` |
| `internal/dispatch` | `internal/cli/dispatch` |
| `internal/register.go` | shared `internal/bootstrap.Module` plus `internal/cli.Module` |
| `cmd/main.go` | `cmd/reagent/main.go` |
| root `ping.go` | `cmd/ping/main.go` |

The migration is performed dependency-first:

1. establish `ai` and update model/message imports;
2. establish `agent` and update run/engine/tool-runtime imports;
3. separate generic tool contracts from concrete default tools;
4. move workspace product policy;
5. add the root facade, configuration API, and error enum;
6. establish the shared private Fx module;
7. relocate CLI-only packages;
8. update command entry points, tests, README, and diagrams;
9. remove emptied old packages.

Intermediate commits may be used, but the completed branch contains no alias-based or deprecated compatibility layer.

## Test Design

### `ai`

Tests cover:

- Message, Content, and ToolCall JSON round trips;
- OpenAI and Anthropic request/response conversion;
- tool argument preservation;
- model, BaseURL, and API-key propagation;
- original official-SDK errors in the unwrap chain;
- no real network requests in unit tests.

### `agent`

Tests cover:

- direct final responses;
- multi-turn tool loops;
- thinking scaffolding exclusion from NewMessages;
- tool-call, tool-result, and final-message ordering;
- parallel-safe scheduling and exclusive barriers;
- JSON Schema validation through the existing library;
- ordinary tool-error feedback;
- partial results on generation or runtime failure;
- cancellation and deadlines;
- concurrent Run isolation;
- caller Request immutability.

### Root SDK

External-package tests use `package reagent_test` and only the documented root API. They cover:

- Configor JSON, YAML, TOML, and environment behavior;
- existing normalization and validation;
- Fx graph construction and startup;
- long-lived concurrent Run calls;
- partial results;
- stable ErrorCode classification and error unwrapping;
- graceful and idempotent Close;
- rejection of Run after Close.

### Workspace, tools, and CLI

Existing tests move with their implementation and retain coverage for:

- workspace escape and symlink protection;
- AGENTS and Skill validation/discovery;
- prompt and Context/History/Input order;
- read pagination and output budgets;
- edit, apply-patch, exec, process, and supervisor behavior;
- Middleware ordering and output limits;
- `go-mysql-sdk` initialization and transaction usage;
- GORM conversation storage and safe history windows;
- optimistic conversation conflicts;
- Terminal and WeCom behavior;
- the complete Fx CLI dependency graph.

## Verification

The implementation is not complete until all applicable checks pass:

```bash
gofmt -w .
go test ./...
go test -race ./...
go vet ./...
git diff --check
```

MySQL integration tests continue to follow their existing environment requirements. The refactor does not weaken or replace their execution policy.

## Completion Criteria

- business callers can import `github.com/PycMono/go-reagent` and use `LoadConfig`, `New`, `Run`, and `Close`;
- the root SDK is synchronous and stateless across Runs;
- one Agent instance safely supports concurrent Runs;
- History loading and NewMessages persistence remain caller-owned;
- `ai` depends on no Agent or product package;
- `agent` depends on `ai` but no product adapter;
- workspace and concrete default tools remain product-private;
- CLI conversation, MySQL, and dispatch remain outside the root Run path;
- Configor, official model SDKs, Fx, logger SDK, YAML library, JSON Schema library, MySQL SDK, transaction manager, GORM, and existing test libraries remain in use;
- public errors expose stable string ErrorCodes and preserve underlying causes;
- existing behavior and new public-contract tests pass;
- no old-package compatibility layer remains.
