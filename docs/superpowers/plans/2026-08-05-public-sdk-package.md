# Public SDK Package Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reorganize go-reagent into the public `ai -> agent -> reagent` package stack, while keeping workspace policy, default tools, persistence, MySQL, dispatch, and CLI lifecycle private.

**Architecture:** `ai` defines model messages and official-SDK-backed clients; `agent` defines the reusable run loop, tool runtime, scheduling, and low-level events; the module-root `reagent` package builds a synchronous, stateless, concurrency-safe default Agent through one private Fx module. The bundled command composes that same private module with CLI-only conversation, MySQL, and dispatch adapters, without adding public replacement hooks or a second configuration path.

**Tech Stack:** Go 1.26, `github.com/jinzhu/configor`, `github.com/openai/openai-go/v3`, `github.com/anthropics/anthropic-sdk-go`, `go.uber.org/fx`, `github.com/PycMono/go-logger-sdk`, `github.com/santhosh-tekuri/jsonschema/v6`, `gopkg.in/yaml.v3`, `github.com/PycMono/go-mysql-sdk`, `github.com/PycMono/go-mysql-sdk/transaction`, and `gorm.io/gorm`.

## Global Constraints

- Execute this plan from an isolated worktree created with `superpowers:using-git-worktrees`; the source checkout currently has user-owned changes in `internal/context/run_context.go`, `internal/conversation/runner.go`, `internal/conversation/store.go`, and `internal/schema/run.go`, and those changes must not be staged, overwritten, or folded into migration commits.
- Keep `github.com/jinzhu/configor` as the only configuration loader and retain JSON, YAML, TOML, example fallback, Configor environment overlay, and current shell-environment behavior.
- Keep the official OpenAI and Anthropic SDKs, Fx, `go-logger-sdk`, `jsonschema/v6`, `yaml.v3`, `go-mysql-sdk`, its transaction manager, GORM, sqlmock, and the existing GORM MySQL test driver in their current roles.
- Do not add aliases or forwarding functions for the old `internal/schema`, `internal/provider`, `internal/engine`, `internal/context`, `internal/app`, `internal/conversation`, `internal/driver`, or `internal/dispatch` import paths; remove each old package after its consumers migrate.
- The root `reagent.New` API accepts only `*reagent.Config`; it must not expose Provider, Tool, Reporter, Store, Registry, Middleware, Scheduler, or Fx injection.
- The root `Agent.Run` is synchronous and stateless, does not load or persist conversation data, and does not expose progress events.
- A single long-lived root Agent must support concurrent Runs, isolate caller-owned slices/maps/JSON arguments, and reject new Runs after Close begins.
- Preserve partial results: return completed `NewMessages` together with an error, and never include system context, external context, History, Input, or thinking scaffolding in `NewMessages`.
- Preserve the existing six default tools, AGENTS/Skill behavior, terminal and WeCom behavior, MySQL migrations, history windows, and optimistic conversation version checks.
- Use narrow `git add` paths in every commit. Do not use `git add .`.

---

## File and Interface Map

The implementation locks in these ownership boundaries before task work begins:

| Path | Responsibility |
| --- | --- |
| `ai/content.go`, `ai/message.go` | Public message, content, tool-call, and tool-definition wire types |
| `ai/model.go` | `Protocol` string enum and `PlatformConfig` |
| `ai/client.go`, `ai/errors.go` | Unified model client and generation-error sentinel/wrapper |
| `ai/providers/factory.go` | Protocol selection without creating a Go parent/subpackage import cycle |
| `ai/providers/openai/*`, `ai/providers/anthropic/*` | Official SDK adapters and conversions |
| `agent/run.go`, `agent/validation.go` | Stateless request/result/context contracts, deep cloning, request validation |
| `agent/event.go`, `agent/reporter.go` | Low-level lifecycle events and deterministic reporter fan-out |
| `agent/tool.go`, `agent/registry.go`, `agent/middleware.go` | Generic tool contracts, immutable registry, JSON Schema validation, middleware |
| `agent/scheduler.go`, `agent/loop.go`, `agent/agent.go` | Ordered tool scheduling, model/tool loop, reusable runtime facade |
| `internal/workspace/*` | WorkDir, AGENTS, Skills, prompt composition, `agent.ContextFactory` |
| `internal/tools/*` | Concrete default tool implementations and process supervision |
| `internal/bootstrap/module.go` | Shared private Fx graph used by both root SDK and CLI |
| `config.go`, `config_validate.go` | Public Configor-backed configuration and validation |
| `types.go`, `error_code.go`, `error.go` | Root aliases/helpers and stable error contract |
| `reagent.go`, `bootstrap.go` | Concurrency-safe root Agent facade and private Fx startup |
| `internal/cli/*` | Process configuration, one-shot lifecycle, persistence, MySQL, Terminal, WeCom |
| `cmd/reagent/*`, `cmd/ping/*` | Process entry points only |

The final interfaces are:

```go
// ai
type Client interface {
	Generate(context.Context, []Message, []ToolDefinition) (*Message, error)
}

// agent
type ContextFactory interface {
	Create(context.Context, RunRequest, []ai.ToolDefinition) (RunContext, error)
}
type Runner interface {
	Run(context.Context, RunRequest, Reporter) (RunResult, error)
}

// reagent root
func LoadConfig(path string) (*Config, error)
func New(config *Config) (*Agent, error)
func (a *Agent) Run(context.Context, RunRequest) (RunResult, error)
func (a *Agent) Close(context.Context) error
```

Because Go forbids `ai` importing `ai/providers/openai` while that subpackage imports `ai`, protocol selection lives at `ai/providers.New`. It remains part of the AI layer, and `internal/bootstrap` is its only product-level consumer.

### Task 1: Separate executable packages from the module root

**Files:**
- Move: `cmd/main.go` -> `cmd/reagent/main.go`
- Move: `cmd/main_test.go` -> `cmd/reagent/main_test.go`
- Move: `ping.go` -> `cmd/ping/main.go`
- Create: `cmd/ping/main_test.go`

**Interfaces:**
- Consumes: existing `main()`, `newApplicationLogger()`, and `pingHandler(http.ResponseWriter, *http.Request)` behavior.
- Produces: an empty module root ready for `package reagent`, plus independently testable `cmd/reagent` and `cmd/ping` executables.

- [ ] **Step 1: Protect the source checkout before moving files**

Run:

```bash
git status --short
git diff -- internal/context/run_context.go internal/conversation/runner.go internal/conversation/store.go internal/schema/run.go
```

Expected: the isolated worktree is clean; if those four paths appear, stop and create a clean worktree rather than touching them.

- [ ] **Step 2: Write the ping handler test at its target path**

```go
package main

import (
	"net/http/httptest"
	"testing"
)

func TestPingHandler(t *testing.T) {
	response := httptest.NewRecorder()
	pingHandler(response, httptest.NewRequest("GET", "/ping", nil))
	if response.Body.String() != "pong" {
		t.Fatalf("body = %q, want pong", response.Body.String())
	}
}
```

- [ ] **Step 3: Verify the target packages do not exist yet**

Run: `go test ./cmd/reagent ./cmd/ping`

Expected: FAIL with `stat .../cmd/reagent: directory not found` and `stat .../cmd/ping: directory not found`.

- [ ] **Step 4: Move the executable files without changing behavior**

Run:

```bash
mkdir -p cmd/reagent cmd/ping
git mv cmd/main.go cmd/reagent/main.go
git mv cmd/main_test.go cmd/reagent/main_test.go
git mv ping.go cmd/ping/main.go
```

Add `cmd/ping/main_test.go` with `apply_patch` using Step 2's exact test.

- [ ] **Step 5: Verify both commands pass**

Run: `go test ./cmd/reagent ./cmd/ping`

Expected: PASS for both command packages.

- [ ] **Step 6: Commit the executable split**

```bash
git add -A -- cmd/main.go cmd/main_test.go ping.go cmd/reagent cmd/ping
git commit -m "refactor: separate command entry points"
```

### Task 2: Publish AI messages, configuration enum, and client contract

**Files:**
- Create: `ai/content.go`
- Create: `ai/message.go`
- Create: `ai/model.go`
- Create: `ai/client.go`
- Create: `ai/errors.go`
- Move/rewrite: `internal/schema/message_test.go` -> `ai/message_test.go`
- Modify: every current import of `internal/schema` that uses message/content/tool-definition types
- Modify: `internal/config/config.go`
- Modify: `internal/config/validate.go`
- Delete: `internal/schema/content.go`
- Delete: `internal/schema/message.go`

**Interfaces:**
- Consumes: current JSON field names and the current `TextBlock`/`TextContent` behavior.
- Produces: `ai.Role`, `ai.ContentType`, `ai.ContentBlock`, `ai.Message`, `ai.ToolCall`, `ai.ToolDefinition`, `ai.Protocol`, `ai.PlatformConfig`, `ai.Client`, `ai.ErrGeneration`, and `ai.WrapGeneration`.

- [ ] **Step 1: Add a failing public AI contract test**

```go
package ai_test

import (
	"encoding/json"
	"testing"

	"github.com/PycMono/go-reagent/ai"
)

func TestMessageRoundTripPreservesToolArguments(t *testing.T) {
	want := ai.Message{
		Role: ai.RoleAssistant,
		ToolCalls: []ai.ToolCall{{
			ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"AGENTS.md"}`),
		}},
	}
	encoded, err := json.Marshal(want)
	if err != nil { t.Fatal(err) }
	var got ai.Message
	if err := json.Unmarshal(encoded, &got); err != nil { t.Fatal(err) }
	if string(got.ToolCalls[0].Arguments) != `{"path":"AGENTS.md"}` {
		t.Fatalf("arguments = %s", got.ToolCalls[0].Arguments)
	}
}

func TestProtocolValuesAreStable(t *testing.T) {
	if ai.ProtocolOpenAI != "openai" || ai.ProtocolAnthropic != "anthropic" {
		t.Fatalf("protocols = %q, %q", ai.ProtocolOpenAI, ai.ProtocolAnthropic)
	}
}
```

- [ ] **Step 2: Verify the public package is missing**

Run: `go test ./ai`

Expected: FAIL with `no required module provides package github.com/PycMono/go-reagent/ai`.

- [ ] **Step 3: Add the public enum and model profile**

```go
package ai

type Protocol string

const (
	ProtocolOpenAI    Protocol = "openai"
	ProtocolAnthropic Protocol = "anthropic"
)

type PlatformConfig struct {
	ID       string   `json:"id" yaml:"id" toml:"id"`
	Protocol Protocol `json:"protocol" yaml:"protocol" toml:"protocol"`
	BaseURL  string   `json:"baseURL" yaml:"baseURL" toml:"baseURL"`
	APIKey   string   `json:"apiKey" yaml:"apiKey" toml:"apiKey"`
	Model    string   `json:"model" yaml:"model" toml:"model"`
}
```

- [ ] **Step 4: Move the message implementation and define the client**

Move the bodies of `internal/schema/content.go` and `internal/schema/message.go` into `ai/content.go` and `ai/message.go`, changing only `package schema` to `package ai`. Add:

```go
package ai

import "context"

type Client interface {
	Generate(context.Context, []Message, []ToolDefinition) (*Message, error)
}
```

- [ ] **Step 5: Add the generation error wrapper**

```go
package ai

import (
	"errors"
	"fmt"
)

var ErrGeneration = errors.New("ai generation failed")

type GenerationError struct {
	Op  string
	Err error
}

func (e *GenerationError) Error() string { return fmt.Sprintf("%s: %v", e.Op, e.Err) }
func (e *GenerationError) Unwrap() error { return e.Err }
func (e *GenerationError) Is(target error) bool { return target == ErrGeneration }

func WrapGeneration(op string, err error) error {
	if err == nil { return nil }
	return &GenerationError{Op: op, Err: err}
}
```

- [ ] **Step 6: Rewrite message-type imports dependency-first**

Use these exact replacements while keeping temporary run/event structs in `internal/schema` until Task 4:

```text
schema.Message        -> ai.Message
schema.ContentBlock   -> ai.ContentBlock
schema.ContentType*   -> ai.ContentType*
schema.TextBlock      -> ai.TextBlock
schema.TextContent    -> ai.TextContent
schema.ToolCall       -> ai.ToolCall
schema.ToolDefinition -> ai.ToolDefinition
```

Update `internal/schema/run.go` and `internal/schema/event.go` to import `ai`, so no duplicate message type remains. Move and rewrite the existing message/content tests into `ai/message_test.go`.

Also remove `PlatformConfig` and protocol constants from `internal/config`: `Config.Platforms` becomes `[]ai.PlatformConfig`, `Current` returns `ai.PlatformConfig`, and validation compares `platform.Protocol` with `ai.ProtocolOpenAI`/`ai.ProtocolAnthropic`. Do not create aliases in the old config package.

- [ ] **Step 7: Remove old message files and verify**

Run:

```bash
go test ./ai ./internal/schema ./internal/provider ./internal/engine ./internal/tools ./internal/context ./internal/conversation/...
```

Expected: PASS; `rg 'internal/schema' ai` returns no matches.

- [ ] **Step 8: Commit the AI contract**

```bash
git add -A -- ai internal/config internal/schema internal/provider internal/engine internal/tools internal/context internal/conversation internal/dispatch tests
git commit -m "refactor: publish ai message contracts"
```

### Task 3: Move official model adapters into the AI layer

**Files:**
- Create: `ai/providers/factory.go`
- Create: `ai/providers/factory_test.go`
- Move: `internal/provider/openai.go` -> `ai/providers/openai/client.go`
- Move: `internal/provider/openai_test.go` -> `ai/providers/openai/client_test.go`
- Move: `internal/provider/claude.go` -> `ai/providers/anthropic/client.go`
- Move: `internal/provider/claude_test.go` -> `ai/providers/anthropic/client_test.go`
- Delete: `internal/provider/interface.go`
- Delete: `internal/provider/factory.go`
- Delete: `internal/provider/factory_test.go`
- Delete: `internal/provider/register.go`
- Delete: `internal/provider/register_test.go`
- Modify: `internal/engine/agent_loop.go`
- Modify: `internal/engine/register.go`
- Modify: `internal/register.go`

**Interfaces:**
- Consumes: `ai.Client`, `ai.PlatformConfig`, `ai.ProtocolOpenAI`, `ai.ProtocolAnthropic`, and both current official SDKs.
- Produces: `providers.New(ai.PlatformConfig) (ai.Client, error)`, `openai.New(ai.PlatformConfig) ai.Client`, and `anthropic.New(ai.PlatformConfig) ai.Client`.

- [ ] **Step 1: Write the failing protocol-selection test**

```go
package providers_test

import (
	"testing"

	"github.com/PycMono/go-reagent/ai"
	"github.com/PycMono/go-reagent/ai/providers"
)

func TestNewSelectsSupportedProtocol(t *testing.T) {
	base := ai.PlatformConfig{ID: "test", BaseURL: "https://example.com/", APIKey: "key", Model: "model"}
	for _, protocol := range []ai.Protocol{ai.ProtocolOpenAI, ai.ProtocolAnthropic} {
		config := base
		config.Protocol = protocol
		client, err := providers.New(config)
		if err != nil || client == nil { t.Fatalf("New(%q) = %T, %v", protocol, client, err) }
	}
}
```

- [ ] **Step 2: Verify the provider package is missing**

Run: `go test ./ai/providers/...`

Expected: FAIL because `ai/providers` does not exist.

- [ ] **Step 3: Move the OpenAI adapter and split conversion helpers**

Use `package openai`, import the official SDK as `openaisdk`, replace schema types with `ai` types, rename `OpenAIProvider` to `Client`, and expose:

```go
func New(config ai.PlatformConfig) ai.Client {
	return &Client{
		client: openaisdk.NewClient(
			option.WithAPIKey(config.APIKey),
			option.WithBaseURL(config.BaseURL),
		),
		model: config.Model,
		name:  config.ID,
	}
}
```

Wrap message conversion, tool conversion, empty choices, and official request failures with `ai.WrapGeneration("openai generate", err)`. Preserve the official SDK error as the innermost cause.

- [ ] **Step 4: Move the Anthropic adapter and split conversion helpers**

Use `package anthropic`, import the official SDK as `anthropicsdk`, replace schema types with `ai` types, rename `ClaudeProvider` to `Client`, and expose:

```go
func New(config ai.PlatformConfig) ai.Client {
	return &Client{
		client: anthropicsdk.NewClient(
			option.WithAPIKey(config.APIKey),
			option.WithBaseURL(config.BaseURL),
		),
		model: config.Model,
		name:  config.ID,
	}
}
```

Keep `MaxTokens: 4096`, tool-result conversion, and raw tool arguments unchanged; wrap failures with `ai.WrapGeneration("anthropic generate", err)`.

- [ ] **Step 5: Implement validation and protocol selection**

```go
func New(config ai.PlatformConfig) (ai.Client, error) {
	if strings.TrimSpace(config.APIKey) == "" { return nil, errors.New("apiKey 不能为空") }
	if strings.TrimSpace(config.Model) == "" { return nil, errors.New("model 不能为空") }
	if strings.TrimSpace(config.BaseURL) == "" { return nil, errors.New("baseURL 不能为空") }
	switch config.Protocol {
	case ai.ProtocolOpenAI:
		return openai.New(config), nil
	case ai.ProtocolAnthropic:
		return anthropic.New(config), nil
	default:
		return nil, fmt.Errorf("不支持的 Provider protocol %q，可选值: openai, anthropic", config.Protocol)
	}
}
```

- [ ] **Step 6: Verify adapters without network access**

Before running tests, change the transitional engine constructor from `provider.LLMProvider` to `ai.Client`, and replace `provider.Register` in `internal.Register` with this private constructor:

```go
func newAIClient(config *config.Config) (ai.Client, error) {
	platform, err := config.Current()
	if err != nil { return nil, err }
	return providers.New(platform)
}
```

Register it with `fx.Provide(newAIClient)`. This constructor disappears when `internal/bootstrap.Module` takes ownership in Task 8; it is not a public compatibility layer.

Run: `go test ./ai/...`

Expected: PASS; existing httptest assertions still verify authorization headers, BaseURL paths, model names, omitted thinking tools, and native tool-result mapping.

- [ ] **Step 7: Remove the old provider package and commit**

Run: `test ! -d internal/provider && go test ./...`

Expected: PASS in the clean worktree.

```bash
git add -A -- ai internal/provider internal/engine internal/register.go tests
git commit -m "refactor: move model clients into ai layer"
```

### Task 4: Publish Agent run, event, reporter, and generic tool contracts

**Files:**
- Move/rewrite: `internal/schema/run.go` -> `agent/run.go`
- Create: `agent/validation.go`
- Move/rewrite: `internal/schema/event.go` -> `agent/event.go`
- Move/rewrite: `internal/engine/reporter.go` -> `agent/reporter.go`
- Create: `agent/tool.go`
- Move: `internal/schema/event_test.go` -> `agent/event_test.go`
- Move: `internal/engine/reporter_test.go` -> `agent/reporter_test.go`
- Move: `internal/tools/registry.go` -> `agent/registry.go`
- Move: `internal/tools/registry_test.go` -> `agent/registry_test.go`
- Move: `internal/tools/middleware.go` -> `agent/middleware.go`
- Move: `internal/tools/middleware_test.go` -> `agent/middleware_test.go`
- Move: `internal/tools/schema_validator.go` -> `agent/schema_validator.go`
- Move: `internal/tools/schema_validator_test.go` -> `agent/schema_validator_test.go`
- Delete: `internal/tools/tool.go`
- Delete: emptied `internal/schema/`
- Create: `internal/tools/output.go`
- Modify: `internal/tools/register.go`

**Interfaces:**
- Consumes: public `ai` message/tool types and existing `jsonschema/v6` behavior.
- Produces: `agent.RunRequest`, `RunResult`, `ContextBlock`, `RunContext`, `ContextFactory`, Reporter/event contracts, Tool/Registry/Middleware contracts, `RegistryOptions`, `NewRegistry`, `DefaultMiddlewareRegistrations`, `ErrRequestInvalid`, and `ErrToolRuntime`.

- [ ] **Step 1: Write a failing external registry test**

```go
package agent_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/PycMono/go-reagent/agent"
	"github.com/PycMono/go-reagent/ai"
)

type echoTool struct{}
func (echoTool) Definition() ai.ToolDefinition {
	return ai.ToolDefinition{Name: "echo", Description: "echo text", InputSchema: map[string]any{
		"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string"}}, "required": []string{"text"},
	}}
}
func (echoTool) Execute(_ context.Context, raw json.RawMessage, _ agent.UpdateEmitter) (agent.ToolOutput, error) {
	return agent.ToolOutput{Content: []ai.ContentBlock{ai.TextBlock(string(raw))}}, nil
}

func TestRegistryValidatesArgumentsWithJSONSchema(t *testing.T) {
	registry, err := agent.NewRegistry(agent.RegistryOptions{
		Tools: []agent.Tool{echoTool{}}, Middlewares: agent.DefaultMiddlewareRegistrations(),
	})
	if err != nil { t.Fatal(err) }
	result, err := registry.Execute(context.Background(), ai.ToolCall{ID: "1", Name: "echo", Arguments: []byte(`{}`)}, nil)
	if err != nil || !result.IsError { t.Fatalf("result/error = %#v, %v", result, err) }
}
```

- [ ] **Step 2: Verify the Agent package is missing**

Run: `go test ./agent`

Expected: FAIL because `agent` does not exist.

- [ ] **Step 3: Define the run and context contracts**

```go
type RunRequest struct {
	RunID    string            `json:"run_id,omitempty"`
	History  []ai.Message      `json:"history,omitempty"`
	Input    ai.Message        `json:"input"`
	Context  []ContextBlock    `json:"context,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}
type ContextBlock struct {
	Name string `json:"name"`; Content string `json:"content"`; Priority int `json:"priority,omitempty"`
}
type RunResult struct {
	RunID string `json:"run_id,omitempty"`; NewMessages []ai.Message `json:"new_messages,omitempty"`
}
type RunContext struct {
	Messages []ai.Message; Tools []ai.ToolDefinition; Metadata map[string]string
}
type ContextFactory interface {
	Create(context.Context, RunRequest, []ai.ToolDefinition) (RunContext, error)
}
```

Add `cloneRequest`, `cloneMessage`, `cloneMessages`, and `cloneMetadata` in `agent/run.go`; clone `Content`, every `ToolCalls` slice, every `ToolCall.Arguments` byte slice, `Context`, and `Metadata`.

- [ ] **Step 4: Move events and reporter fan-out**

Replace all schema references with `ai` references. Keep event string values exactly `thinking`, `tool_start`, `tool_update`, `tool_end`, and `message`; keep tool phases exactly `start`, `update`, and `end`. The Reporter interface remains:

```go
type Reporter interface { Report(context.Context, AgentEvent) }
type ReporterRegistration struct { Name string; Order int; Reporter Reporter }
func NewMultiReporter([]ReporterRegistration) Reporter
```

- [ ] **Step 5: Move generic tool runtime out of default tools**

Define `agent.Tool`, `Registry`, `Execution`, `Handler`, `Middleware`, and `RegistryOptions` without Fx annotations:

```go
type RegistryOptions struct {
	Tools       []Tool
	Middlewares []MiddlewareRegistration
}
func NewRegistry(options RegistryOptions) (Registry, error)
func DefaultMiddlewareRegistrations() []MiddlewareRegistration
```

Keep middleware order `10 recovery`, `20 context`, `30 schema_validation`, `40 logging`, `50 output_limit`, `60 event_forwarding`; keep the 50 KiB output cap and `jsonschema/v6` compiler.

- [ ] **Step 6: Add stable lower-layer error sentinels and request validation**

```go
var (
	ErrRequestInvalid = errors.New("agent request invalid")
	ErrToolRuntime    = errors.New("agent tool runtime failed")
)
```

Move request-shape checks from the workspace factory into `agent/validation.go`. Every invalid Input/Context error wraps `ErrRequestInvalid`; nil/canceled contexts preserve `context.Canceled` and `context.DeadlineExceeded` through `%w`.

- [ ] **Step 7: Convert concrete tools to the public contracts**

Change concrete implementations to return `agent.ToolOutput`, accept `agent.UpdateEmitter`, and return `ai.ToolDefinition`. Add:

```go
package tools

func textToolOutput(text string) agent.ToolOutput {
	if text == "" { return agent.ToolOutput{} }
	return agent.ToolOutput{Content: []ai.ContentBlock{ai.TextBlock(text)}}
}
```

Update the Fx group annotations from `tools.Tool` to `agent.Tool`; Task 8 will move registry construction into bootstrap.

Keep the intermediate graph buildable by adding this private constructor to `internal/tools/register.go`:

```go
type registryParams struct {
	fx.In
	Tools []agent.Tool `group:"agent_tools"`
}

func newRegistry(params registryParams) (agent.Registry, error) {
	return agent.NewRegistry(agent.RegistryOptions{
		Tools: params.Tools,
		Middlewares: agent.DefaultMiddlewareRegistrations(),
	})
}
```

Provide `newRegistry` from the existing tools Fx option. Task 8 moves this exact constructor to `internal/bootstrap` and removes it here.

- [ ] **Step 8: Verify contracts, schema validation, middleware, and tools**

Run:

```bash
go test ./agent ./internal/tools ./internal/context ./internal/engine ./internal/conversation/... ./internal/dispatch
```

Expected: PASS with ordinary tool execution errors still returned as `ToolResult{IsError:true}`.

- [ ] **Step 9: Remove internal schema and commit**

```bash
test ! -d internal/schema
git add -A -- agent internal/schema internal/tools internal/context internal/engine internal/conversation internal/dispatch tests
git commit -m "refactor: publish agent tool and run contracts"
```

### Task 5: Move the scheduler, loop, and reusable runtime into `agent`

**Files:**
- Move: `internal/engine/tool_scheduler.go` -> `agent/scheduler.go`
- Move: `internal/engine/tool_scheduler_test.go` -> `agent/scheduler_test.go`
- Move: `internal/engine/run_validation.go` -> `agent/response_validation.go`
- Move: `internal/engine/agent_loop.go` -> `agent/loop.go`
- Move: `internal/engine/loop_test.go` -> `agent/loop_test.go`
- Move: `internal/engine/run_messages_test.go` -> `agent/run_messages_test.go`
- Move: `internal/engine/runtime.go` -> `agent/agent.go`
- Move: `internal/engine/runtime_test.go` -> `agent/agent_test.go`
- Modify: `internal/engine/register.go`
- Modify: `internal/engine/register_test.go`
- Move later in Task 9: `internal/engine/terminal_reporter.go` and its test

**Interfaces:**
- Consumes: `ai.Client`, `agent.ContextFactory`, `agent.Registry`, `agent.Reporter`.
- Produces: `agent.Scheduler`, `NewScheduler`, `agent.Loop`, `NewLoop`, `agent.Agent`, `agent.Runner`, and `New`.

- [ ] **Step 1: Add failing concurrent isolation and partial-result tests**

```go
func TestAgentRunReturnsIndependentPartialMessages(t *testing.T) {
	client := &scriptedClient{responses: []*ai.Message{{
		Role: ai.RoleAssistant,
		ToolCalls: []ai.ToolCall{{ID: "call-1", Name: "missing", Arguments: []byte(`{}`)}},
	}}}
	runtime := newTestAgent(t, client)
	result, err := runtime.Run(context.Background(), validRequest("run-1", "hello"), nil)
	if err == nil || result.RunID != "run-1" || len(result.NewMessages) != 2 {
		t.Fatalf("result/error = %#v, %v", result, err)
	}
}
```

Also migrate the current concurrent scheduler tests and add a 16-goroutine test that mutates the original request only after all Runs return, asserting every captured request and result remains unchanged.

- [ ] **Step 2: Verify the public runtime is absent**

Run: `go test ./agent -run 'TestAgentRun|TestToolScheduler'`

Expected: FAIL with undefined `New`, `NewLoop`, or `NewScheduler`.

- [ ] **Step 3: Move and rename the scheduler**

Apply these names consistently:

```text
engine.ToolScheduler    -> agent.Scheduler
engine.NewToolScheduler -> agent.NewScheduler
tools.Registry          -> agent.Registry
schema.ToolCall         -> ai.ToolCall
schema.ToolDefinition   -> ai.ToolDefinition
schema.ToolResult       -> agent.ToolResult
```

Keep consecutive parallel-safe waves, exclusive barriers, result ordering, and the configured maximum parallelism unchanged.

- [ ] **Step 4: Move and rename the loop**

```text
provider.LLMProvider -> ai.Client
engine.AgentLoop     -> agent.Loop
engine.NewAgentLoop  -> agent.NewLoop
context.RunContext   -> agent.RunContext
```

Keep the thinking request tool-free; wrap every client failure with `ai.WrapGeneration("thinking", err)` or `ai.WrapGeneration("action", err)`. Wrap only non-cancellation scheduler infrastructure failures with `ErrToolRuntime`; ordinary tool errors remain messages.

- [ ] **Step 5: Implement the reusable Agent facade**

```go
type Runner interface {
	Run(context.Context, RunRequest, Reporter) (RunResult, error)
}

type Agent struct {
	factory  ContextFactory
	loop     *Loop
	registry Registry
}

func New(factory ContextFactory, loop *Loop, registry Registry) (*Agent, error)
func (a *Agent) Run(ctx context.Context, request RunRequest, reporter Reporter) (RunResult, error)
```

`Run` validates and deep-clones the request, obtains one per-run `RunContext`, executes the loop, and deep-clones `NewMessages` into the result. It never stores run messages on `Agent`.

- [ ] **Step 6: Verify runtime semantics and race safety**

Run:

```bash
go test ./agent
go test -race ./agent
```

Expected: PASS; thinking messages are absent from `NewMessages`, and tool call/result/final ordering matches existing tests.

- [ ] **Step 7: Update consumers and remove all engine code except terminal reporter**

Update conversation and app contracts to `agent.Runner`, `agent.Reporter`, `agent.RunRequest`, and `agent.RunResult`. Rewrite `internal/engine/register.go` as a transitional Fx composition option: it supplies the terminal registration and aggregated `agent.Reporter`, and it constructs `agent.Scheduler`, `agent.Loop`, and `agent.Agent` from the public constructors. Register `agent.New` both as `*agent.Agent` and `agent.Runner` with `fx.As(fx.Self())` plus `fx.As(new(agent.Runner))`. Task 8 moves those runtime constructors into bootstrap and reduces this option to reporter-only. Leave the option, `terminal_reporter.go`, and their tests under the old directory until Task 9; they move together and the directory is deleted there.

- [ ] **Step 8: Commit the Agent core**

```bash
git add -A -- agent internal/engine internal/context internal/conversation internal/app internal/dispatch tests
git commit -m "refactor: move runtime into agent package"
```

### Task 6: Move workspace product policy behind `agent.ContextFactory`

**Files:**
- Move: all remaining `internal/context/*.go` -> corresponding `internal/workspace/*.go`
- Create: `internal/workspace/workspace.go`
- Create: `internal/workspace/errors.go`
- Rename: `internal/workspace/register.go` -> `internal/workspace/module.go`
- Modify: `internal/tools/workspace.go`
- Modify: `internal/config/register.go`
- Modify: `internal/register.go`
- Modify: workspace and tool tests that currently use `internal/config.WorkDir`
- Delete: emptied `internal/context/`

**Interfaces:**
- Consumes: `agent.RunRequest`, `agent.RunContext`, `agent.ContextFactory`, and `ai` message/tool types.
- Produces: private `workspace.WorkDir`, `workspace.ErrInvalid`, `workspace.RunContextFactory`, and `workspace.Module`.

- [ ] **Step 1: Move the workspace tests first**

Move every context test to `internal/workspace`, change `package context` to `package workspace`, and add this assertion:

```go
func TestRunContextFactoryImplementsAgentContract(t *testing.T) {
	var _ agent.ContextFactory = NewRunContextFactory(
		NewPromptComposer(t.TempDir()), NewSkillLoader(t.TempDir()),
	)
}
```

- [ ] **Step 2: Verify the target workspace package is missing**

Run: `go test ./internal/workspace`

Expected: FAIL because the directory does not exist.

- [ ] **Step 3: Move prompt, AGENTS, Skill, and context assembly files**

Change imports and return types to `agent.RunContext`; keep context ordering exactly: generated System message, external Context sorted by descending Priority, History, then Input. Keep AGENTS and Skills rediscovery inside each `Create` call.

- [ ] **Step 4: Add workspace-owned WorkDir and error classification**

```go
package workspace

import "errors"

type WorkDir string
var ErrInvalid = errors.New("agent workspace invalid")
```

Wrap missing/empty/invalid AGENTS, no eligible Skills, Skill discovery failures, and invalid workspace paths with `ErrInvalid`, preserving the concrete cause. `internal/tools.NewWorkspace` must also wrap WorkDir resolution/open failures with `workspace.ErrInvalid`, so root bootstrap can classify them without parsing error strings.

- [ ] **Step 5: Make default tools consume the workspace WorkDir type**

Change `internal/tools.NewWorkspace` to accept `workspace.WorkDir`, and change all Fx supplies/tests from `config.WorkDir(path)` to `workspace.WorkDir(path)`. Do not alter `os.OpenRoot`, symlink guards, lifecycle hooks, or process supervision.

- [ ] **Step 6: Define the private workspace module**

```go
var Module = fx.Options(fx.Provide(
	newPromptComposer,
	newSkillLoader,
	fx.Annotate(NewRunContextFactory, fx.As(new(agent.ContextFactory))),
))
```

Construct both prompt and Skill loaders from the same `workspace.WorkDir`.

Update the transitional process provider to return `workspace.WorkDir`, and replace `ctxpkg.Register` with `workspace.Module` in `internal.Register`. These changes only keep the pre-root CLI graph buildable until Task 9.

- [ ] **Step 7: Verify workspace behavior and package removal**

Run:

```bash
go test ./internal/workspace ./internal/tools ./agent
test ! -d internal/context
```

Expected: PASS, including AGENTS/Skill validation, prompt budgets, context ordering, cancellation, and caller immutability.

- [ ] **Step 8: Commit workspace ownership**

```bash
git add -A -- internal/workspace internal/context internal/tools internal/config/register.go internal/register.go agent tests
git commit -m "refactor: isolate workspace product policy"
```

### Task 7: Publish Configor configuration and stable error enums

**Files:**
- Move: `internal/config/config.go` -> `config.go`
- Move: `internal/config/validate.go` -> `config_validate.go`
- Move: `internal/config/config_test.go` -> `config_test.go`
- Create: `error_code.go`
- Create: `error.go`
- Create: `error_test.go`
- Create: `types.go`
- Modify temporarily: `internal/config/register.go` for Task 9 relocation
- Modify: current `internal/app`, `internal/conversation`, `internal/driver/mysql`, `internal/dispatch`, `internal/engine` consumers of config types
- Modify: `internal/register.go`

**Interfaces:**
- Consumes: Configor, all existing config field tags/defaults/validation, and `ai.PlatformConfig`/`ai.Protocol`.
- Produces: `reagent.Config`, nested CLI configuration types, `LoadConfig`, `Current`, root aliases/helpers, `ErrorCode`, `Error`, `ErrClosed`, and `ErrorCodeOf`.

- [ ] **Step 1: Add failing external config and error tests**

```go
package reagent_test

func TestLoadConfigUsesConfigorAndStableErrors(t *testing.T) {
	path := writeConfig(t, `{"currentPlatform":"missing","platforms":[]}`)
	_, err := reagent.LoadConfig(path)
	if reagent.ErrorCodeOf(err) != reagent.ErrorCodeConfigInvalid {
		t.Fatalf("code = %q, error = %v", reagent.ErrorCodeOf(err), err)
	}
	var sdkErr *reagent.Error
	if !errors.As(err, &sdkErr) { t.Fatalf("error type = %T", err) }
}

func TestMessageHelpers(t *testing.T) {
	message := reagent.UserMessage("hello")
	if message.Role != reagent.RoleUser || message.Content[0] != reagent.TextBlock("hello") {
		t.Fatalf("message = %#v", message)
	}
}
```

- [ ] **Step 2: Verify root public symbols are missing**

Run: `go test .`

Expected: FAIL with undefined `LoadConfig`, `ErrorCodeOf`, or `UserMessage`.

- [ ] **Step 3: Move Config and use the AI protocol enum**

Define:

```go
type PlatformConfig = ai.PlatformConfig
type Protocol = ai.Protocol
const (
	ProtocolOpenAI = ai.ProtocolOpenAI
	ProtocolAnthropic = ai.ProtocolAnthropic
)
```

`Config.Platforms` remains `[]PlatformConfig`, and `Current() (PlatformConfig, error)` remains source-compatible for normal callers. Replace string comparisons with the enum constants; retain all Configor tags and normalization.

Keep `DefaultHistoryMessageLimit = 100` and every existing nested `BotConfig`, `WeComConfig`, `ConversationConfig`, and `MySQLConfig` field/tag unchanged.

- [ ] **Step 4: Expose only the Configor-backed loader**

```go
func LoadConfig(path string) (*Config, error) {
	var config Config
	if err := configor.Load(&config, path); err != nil {
		return nil, wrap(ErrorCodeConfigLoad, "LoadConfig", fmt.Errorf("加载配置 %s 失败: %w", path, err))
	}
	if err := config.normalizeAndValidate(); err != nil {
		return nil, wrap(ErrorCodeConfigInvalid, "LoadConfig", fmt.Errorf("加载配置 %s 失败: %w", path, err))
	}
	return &config, nil
}
```

Do not add byte decoding, manual environment mapping, or a `CONFIG_PATH` fallback to the root package.

- [ ] **Step 5: Implement the stable string enum exactly**

```go
type ErrorCode string
const (
	ErrorCodeUnknown ErrorCode = "unknown"
	ErrorCodeConfigLoad ErrorCode = "config_load_failed"
	ErrorCodeConfigInvalid ErrorCode = "config_invalid"
	ErrorCodeInitialization ErrorCode = "initialization_failed"
	ErrorCodeRequestInvalid ErrorCode = "request_invalid"
	ErrorCodeWorkspaceInvalid ErrorCode = "workspace_invalid"
	ErrorCodeAIGeneration ErrorCode = "ai_generation_failed"
	ErrorCodeToolRuntime ErrorCode = "tool_runtime_failed"
	ErrorCodeCanceled ErrorCode = "canceled"
	ErrorCodeDeadlineExceeded ErrorCode = "deadline_exceeded"
	ErrorCodeClosed ErrorCode = "agent_closed"
	ErrorCodeInternal ErrorCode = "internal"
)
```

- [ ] **Step 6: Implement wrapped errors and classification lookup**

```go
var ErrClosed = errors.New("reagent: agent closed")

type Error struct { Code ErrorCode; Op string; Err error }
func (e *Error) Error() string { return fmt.Sprintf("reagent %s [%s]: %v", e.Op, e.Code, e.Err) }
func (e *Error) Unwrap() error { return e.Err }

func ErrorCodeOf(err error) ErrorCode {
	if err == nil { return ErrorCodeUnknown }
	var sdkErr *Error
	if errors.As(err, &sdkErr) { return sdkErr.Code }
	switch {
	case errors.Is(err, context.Canceled): return ErrorCodeCanceled
	case errors.Is(err, context.DeadlineExceeded): return ErrorCodeDeadlineExceeded
	case errors.Is(err, ErrClosed): return ErrorCodeClosed
	default: return ErrorCodeUnknown
	}
}

func wrap(code ErrorCode, op string, err error) error {
	if err == nil { return nil }
	var sdkErr *Error
	if errors.As(err, &sdkErr) { return err }
	return &Error{Code: code, Op: op, Err: err}
}

func classify(op string, err error) error {
	if err == nil { return nil }
	switch {
	case errors.Is(err, context.Canceled): return wrap(ErrorCodeCanceled, op, err)
	case errors.Is(err, context.DeadlineExceeded): return wrap(ErrorCodeDeadlineExceeded, op, err)
	case errors.Is(err, ErrClosed): return wrap(ErrorCodeClosed, op, err)
	case errors.Is(err, agent.ErrRequestInvalid): return wrap(ErrorCodeRequestInvalid, op, err)
	case errors.Is(err, workspace.ErrInvalid): return wrap(ErrorCodeWorkspaceInvalid, op, err)
	case errors.Is(err, ai.ErrGeneration): return wrap(ErrorCodeAIGeneration, op, err)
	case errors.Is(err, agent.ErrToolRuntime): return wrap(ErrorCodeToolRuntime, op, err)
	default: return wrap(ErrorCodeInternal, op, err)
	}
}
```

- [ ] **Step 7: Add root type aliases and message helpers**

```go
type Role = ai.Role
type Message = ai.Message
type ContentBlock = ai.ContentBlock
type ToolCall = ai.ToolCall
type RunRequest = agent.RunRequest
type RunResult = agent.RunResult
type ContextBlock = agent.ContextBlock

func TextBlock(text string) ContentBlock { return ai.TextBlock(text) }
func UserMessage(text string) Message { return Message{Role: RoleUser, Content: []ContentBlock{TextBlock(text)}} }
func SystemMessage(text string) Message { return Message{Role: RoleSystem, Content: []ContentBlock{TextBlock(text)}} }
```

Alias role constants too; do not alias Client, Tool, Registry, Reporter, or Store.

- [ ] **Step 8: Verify every existing configuration format**

First rewrite the transitional `internal/config/register.go` to import the root package and provide `*reagent.Config`, `ai.PlatformConfig`, `workspace.WorkDir`, and its existing `Prompt`:

```go
func NewConfig() (*reagent.Config, error) { return reagent.LoadConfig(configurationPath()) }
func NewPlatform(config *reagent.Config) (ai.PlatformConfig, error) { return config.Current() }
```

This file moves unchanged in responsibility to `internal/cli/config.go` in Task 9.

Update every current CLI/product consumer to use `*reagent.Config`, `reagent.ConversationConfig`, and `reagent.MySQLConfig`; keep importing `internal/config` only where the transitional `Prompt` provider is required. Because the root package does not import CLI adapters, this direction introduces no cycle.

Run: `go test . -run 'TestLoadConfig|TestConfig|TestMessageHelpers'`

Expected: PASS for JSON, YAML, TOML, Configor environment overlays, defaults, URL normalization, current-platform selection, duplicate IDs, conversation/MySQL validation, and stable error values.

- [ ] **Step 9: Commit public configuration and errors**

```bash
git add -A -- config.go config_validate.go config_test.go error_code.go error.go error_test.go types.go internal/config internal/register.go
git commit -m "feat: expose public sdk configuration and errors"
```

### Task 8: Build the shared private Fx graph and root Agent facade

**Files:**
- Create: `internal/bootstrap/module.go`
- Create: `internal/bootstrap/module_test.go`
- Modify: `internal/tools/register.go` -> `internal/tools/module.go`
- Create: `bootstrap.go`
- Create: `reagent.go`
- Create: `reagent_test.go`
- Modify: `internal/register.go`

**Interfaces:**
- Consumes: one validated `ai.PlatformConfig`, `workspace.WorkDir`, `providers.New`, `workspace.Module`, concrete `tools.Module`, and public `agent` constructors.
- Produces: private `bootstrap.Module`; public `New(*Config)`, concurrency-safe `Agent.Run`, and idempotent `Agent.Close`.

- [ ] **Step 1: Add a failing external root lifecycle test**

```go
func TestNewRejectsNilConfigAndCloseIsIdempotent(t *testing.T) {
	_, err := reagent.New(nil)
	if reagent.ErrorCodeOf(err) != reagent.ErrorCodeConfigInvalid { t.Fatalf("error = %v", err) }

	agent := newHTTPBackedAgent(t)
	if err := agent.Close(context.Background()); err != nil { t.Fatal(err) }
	if err := agent.Close(context.Background()); err != nil { t.Fatal(err) }
	_, err = agent.Run(context.Background(), reagent.RunRequest{Input: reagent.UserMessage("after close")})
	if !errors.Is(err, reagent.ErrClosed) || reagent.ErrorCodeOf(err) != reagent.ErrorCodeClosed {
		t.Fatalf("Run after Close error = %v", err)
	}
}
```

`newHTTPBackedAgent` creates a temp workspace with valid `AGENTS.md` and one valid Skill, starts an httptest OpenAI-compatible server, writes config with that BaseURL, calls `LoadConfig`, changes into the temp workspace only for `New`, restores the original directory immediately, and registers `Agent.Close` with `t.Cleanup`.

- [ ] **Step 2: Verify root construction is absent**

Run: `go test . -run TestNewRejectsNilConfigAndCloseIsIdempotent`

Expected: FAIL with undefined `New` or `Agent`.

- [ ] **Step 3: Define the default-tools module**

Rename `Register` to `Module`, keep all six `fx.Annotate(..., fx.As(new(agent.Tool)), fx.ResultTags("group:\"agent_tools\""))` providers, and remove registry/middleware construction from this module. Keep Workspace and ProcessSupervisor lifecycle providers.

- [ ] **Step 4: Define the shared bootstrap module**

```go
type registryParams struct {
	fx.In
	Tools []agent.Tool `group:"agent_tools"`
}

func newClient(config ai.PlatformConfig) (ai.Client, error) { return providers.New(config) }
func newRegistry(params registryParams) (agent.Registry, error) {
	return agent.NewRegistry(agent.RegistryOptions{
		Tools: params.Tools,
		Middlewares: agent.DefaultMiddlewareRegistrations(),
	})
}
func newScheduler(registry agent.Registry) *agent.Scheduler { return agent.NewScheduler(registry, 4) }
func newLoop(client ai.Client, scheduler *agent.Scheduler) *agent.Loop {
	return agent.NewLoop(client, scheduler, true)
}

var Module = fx.Options(
	workspace.Module,
	tools.Module,
	fx.Provide(
		newClient,
		newRegistry,
		newScheduler,
		newLoop,
		fx.Annotate(agent.New, fx.As(fx.Self()), fx.As(new(agent.Runner))),
	),
)
```

- [ ] **Step 5: Defensively clone configuration before bootstrap**

Implement `cloneConfig` by value-copying Config, cloning `Platforms`, then calling `normalizeAndValidate` on the clone. Supply only its selected `ai.PlatformConfig` to Fx. Caller mutation after `New` must not alter the selected client.

```go
func New(input *Config) (*Agent, error) {
	if input == nil {
		return nil, wrap(ErrorCodeConfigInvalid, "New", errors.New("config is required"))
	}
	config := cloneConfig(input)
	if err := config.normalizeAndValidate(); err != nil {
		return nil, wrap(ErrorCodeConfigInvalid, "New", err)
	}
	app, runtime, err := buildAgent(config)
	if err != nil { return nil, classifyInitialization("New", err) }
	return &Agent{app: app, runtime: runtime}, nil
}
```

`classifyInitialization` maps `workspace.ErrInvalid` to `workspace_invalid` and every other Fx construction/start error to `initialization_failed`; it does not map initialization errors through the Run-only AI/tool codes.

```go
func classifyInitialization(op string, err error) error {
	if err == nil { return nil }
	if errors.Is(err, workspace.ErrInvalid) {
		return wrap(ErrorCodeWorkspaceInvalid, op, err)
	}
	return wrap(ErrorCodeInitialization, op, err)
}
```

- [ ] **Step 6: Start the private Fx app in root bootstrap**

```go
func buildAgent(config *Config) (*fx.App, agent.Runner, error) {
	workDir, err := os.Getwd()
	if err != nil { return nil, nil, err }
	platform, err := config.Current()
	if err != nil { return nil, nil, err }
	var runtime *agent.Agent
	app := fx.New(
		fx.NopLogger,
		fx.Supply(platform, workspace.WorkDir(workDir)),
		bootstrap.Module,
		fx.Populate(&runtime),
	)
	if err := app.Err(); err != nil { return nil, nil, err }
	if err := app.Start(context.Background()); err != nil { return nil, nil, err }
	return app, runtime, nil
}
```

On a start failure, call `app.Stop` before returning. Classify config failures as `config_invalid`, workspace construction failures as `workspace_invalid`, and other graph/start failures as `initialization_failed`.

- [ ] **Step 7: Implement concurrency-safe Run admission**

```go
type Agent struct {
	app *fx.App
	runtime agent.Runner
	mu sync.Mutex
	closed bool
	active sync.WaitGroup
	closeOnce sync.Once
	closeErr error
}

func (a *Agent) beginRun() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed { return false }
	a.active.Add(1)
	return true
}
```

`Run` rejects nil contexts as `request_invalid`, calls `beginRun`, defers `active.Done`, invokes `a.runtime.Run(ctx, request, nil)`, and classifies errors in this order: canceled, deadline, closed, `agent.ErrRequestInvalid`, `workspace.ErrInvalid`, `ai.ErrGeneration`, `agent.ErrToolRuntime`, internal. Preserve returned partial messages.

```go
func (a *Agent) Run(ctx context.Context, request RunRequest) (RunResult, error) {
	result := RunResult{RunID: request.RunID}
	if ctx == nil {
		return result, wrap(ErrorCodeRequestInvalid, "Run", errors.New("context is required"))
	}
	if !a.beginRun() {
		return result, wrap(ErrorCodeClosed, "Run", ErrClosed)
	}
	defer a.active.Done()
	result, err := a.runtime.Run(ctx, request, nil)
	return result, classify("Run", err)
}
```

- [ ] **Step 8: Implement idempotent Close**

```go
func (a *Agent) Close(ctx context.Context) error {
	if ctx == nil { return wrap(ErrorCodeRequestInvalid, "Close", errors.New("context is required")) }
	a.closeOnce.Do(func() { a.closeErr = a.close(ctx) })
	return a.closeErr
}

func (a *Agent) close(ctx context.Context) error {
	a.mu.Lock()
	a.closed = true
	a.mu.Unlock()
	done := make(chan struct{})
	go func() { a.active.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done(): return classify("Close", ctx.Err())
	}
	return classify("Close", a.app.Stop(ctx))
}
```

The mutex prevents `Wait` racing with a future positive `Add`; `sync.Once` makes later calls return the first close result.

- [ ] **Step 9: Verify graph, lifecycle, and root API**

Run:

```bash
go test ./internal/bootstrap ./internal/tools .
go test -race .
```

Expected: PASS; Fx creates one client, one immutable registry, one process supervisor, and one reusable Agent.

- [ ] **Step 10: Remove the old composition root and commit**

The bundled command still needs an intermediate graph until Task 9. Rewrite `internal.Register` to compose `bootstrap.Module` with the existing private config, MySQL, conversation, reporter, and app options; remove the temporary AI/runtime/registry constructors now owned by bootstrap. Do not delete `internal/register.go` yet.

```bash
git add -A -- internal/bootstrap internal/tools bootstrap.go reagent.go reagent_test.go internal/register.go
git commit -m "feat: add default reagent sdk facade"
```

### Task 9: Relocate CLI-only lifecycle, conversation, MySQL, and dispatch

**Files:**
- Create: `internal/cli/module.go`
- Move: `internal/config/register.go` -> `internal/cli/config.go`
- Move: `internal/config/register_test.go` -> `internal/cli/config_test.go`
- Move: `internal/app/*` -> `internal/cli/app/*`
- Move: `internal/conversation/*` -> `internal/cli/conversation/*`
- Move: `internal/driver/mysql/*` -> `internal/cli/driver/mysql/*`
- Move: `internal/dispatch/*` -> `internal/cli/dispatch/*`
- Move: `internal/engine/terminal_reporter.go` -> `internal/cli/dispatch/terminal.go`
- Move: `internal/engine/terminal_reporter_test.go` -> `internal/cli/dispatch/terminal_test.go`
- Modify: `cmd/reagent/main.go`
- Modify: `cmd/reagent/main_test.go`
- Delete: emptied old directories
- Delete: `internal/register.go`

**Interfaces:**
- Consumes: root `reagent.Config`/`LoadConfig`, `ai.PlatformConfig`, `agent.Runner`/Reporter/events, `workspace.WorkDir`, `bootstrap.Module`, and the unchanged persistence dependencies.
- Produces: `cli.Module` plus private `app.Module`, `conversation.Module`, `driver/mysql.Module`, and `dispatch.Module` for the bundled command.

- [ ] **Step 1: Move CLI tests to their final paths before implementations**

Preserve all current test bodies and change imports according to this table:

```text
internal/config             -> root reagent for Config types
internal/engine.Reporter    -> agent.Reporter
internal/schema             -> ai and agent
internal/conversation       -> internal/cli/conversation
internal/driver/mysql       -> internal/cli/driver/mysql
internal/dispatch           -> internal/cli/dispatch
```

- [ ] **Step 2: Verify the CLI target packages are absent**

Run: `go test ./internal/cli/... ./cmd/reagent`

Expected: FAIL because `internal/cli` does not exist.

- [ ] **Step 3: Move process configuration without changing Configor behavior**

`internal/cli/config.go` must provide:

```go
type Prompt string

func NewConfig() (*reagent.Config, error) { return reagent.LoadConfig(configurationPath()) }
func NewPlatform(config *reagent.Config) (ai.PlatformConfig, error) { return config.Current() }
func NewWorkDir() (workspace.WorkDir, error) {
	path, err := os.Getwd()
	if err != nil { return "", fmt.Errorf("获取工作区失败: %w", err) }
	return workspace.WorkDir(path), nil
}
```

Keep `CONFIG_PATH` trimming/default `config.json`, `AGENT_PROMPT`, `AGENT_USER_ID`, and `AGENT_CONVERSATION_ID` exactly where the CLI currently reads them.

- [ ] **Step 4: Move the conversation runner and Store unchanged in ownership**

Use `agent.Runner` for runtime calls, `agent.Reporter` for events, `agent.RunRequest` for History/Input/Context/Metadata, and `agent.RunResult` for partial results. Continue loading bounded History before runtime and appending Input plus `NewMessages` afterward; continue `errors.Join(runErr, persistErr)`.

- [ ] **Step 5: Move MySQL driver and Store packages without replacing libraries**

Update only import paths and Config type ownership. Keep `sqlsdk.NewTransProvider`, `transaction.Manager`, `UseDB`, Fx close hooks, GORM models/codecs/windows, optimistic version checks, sqlmock, and migration SQL unchanged. Do not introduce `database/sql` construction or `AutoMigrate`.

- [ ] **Step 6: Move Terminal and WeCom into dispatch**

Terminal and WeCom implement `agent.Reporter`. Preserve terminal output locking, argument truncation, exec-only updates, WeCom 4096-byte UTF-8 truncation, HTTP timeout, and the existing event routing. `dispatch.Module` contributes ordered terminal and optional WeCom registrations and constructs `agent.NewMultiReporter`.

- [ ] **Step 7: Move the app lifecycle and create CLI module**

```go
var Module = fx.Options(
	fx.Provide(NewConfig, NewPlatform, NewWorkDir, NewPrompt),
	drivermysql.Module,
	conversationmysql.Module,
	conversation.Module,
	dispatch.Module,
	app.Module,
)
```

The app runner keeps one-shot start/stop behavior and passes the CLI Reporter to the lower `agent.Runner`; no Reporter enters root `reagent.Agent.Run`.

- [ ] **Step 8: Compose exactly one Fx app in `cmd/reagent`**

```go
fx.New(
	bootstrap.Module,
	cli.Module,
	fx.WithLogger(func() fxevent.Logger { return fxevent.NopLogger }),
).Run()
```

Keep `go-logger-sdk` initialization and logger options unchanged. Do not call `reagent.New` from the CLI, because that would create a nested Fx app.

- [ ] **Step 9: Verify CLI, persistence, and integration behavior**

Run:

```bash
go test ./internal/cli/... ./cmd/reagent ./tests/integration/...
```

Expected: PASS, including conversation history, MySQL initialization/transactions, Fx dependency graph, terminal/WeCom routing, and supervisor lifecycle tests.

- [ ] **Step 10: Remove emptied old packages and commit**

```bash
test ! -d internal/app
test ! -d internal/config
test ! -d internal/context
test ! -d internal/conversation
test ! -d internal/dispatch
test ! -d internal/driver
test ! -d internal/engine
git add -A -- internal/cli internal/app internal/config internal/conversation internal/dispatch internal/driver internal/engine internal/register.go cmd/reagent tests
git commit -m "refactor: isolate bundled cli adapters"
```

### Task 10: Prove root concurrency, cancellation, error, and Close contracts

**Files:**
- Create: `reagent_concurrency_test.go`
- Create: `reagent_error_test.go`
- Create: `reagent_close_test.go`
- Modify: `reagent.go`
- Modify: `error.go`
- Modify: `bootstrap.go`

**Interfaces:**
- Consumes: only the documented root package from external tests, plus an httptest OpenAI-compatible endpoint.
- Produces: evidence that one root Agent is concurrent, stateless, immutable at boundaries, correctly classified, and gracefully closeable.

- [ ] **Step 1: Add a concurrent real-adapter test**

Create an httptest handler that returns `{"content":"plan"}` when request JSON has no `tools`, and returns a final assistant message containing the last user message when `tools` is present. Start one root Agent and execute 16 Runs concurrently:

```go
const runCount = 16
results := make(chan reagent.RunResult, runCount)
errorsCh := make(chan error, runCount)
for index := 0; index < runCount; index++ {
	index := index
	go func() {
		result, err := sdk.Run(context.Background(), reagent.RunRequest{
			RunID: fmt.Sprintf("run-%d", index),
			Input: reagent.UserMessage(fmt.Sprintf("input-%d", index)),
			Metadata: map[string]string{"index": strconv.Itoa(index)},
		})
		results <- result
		errorsCh <- err
	}()
}
```

Assert every RunID appears once, every result has only its own final action message, and no thinking message or other Run's input appears.

- [ ] **Step 2: Verify the concurrent contract under the race detector**

Run: `go test -race . -run TestAgentConcurrentRunsAreIsolated`

Expected before fixes: FAIL or race report if request/result slices, workspace discovery, registry, or root admission state are shared unsafely.

- [ ] **Step 3: Deep-clone all run boundaries**

Ensure `agent.Run` clones before validation/context creation; workspace clones Context/History/Metadata when composing; the loop owns a private `contextHistory`; scheduler allocates a result slot per call; root returns a final deep clone. Do not add a global Run mutex.

- [ ] **Step 4: Add cancellation and deadline tests**

Block the action response server until the request context is canceled. Assert:

```go
if !errors.Is(err, context.Canceled) || reagent.ErrorCodeOf(err) != reagent.ErrorCodeCanceled {
	t.Fatalf("canceled error = %v", err)
}
```

Repeat with `context.WithTimeout` and assert `deadline_exceeded`. Start another Run concurrently and assert canceling the first does not cancel the second.

- [ ] **Step 5: Add official-SDK unwrap and partial-result tests**

Make the server first return an assistant `read` tool call for `AGENTS.md`, allow that tool result to complete, then return an HTTP 500 OpenAI error JSON response on the next action request. Assert `ErrorCodeAIGeneration`, a non-empty ordered partial result, `errors.Is(err, ai.ErrGeneration)`, and `errors.As(err, new(*openai.Error))`. The root error must not map provider-specific status/error codes.

- [ ] **Step 6: Add Close admission and deadline tests**

Block one active Run, begin Close, and assert a new Run immediately returns `ErrClosed`. Release the active Run and assert Close completes and background ProcessSupervisor sessions are empty. Use an already-expired context in a separate Agent and assert Close returns `deadline_exceeded`; a second Close returns the same first error.

- [ ] **Step 7: Implement only the fixes exposed by these tests**

Keep Run admission under `Agent.mu`, do not serialize runtime execution, and preserve the error classification order from Task 8. If Close times out waiting for active Runs, keep the Agent permanently closed and return that first timeout result on subsequent Close calls.

- [ ] **Step 8: Verify all root contracts**

Run:

```bash
go test .
go test -race .
```

Expected: PASS with no races and no real network traffic.

- [ ] **Step 9: Commit contract hardening**

```bash
git add reagent.go bootstrap.go error.go reagent_concurrency_test.go reagent_error_test.go reagent_close_test.go
git commit -m "test: enforce sdk concurrency and lifecycle contracts"
```

### Task 11: Enforce dependency direction and remove compatibility remnants

**Files:**
- Create: `tests/integration/package_boundaries_test.go`
- Modify: remaining tests under `tests/integration/`
- Delete: any emptied legacy directories/files found by the boundary scan

**Interfaces:**
- Consumes: final Go package graph.
- Produces: a standard-library-only test that prevents `ai` and `agent` from importing product/CLI layers and prevents legacy imports from returning.

- [ ] **Step 1: Add the failing package-boundary test**

```go
func TestPublicPackageDependencyBoundaries(t *testing.T) {
	tests := []struct{ pkg string; forbidden []string }{
		{"github.com/PycMono/go-reagent/ai", []string{"/agent", "/internal/", "github.com/PycMono/go-reagent"}},
		{"github.com/PycMono/go-reagent/agent", []string{"/internal/", "github.com/PycMono/go-reagent"}},
	}
	for _, test := range tests {
		imports := goListDependencies(t, test.pkg)
		for _, dependency := range imports {
			for _, forbidden := range test.forbidden {
				if forbidden == "github.com/PycMono/go-reagent" && dependency == forbidden { t.Fatalf("%s imports root", test.pkg) }
				if strings.HasPrefix(forbidden, "/") && strings.Contains(dependency, forbidden) { t.Fatalf("%s imports %s", test.pkg, dependency) }
			}
		}
	}
}
```

`goListDependencies` runs `go list -deps -f={{.ImportPath}} <pkg>`, splits lines, and excludes the package itself. Add explicit checks that no Go file imports `/internal/schema`, `/internal/provider`, `/internal/engine`, or `/internal/context`.

- [ ] **Step 2: Verify the scan catches any remaining old import**

Run:

```bash
go test ./tests/integration -run TestPublicPackageDependencyBoundaries
rg 'go-reagent/internal/(schema|provider|engine|context|app|conversation|driver|dispatch)' --glob '*.go'
```

Expected before cleanup: FAIL or matching paths if any migration remnant remains.

- [ ] **Step 3: Rewrite the remaining integration imports**

Use root `reagent` only for public SDK tests, `ai`/`agent` for foundation tests, and `internal/cli/...` only for bundled-product integration tests. Do not add an import shim.

- [ ] **Step 4: Verify dependency direction and legacy removal**

Run:

```bash
go test ./tests/integration -run TestPublicPackageDependencyBoundaries
test -z "$(rg -l 'go-reagent/internal/(schema|provider|engine|context|app|conversation|driver|dispatch)' --glob '*.go' || true)"
```

Expected: PASS and no legacy import matches.

- [ ] **Step 5: Commit package-boundary enforcement**

```bash
git add -A -- tests/integration/package_boundaries_test.go tests/integration ai agent internal bootstrap.go config.go config_validate.go error.go error_code.go reagent.go types.go
git commit -m "test: enforce public package boundaries"
```

### Task 12: Update public documentation and run full verification

**Files:**
- Modify: `README.md`
- Create: `docs/sdk-architecture.md`
- Modify: `docs/conversation-persistence.md`
- Modify: `config.example.json` only if comments/paths reference old ownership; do not change keys or values
- Modify: `docs/superpowers/specs/2026-08-05-public-sdk-package-design.md` only to record the Go-cycle-safe factory path `ai/providers/factory.go`

**Interfaces:**
- Consumes: final exported API and package graph.
- Produces: copyable SDK usage, caller-owned persistence flow, CLI usage, dependency diagram, lifecycle guidance, and accurate package paths.

- [ ] **Step 1: Add the SDK quick-start example to README**

```go
config, err := reagent.LoadConfig("config.json")
if err != nil { return err }

sdk, err := reagent.New(config)
if err != nil { return err }
defer sdk.Close(context.Background())

result, err := sdk.Run(ctx, reagent.RunRequest{
	RunID: "run-123",
	History: history,
	Input: reagent.UserMessage("Review the current workspace"),
	Metadata: map[string]string{"conversation_id": conversationID},
})
// Persist result.NewMessages in the business transaction, including a
// non-empty partial result when err is non-nil if business policy permits.
```

State explicitly that callers load History and persist `NewMessages`; the root SDK exposes no Store, Reporter, Provider, or Tool injection.

- [ ] **Step 2: Document package direction and ownership**

Add this diagram to `docs/sdk-architecture.md`:

```text
ai <- agent <- reagent
              |-- internal/bootstrap
              |-- internal/workspace
              `-- internal/tools

internal/cli -> reagent/agent + internal/bootstrap
cmd/reagent  -> internal/cli + internal/bootstrap
```

Document synchronous Run, per-run workspace rediscovery, concurrent Agent use, partial results, ErrorCode values, and Close semantics.

- [ ] **Step 3: Update persistence documentation without changing behavior**

Show the CLI-only flow `LoadOrCreate -> agent.Run -> AppendTurn`, retain migration names and environment variables, and state that business SDK callers can implement the same transaction policy outside the SDK.

- [ ] **Step 4: Run formatting and focused API checks**

Run:

```bash
gofmt -w ai agent internal cmd *.go tests
go doc github.com/PycMono/go-reagent
go doc github.com/PycMono/go-reagent/ai
go doc github.com/PycMono/go-reagent/agent
```

Expected: docs show only the intended root API; root docs do not list Reporter, Tool, Client, Registry, Store, or Fx options.

- [ ] **Step 5: Run the complete test suite**

Run:

```bash
go test ./...
go test -race ./...
go vet ./...
git diff --check
```

Expected: all commands exit 0. MySQL integration tests retain their existing environment-gated behavior.

- [ ] **Step 6: Verify dependencies and old package deletion**

Run:

```bash
go mod tidy
git diff -- go.mod go.sum
for path in internal/schema internal/provider internal/engine internal/context internal/app internal/config internal/conversation internal/driver internal/dispatch; do test ! -e "$path"; done
rg 'configor|openai-go/v3|anthropic-sdk-go|go.uber.org/fx|go-logger-sdk|jsonschema/v6|yaml.v3|go-mysql-sdk|gorm.io/gorm' go.mod
```

Expected: no replacement dependency appears, every required dependency remains, and all old package paths are absent.

- [ ] **Step 7: Review the final diff for compatibility shims and unrelated changes**

Run:

```bash
git diff --stat "$(git merge-base HEAD master)"..HEAD
rg 'Deprecated:|type .* = .*internal/|func .*internal/' --glob '*.go'
git status --short
```

Expected: no compatibility shims, no unrelated files, and only documentation changes remain unstaged before the final commit.

- [ ] **Step 8: Commit documentation and dependency cleanup**

```bash
git add -A -- README.md docs/sdk-architecture.md docs/conversation-persistence.md docs/superpowers/specs/2026-08-05-public-sdk-package-design.md config.example.json go.mod go.sum
git commit -m "docs: document public reagent sdk"
```

- [ ] **Step 9: Perform the final clean-tree verification**

Run:

```bash
go test ./...
go test -race ./...
go vet ./...
git diff --check
git status --short
```

Expected: all verification commands exit 0 and `git status --short` prints nothing.
