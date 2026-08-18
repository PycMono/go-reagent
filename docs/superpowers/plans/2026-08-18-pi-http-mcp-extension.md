# Pi HTTP MCP Extension and Exa Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 `pi` 增加 Go 编译期扩展运行时和通用 Streamable HTTP MCP 客户端，并在 Web 应用中通过 Exa Hosted MCP 暴露 `web_search_exa` 与 `web_fetch_exa`。

**Architecture:** 现有静态 `agent_tools` 与扩展启动时发现的工具共同进入一个启动阶段可写、启动后冻结的注册表；Extension Runtime 通过 Fx 管理注册、失败回滚和逆序关闭。`pi/mcp` 使用标准库实现 MCP JSON-RPC、JSON/SSE Streamable HTTP 和远端工具代理，`application/web` 只负责把业务配置转换成通用 MCP 扩展。

**Tech Stack:** Go 1.26、标准库 `net/http`/`encoding/json`/`bufio`、`go.uber.org/fx`、现有 `jsonschema/v6`、`httptest`。

**Spec:** `docs/superpowers/specs/2026-08-18-pi-http-mcp-extension-design.md`

## Global Constraints

- Exa 必须在本期形成真实端到端闭环，不能只交付扩展接口。
- `pi` 根包不得导入 `pi/mcp`；组合只能发生在 `application/web`。
- 不增加第三方依赖；MCP HTTP、SSE 和 JSON-RPC 使用 Go 标准库实现。
- 保持 `ai.Tool`、Agent、Loop、Scheduler、现有 `agent_tools` 和 `NewToolRuntime(ToolRuntimeOptions)` 的公共行为。
- 未配置或未启用 MCP 时，现有 CLI、Web 和测试行为不变。
- 启用的 MCP Server 一期必须 `required: true`，发现失败时 Fx fail-fast。
- MCP 工具默认 `ParallelSafe: false`，运行期间不热增删工具。
- MCP protocol version 固定为 `2025-03-26`，单个 HTTP 响应上限为 16 MiB。
- 认证 Header、API Key、完整请求参数和完整远端内容不得进入日志或错误。
- 普通测试不得访问公网；真实 Exa 测试必须显式 opt-in。
- 工作区已有未提交修改属于用户；每次提交只暂存当前任务列出的文件。
- 实施开始前必须通过 `superpowers:using-git-worktrees` 创建隔离 worktree；当前主工作区已有与 `application/web/register_test.go` 等目标文件重叠的用户修改，不得在原工作区直接编辑或暂存。

## File Map

```text
pi/
├── extension.go
├── extension_runtime.go
├── extension_test.go
├── extension_runtime_test.go
├── tool_registry.go
├── tool_registry_test.go
├── tool_runtime.go
├── register.go
├── register_test.go
└── mcp/
    ├── protocol.go
    ├── errors.go
    ├── transport_http.go
    ├── transport_http_test.go
    ├── client.go
    ├── client_test.go
    ├── tool.go
    ├── tool_test.go
    ├── extension.go
    ├── extension_test.go
    └── exa_integration_test.go

config/{config.go,validate.go,config_test.go}
application/web/{mcp.go,mcp_test.go,register.go,register_test.go}
pi/test/package_boundaries_test.go
config.example.json
```

---

### Task 1: Introduce the startup-mutable Tool Registry

**Files:**
- Create: `pi/tool_registry.go`
- Create: `pi/tool_registry_test.go`
- Modify: `pi/tool_runtime.go`
- Test: `pi/test/tool_runtime_test.go`
- Test: `pi/test/tool_runtime_public_test.go`
- Test: `pi/test/schema_validator_test.go`

**Interfaces:**
- Consumes: `ai.Tool`, `ai.ToolDefinition`, existing `compileSchemaValidator` and middleware composition.
- Produces: `newToolRegistry([]ai.Tool) (*toolRegistry, error)`, `register(owner string, tool ai.Tool) error`, `rollback(owner string)`, `freeze()`, `definitions()`, `lookup(name string)` and `newToolRuntimeFromRegistry`.
- Preserves: `NewToolRuntime(ToolRuntimeOptions) (ToolRuntime, error)`.

- [ ] **Step 1: Write failing registry tests**

Add package-internal tests for static registration, stable sorting, owner rollback, duplicate rejection, typed-nil rejection and freeze behavior:

```go
func TestToolRegistryRollsBackOneOwnerAndFreezes(t *testing.T) {
    registry, err := newToolRegistry([]ai.Tool{registryTestTool("read")})
    if err != nil {
        t.Fatal(err)
    }
    if err := registry.register("mcp:exa", registryTestTool("web_search_exa")); err != nil {
        t.Fatal(err)
    }
    registry.rollback("mcp:exa")
    if got := registry.definitions(); len(got) != 1 || got[0].Name != "read" {
        t.Fatalf("definitions = %#v", got)
    }
    registry.freeze()
    if err := registry.register("mcp:exa", registryTestTool("web_fetch_exa")); err == nil {
        t.Fatal("register after freeze succeeded")
    }
}

func TestToolRegistryRejectsDuplicateAcrossOwners(t *testing.T) {
    registry, err := newToolRegistry([]ai.Tool{registryTestTool("read")})
    if err != nil {
        t.Fatal(err)
    }
    if err := registry.register("mcp:exa", registryTestTool("read")); err == nil || !strings.Contains(err.Error(), `tool "read"`) {
        t.Fatalf("duplicate error = %v", err)
    }
}
```

Hold a nil `*registryTestTool` in an `ai.Tool` interface and assert construction rejects it before calling `Definition`.

- [ ] **Step 2: Run the focused tests and verify failure**

Run: `go test ./pi -run 'TestToolRegistry' -count=1`

Expected: FAIL because the registry does not exist.

- [ ] **Step 3: Implement the minimal registry**

```go
const staticToolOwner = "pi:static"

type toolEntry struct {
    definition   ai.ToolDefinition
    tool         ai.Tool
    validateArgs func(json.RawMessage) error
    owner        string
}

type toolRegistry struct {
    mu     sync.RWMutex
    tools  map[string]toolEntry
    frozen bool
}
```

Validate and compile schema before the final locked duplicate/frozen check. `rollback` removes only the exact owner. Copy definitions under read lock, sort after releasing it, and never hold a lock while executing a tool.

- [ ] **Step 4: Refactor ToolRuntime without public behavior changes**

```go
type toolRuntime struct {
    registry *toolRegistry
    handler  Handler
}

func NewToolRuntime(options ToolRuntimeOptions) (ToolRuntime, error) {
    registry, err := newToolRegistry(options.Tools)
    if err != nil {
        return nil, err
    }
    registry.freeze()
    return newToolRuntimeFromRegistry(registry, options.Middlewares), nil
}

func newToolRuntimeFromRegistry(registry *toolRegistry, middlewares []MiddlewareRegistration) ToolRuntime {
    return &toolRuntime{registry: registry, handler: composeHandler(middlewares)}
}
```

Keep event order, validation, middleware, context propagation and result normalization unchanged.

- [ ] **Step 5: Run registry and existing ToolRuntime tests**

```bash
go test ./pi ./pi/test -run 'TestToolRegistry|TestToolRuntime' -count=1
go test -race ./pi ./pi/test -run 'TestToolRegistry|TestToolRuntime' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit Task 1**

```bash
git add pi/tool_registry.go pi/tool_registry_test.go pi/tool_runtime.go pi/test/tool_runtime_test.go pi/test/tool_runtime_public_test.go pi/test/schema_validator_test.go
git commit -m "refactor(pi): add startup tool registry"
```

### Task 2: Add the compile-time Extension Runtime and Fx wiring

**Files:**
- Create: `pi/extension.go`
- Create: `pi/extension_test.go`
- Create: `pi/extension_runtime.go`
- Create: `pi/extension_runtime_test.go`
- Modify: `pi/register.go`
- Modify: `pi/register_test.go`

**Interfaces:**
- Consumes: Task 1 `toolRegistry` and Fx `agent_tools` group.
- Produces: public `Extension`, `ExtensionAPI`, `ExtensionCloser`; internal `extensionRuntime`; Fx group `agent_extensions`.

- [ ] **Step 1: Write failing contract and lifecycle tests**

Cover sorted startup, duplicate/blank/typed-nil extensions, failure cleanup including the current extension, reverse shutdown and freeze:

```go
func TestExtensionRuntimeStartsSortedAndStopsReversed(t *testing.T) {
    var events []string
    registry, err := newToolRegistry(nil)
    if err != nil {
        t.Fatal(err)
    }
    lifecycle := fxtest.NewLifecycle(t)
    _, err = newExtensionRuntime(extensionRuntimeParams{
        Lifecycle: lifecycle,
        Registry: registry,
        Extensions: []Extension{
            newExtensionFake("zeta", &events),
            newExtensionFake("alpha", &events),
        },
    })
    if err != nil {
        t.Fatal(err)
    }
    lifecycle.RequireStart()
    lifecycle.RequireStop()
    want := []string{"start:alpha", "start:zeta", "stop:zeta", "stop:alpha"}
    if !slices.Equal(events, want) {
        t.Fatalf("events = %v, want %v", events, want)
    }
}
```

In the failure test, make `zeta` register a tool then return `errors.New("discover failed")`; assert owner rollback and closers for `zeta` and earlier successful extensions.

- [ ] **Step 2: Run focused tests and verify failure**

Run: `go test ./pi -run 'TestExtension' -count=1`

Expected: FAIL because extension contracts do not exist.

- [ ] **Step 3: Add the public contracts**

```go
type Extension interface {
    Name() string
    Register(context.Context, ExtensionAPI) error
}

type ExtensionAPI interface {
    RegisterTool(ai.Tool) error
}

type ExtensionCloser interface {
    Close(context.Context) error
}
```

The concrete API binds one normalized owner to `toolRegistry.register`; do not expose Fx or the registry.

- [ ] **Step 4: Implement lifecycle, rollback and error joining**

```go
type extensionRuntimeParams struct {
    fx.In
    Lifecycle  fx.Lifecycle
    Registry   *toolRegistry
    Extensions []Extension `group:"agent_extensions"`
}

type extensionRuntime struct {
    registry   *toolRegistry
    extensions []Extension
    started    []Extension
}
```

Validate and sort during construction. OnStart registers sequentially; failure rolls back the current owner, closes current and prior extensions in reverse, clears `started`, and returns an extension-scoped error. On success freeze the registry. OnStop attempts every closer in reverse and combines errors with `errors.Join`.

- [ ] **Step 5: Wire CoreRegister before downstream consumers**

```go
type toolRegistryParams struct {
    fx.In
    Tools []ai.Tool `group:"agent_tools"`
}

func newFXToolRegistry(params toolRegistryParams) (*toolRegistry, error) {
    return newToolRegistry(params.Tools)
}

func newFXToolRuntime(registry *toolRegistry, _ *extensionRuntime) ToolRuntime {
    return newToolRuntimeFromRegistry(registry, DefaultMiddlewareRegistrations())
}
```

Provide `newFXToolRegistry`, `newExtensionRuntime`, then `newFXToolRuntime`. The explicit runtime dependency makes the extension hook precede Web server hooks. Update register tests and add an Fx-grouped fake extension whose tool appears after `RequireStart`.

- [ ] **Step 6: Run extension and Fx tests**

```bash
go test ./pi -run 'TestExtension|TestCoreRegister|Test.*ToolsRegister' -count=1
go test -race ./pi -run 'TestExtension' -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit Task 2**

```bash
git add pi/extension.go pi/extension_test.go pi/extension_runtime.go pi/extension_runtime_test.go pi/register.go pi/register_test.go
git commit -m "feat(pi): add compile-time extension runtime"
```

### Task 3: Implement MCP JSON-RPC and Streamable HTTP transport

**Files:**
- Create: `pi/mcp/protocol.go`
- Create: `pi/mcp/errors.go`
- Create: `pi/mcp/transport_http.go`
- Create: `pi/mcp/transport_http_test.go`

**Interfaces:**
- Consumes: Go standard library only.
- Produces: `Request`, `Response`, `RPCError`, `HTTPTransportOptions`, `NewHTTPTransport`, `Send`, `Close`.
- Security: 16 MiB response cap, loopback-only plaintext HTTP, no cross-host redirects, no secret/body echo in errors.

- [ ] **Step 1: Write failing JSON, SSE and session tests**

```go
func TestHTTPTransportHandlesJSONAndSession(t *testing.T) {
    var calls atomic.Int32
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        call := calls.Add(1)
        w.Header().Set("Content-Type", "application/json")
        if call == 1 {
            w.Header().Set("Mcp-Session-Id", "session-1")
        } else if got := r.Header.Get("Mcp-Session-Id"); got != "session-1" {
            t.Errorf("session header = %q", got)
        }
        _, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`)
    }))
    defer server.Close()

    transport, err := NewHTTPTransport(HTTPTransportOptions{Endpoint: server.URL, Timeout: time.Second})
    if err != nil {
        t.Fatal(err)
    }
    id := int64(1)
    request := Request{JSONRPC: "2.0", ID: &id, Method: "initialize"}
    if _, err := transport.Send(context.Background(), request); err != nil {
        t.Fatal(err)
    }
    request.Method = "tools/list"
    if _, err := transport.Send(context.Background(), request); err != nil {
        t.Fatal(err)
    }
}
```

Add table cases for JSON, SSE comments/multiple `data:` lines/multiple events, 202/204 notification, HTTP status, wrong Content-Type, malformed payload, mismatched id, >16 MiB, timeout and caller cancellation.

- [ ] **Step 2: Write failing security tests**

Reject public plaintext HTTP, endpoint userinfo/query/fragment, controlled headers (`Host`, `Content-Length`, `Mcp-Session-Id`) and cross-host redirects. Put `never-print-mcp-secret` in a Header and body; assert every error omits it.

- [ ] **Step 3: Run tests and verify failure**

Run: `go test ./pi/mcp -run 'TestHTTPTransport' -count=1`

Expected: FAIL because `pi/mcp` does not exist.

- [ ] **Step 4: Define wire and error types**

```go
const ProtocolVersion = "2025-03-26"

type Request struct {
    JSONRPC string `json:"jsonrpc"`
    ID      *int64 `json:"id,omitempty"`
    Method  string `json:"method"`
    Params  any    `json:"params,omitempty"`
}

type Response struct {
    JSONRPC string          `json:"jsonrpc"`
    ID      *int64          `json:"id,omitempty"`
    Result  json.RawMessage `json:"result,omitempty"`
    Error   *RPCError       `json:"error,omitempty"`
}

type RPCError struct {
    Code    int             `json:"code"`
    Message string          `json:"message"`
    Data    json.RawMessage `json:"data,omitempty"`
}
```

Typed errors may include operation, endpoint host, HTTP status/RPC code and bounded message, never request params, headers, error data or response body.

- [ ] **Step 5: Implement HTTPTransport**

```go
const maxHTTPResponseBytes int64 = 16 << 20

type HTTPTransportOptions struct {
    Endpoint   string
    Headers    http.Header
    Timeout    time.Duration
    HTTPClient *http.Client
}

func NewHTTPTransport(HTTPTransportOptions) (*HTTPTransport, error)
func (t *HTTPTransport) Send(context.Context, Request) (Response, error)
func (t *HTTPTransport) Close(context.Context) error
```

Apply `context.WithTimeout` without replacing an earlier deadline. Read `maxHTTPResponseBytes+1` and reject overflow. Parse SSE line-by-line, set the scanner buffer limit to `int(maxHTTPResponseBytes)`, join consecutive `data:` lines with newline, decode completed events and select the matching id. Notifications accept empty 202/204. Copy client/Header inputs, reject cross-host redirects, synchronize Session ID, and always close idle connections even if DELETE fails.

- [ ] **Step 6: Run transport tests including race**

```bash
go test ./pi/mcp -run 'TestHTTPTransport' -count=1
go test -race ./pi/mcp -run 'TestHTTPTransport' -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit Task 3**

```bash
git add pi/mcp/protocol.go pi/mcp/errors.go pi/mcp/transport_http.go pi/mcp/transport_http_test.go
git commit -m "feat(mcp): add streamable HTTP transport"
```

### Task 4: Implement MCP client handshake, discovery and calls

**Files:**
- Modify: `pi/mcp/protocol.go`
- Create: `pi/mcp/client.go`
- Create: `pi/mcp/client_test.go`

**Interfaces:**
- Consumes: Task 3 `Send`/`Close` and protocol version.
- Produces: `Client`, `NewClient`, `Initialize`, `ListTools`, `CallTool`, `Close`, plus MCP Tool/Content result types.

- [ ] **Step 1: Write failing sequence and pagination tests**

Use a fake transport and assert the exact method order:

```go
wantMethods := []string{
    "initialize",
    "notifications/initialized",
    "tools/list",
    "tools/list",
    "tools/call",
}
```

The first list response returns `nextCursor:"page-2"`; assert the second request contains it and tools/call receives the original JSON object. Add protocol mismatch, pre-initialize use, duplicate tools, cursor cycle, malformed result, RPC error, `isError`, cancellation and Close tests.

- [ ] **Step 2: Run client tests and verify failure**

Run: `go test ./pi/mcp -run 'TestClient' -count=1`

Expected: FAIL because Client is undefined.

- [ ] **Step 3: Add MCP result types**

```go
type Tool struct {
    Name        string         `json:"name"`
    Title       string         `json:"title,omitempty"`
    Description string         `json:"description,omitempty"`
    InputSchema map[string]any `json:"inputSchema"`
}

type Content struct {
    Type string `json:"type"`
    Text string `json:"text,omitempty"`
}

type CallToolResult struct {
    Content           []Content `json:"content"`
    StructuredContent any       `json:"structuredContent,omitempty"`
    IsError           bool      `json:"isError,omitempty"`
}
```

Add initialize/list/call params/results with exact MCP camelCase tags.

- [ ] **Step 4: Implement the client state machine**

```go
type Transport interface {
    Send(context.Context, Request) (Response, error)
    Close(context.Context) error
}

type Client struct {
    transport Transport
    nextID    atomic.Int64
    stateMu   sync.RWMutex
    initialized bool
    closed      bool
    name      string
    version   string
}

func NewClient(transport Transport, name, version string) (*Client, error)
func (c *Client) Initialize(context.Context) error
func (c *Client) ListTools(context.Context) ([]Tool, error)
func (c *Client) CallTool(context.Context, string, json.RawMessage) (CallToolResult, error)
func (c *Client) Close(context.Context) error
```

Initialize sends the exact `name` and `version` constructor arguments with empty capabilities, validates exact negotiated version, then sends the notification. Hold the write lock across initialization so concurrent callers cannot handshake twice; List/Call check initialized and closed state under a read lock. List follows unique cursors and rejects duplicate/blank tools. Call decodes arguments as one JSON object using `UseNumber`, rejects trailing input and never retries. Close marks the client closed only after delegating transport cleanup and is idempotent after a successful close.

- [ ] **Step 5: Run client tests**

```bash
go test ./pi/mcp -run 'TestClient' -count=1
go test -race ./pi/mcp -run 'TestClient' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit Task 4**

```bash
git add pi/mcp/protocol.go pi/mcp/client.go pi/mcp/client_test.go
git commit -m "feat(mcp): add protocol client"
```

### Task 5: Adapt MCP tools and implement one-server Extension

**Files:**
- Create: `pi/mcp/tool.go`
- Create: `pi/mcp/tool_test.go`
- Create: `pi/mcp/extension.go`
- Create: `pi/mcp/extension_test.go`

**Interfaces:**
- Consumes: `pi.Extension`, `pi.ExtensionAPI`, Task 4 `Client` and `Tool`.
- Produces: `ExtensionOptions`, `NewExtension(ExtensionOptions) (pi.Extension, error)` and MCP-backed `ai.Tool`.
- Guarantees: allowlist complete、名称确定、默认不并发、Close 关闭 MCP Client。

- [ ] **Step 1: Write failing proxy tool tests**

```go
func TestProxyToolMapsDefinitionAndText(t *testing.T) {
    caller := &toolCallerFake{result: CallToolResult{
        Content: []Content{{Type: "text", Text: "result one"}, {Type: "text", Text: "result two"}},
    }}
    tool := newProxyTool(caller, Tool{
        Name: "web_search_exa", Title: "Web search", Description: "Search the web",
        InputSchema: map[string]any{"type": "object"},
    }, "web_search_exa")
    output, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"go"}`), nil)
    if err != nil {
        t.Fatal(err)
    }
    if definition := tool.Definition(); definition.Name != "web_search_exa" || definition.ParallelSafe {
        t.Fatalf("definition = %#v", definition)
    }
    if got, _ := ai.TextContent(output.Content); got != "result one\nresult two" {
        t.Fatalf("text = %q", got)
    }
}
```

Add cases for empty text, structuredContent fallback, mixed/unsupported content, `isError:true`, cancellation and caller errors. Errors must omit arguments.

- [ ] **Step 2: Write failing Extension discovery tests**

Inject a fake client returning Exa tools plus an unrelated tool. Assert only allowlisted tools register, optional prefix works, a missing allowed tool fails, duplicate allowlist fails construction, and Close delegates exactly once.

- [ ] **Step 3: Run and verify failure**

Run: `go test ./pi/mcp -run 'TestProxyTool|TestExtension' -count=1`

Expected: FAIL because proxy/extension types do not exist.

- [ ] **Step 4: Implement the proxy adapter**

```go
type toolCaller interface {
    CallTool(context.Context, string, json.RawMessage) (CallToolResult, error)
}

type proxyTool struct {
    caller      toolCaller
    remoteName string
    definition ai.ToolDefinition
}
```

Join text blocks with one newline. For `isError`, return a bounded remote error and no successful output. If no text and structuredContent exists, marshal it as one JSON text block. Any non-text content returns unsupported-content instead of being dropped.

- [ ] **Step 5: Implement Extension construction, discovery and Close**

```go
type ExtensionOptions struct {
    Name       string
    Endpoint   string
    Headers    http.Header
    Timeout    time.Duration
    AllowTools []string
    ToolPrefix string
    HTTPClient *http.Client
}

func NewExtension(ExtensionOptions) (pi.Extension, error)
```

Normalize the identity by concatenating `mcp:` with the normalized `Name` value, so Exa becomes `mcp:exa`. Reject blank/duplicate allowlist values and invalid prefixes. Register initializes, lists, verifies every allowed name, builds all proxies, sorts by exposed name, then calls `RegisterTool`. Do not expose unlisted tools. Close is safe after partial Register. Add an unexported fake-client constructor for tests.

- [ ] **Step 6: Run MCP package tests**

```bash
go test ./pi/mcp -count=1
go test -race ./pi/mcp -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit Task 5**

```bash
git add pi/mcp/tool.go pi/mcp/tool_test.go pi/mcp/extension.go pi/mcp/extension_test.go
git commit -m "feat(mcp): expose remote tools as Pi extensions"
```

### Task 6: Add generic MCP configuration and Exa template

**Files:**
- Modify: `config/config.go`
- Modify: `config/validate.go`
- Modify: `config/config_test.go`
- Modify: `config.example.json`

**Interfaces:**
- Consumes: existing Configor JSON/YAML/TOML load and normalization.
- Produces: `Config.MCP`, `MCPConfig`, `MCPServerConfig`.
- Default: absent/disabled MCP is valid; enabled MCP is required and fail-fast.

- [ ] **Step 1: Write a failing valid Exa config test**

Use a valid base config plus:

```json
"mcp": {
  "servers": [{
    "name": " exa ",
    "enabled": true,
    "required": true,
    "url": "https://mcp.exa.ai/mcp",
    "timeout": 0,
    "header_env": {"x-api-key": " EXA_API_KEY "},
    "allow_tools": [" web_search_exa ", "web_fetch_exa"],
    "tool_prefix": ""
  }]
}
```

Assert name is `exa`, timeout defaults to 60, Header key canonicalizes to `X-Api-Key`, env value trims, and allowlist order remains stable.

- [ ] **Step 2: Write a failing validation table**

Cover duplicate enabled names, blank name/URL/allowlist, `required:false`, negative timeout, public HTTP, URL userinfo/query/fragment, duplicate tools after trimming, invalid prefix, blank Header/env, case-insensitive duplicate Header, and blocked `Host`, `Content-Length`, `Mcp-Session-Id`. Put a sentinel credential in input and assert errors omit it. Prove disabled malformed entries and absent MCP remain valid.

- [ ] **Step 3: Run and verify failure**

Run: `go test ./config -run 'TestLoadConfig.*MCP' -count=1`

Expected: FAIL because MCP config fields do not exist.

- [ ] **Step 4: Add structs and normalization**

```go
type MCPConfig struct {
    Servers []MCPServerConfig `json:"servers" yaml:"servers" toml:"servers"`
}

type MCPServerConfig struct {
    Name       string            `json:"name" yaml:"name" toml:"name"`
    Enabled    bool              `json:"enabled" yaml:"enabled" toml:"enabled"`
    Required   bool              `json:"required" yaml:"required" toml:"required"`
    URL        string            `json:"url" yaml:"url" toml:"url"`
    Timeout    int               `json:"timeout" yaml:"timeout" toml:"timeout"`
    HeaderEnv  map[string]string `json:"header_env" yaml:"header_env" toml:"header_env"`
    AllowTools []string          `json:"allow_tools" yaml:"allow_tools" toml:"allow_tools"`
    ToolPrefix string            `json:"tool_prefix" yaml:"tool_prefix" toml:"tool_prefix"`
}
```

Call `MCP.normalizeAndValidate` from the top-level flow. Canonicalize Header keys while detecting case-folded duplicates and copy maps/slices. Permit HTTP only for loopback IP or `localhost`; reject all query strings. Disabled entries do not resolve or validate network fields.

- [ ] **Step 5: Add Exa to config.example.json**

Add the same Exa server with `enabled:true`, `required:true`, URL `https://mcp.exa.ai/mcp`, timeout 60, `x-api-key -> EXA_API_KEY`, and the two allowed tools. Do not place a real credential in the file.

- [ ] **Step 6: Run all config tests**

Run: `go test ./config -count=1`

Expected: PASS, including existing platform, Redis, MySQL, HTTP and workspace tests.

- [ ] **Step 7: Commit Task 6**

```bash
git add config/config.go config/validate.go config/config_test.go config.example.json
git commit -m "feat(config): add HTTP MCP server settings"
```

### Task 7: Compose Exa MCP into the Web Fx graph

**Files:**
- Create: `application/web/mcp.go`
- Create: `application/web/mcp_test.go`
- Modify: `application/web/register.go`
- Modify: `application/web/register_test.go`

**Interfaces:**
- Consumes: `config.MCPServerConfig`, `mcp.NewExtension`, environment variables and `agent_extensions`.
- Produces: `newMCPExtensions(*config.Config) (mcpExtensionsOut, error)`.
- Boundary: `agentRegister` remains usable without business Config; top-level `Register` adds MCP.

- [ ] **Step 1: Write failing config-to-extension tests**

Define:

```go
type mcpExtensionsOut struct {
    fx.Out
    Extensions []pi.Extension `group:"agent_extensions,flatten"`
}
```

Test no servers, disabled server, missing `EXA_API_KEY`, and successful Header resolution with `t.Setenv`. Missing-secret errors mention Header/env names but no values.

- [ ] **Step 2: Write a failing discovery-before-consumer Fx test**

Use `httptest.Server` for initialize, notification and tools/list. Build the agent graph with `agentRegister`, `newMCPExtensions`, supplied Config/WorkDir/provider, then append a consumer lifecycle hook that asserts both Exa definitions already exist:

```go
fx.Invoke(func(lifecycle fx.Lifecycle, runtime pi.ToolRuntime) {
    lifecycle.Append(fx.Hook{OnStart: func(context.Context) error {
        names := definitionNames(runtime.Definitions())
        if !slices.Contains(names, "web_search_exa") || !slices.Contains(names, "web_fetch_exa") {
            return fmt.Errorf("MCP tools unavailable at consumer start: %v", names)
        }
        return nil
    }})
})
```

Add a missing-`web_fetch_exa` response; Start must fail and the consumer hook must not run.

- [ ] **Step 3: Run and verify failure**

Run: `go test ./application/web -run 'TestMCP|TestWeb.*MCP' -count=1`

Expected: FAIL because the provider does not exist.

- [ ] **Step 4: Implement config-to-extension conversion**

For every enabled server: resolve each Header env with `os.LookupEnv`, reject missing/empty values, build a fresh `http.Header`, convert seconds to `time.Duration`, call `mcp.NewExtension`, and append the returned extension. Never log or wrap the resolved map.

- [ ] **Step 5: Mount only in top-level Web Register**

```go
fx.Provide(
    config.NewFromEnvironment,
    config.NewPlatform,
    NewChatWorkDir,
    agentprofiledriver.NewCatalog,
    newMCPExtensions,
)
```

Keep `agentRegister` independently usable. Fx groups are graph-wide, so `pi.CoreRegister` receives extensions without importing `pi/mcp`.

- [ ] **Step 6: Preserve registration compatibility**

Keep current explicit-business-tool, direct-general-chat and graph-validation tests passing. Add a no-MCP start test asserting the original tool list remains unchanged.

- [ ] **Step 7: Run Web and Pi tests**

```bash
go test ./application/web ./pi ./pi/test -count=1
go test -race ./application/web ./pi -count=1
```

Expected: PASS; discovery completes before the simulated consumer hook.

- [ ] **Step 8: Commit Task 7**

```bash
git add application/web/mcp.go application/web/mcp_test.go application/web/register.go application/web/register_test.go
git commit -m "feat(web): register Exa through HTTP MCP"
```

### Task 8: Add boundaries, live Exa smoke coverage and final verification

**Files:**
- Create: `pi/mcp/exa_integration_test.go`
- Modify: `pi/test/package_boundaries_test.go`
- Modify only for a factual final mismatch: `docs/superpowers/specs/2026-08-18-pi-http-mcp-extension-design.md`

**Interfaces:**
- Consumes: completed MCP client and `EXA_API_KEY`.
- Produces: opt-in live verification and enforced import direction.
- Gate: both `GO_REAGENT_EXA_INTEGRATION=1` and a non-empty `EXA_API_KEY`.

- [ ] **Step 1: Add package-boundary coverage**

```go
func TestMCPDependencyDirection(t *testing.T) {
    for _, pkg := range []string{modulePath + "/pi", modulePath + "/pi/ai", modulePath + "/pi/harness"} {
        for _, dependency := range goListDependencies(t, pkg) {
            if dependency == modulePath+"/pi/mcp" {
                t.Fatalf("%s must not depend on pi/mcp", pkg)
            }
        }
    }
    dependencies := goListDependencies(t, modulePath+"/pi/mcp")
    if !slices.Contains(dependencies, modulePath+"/pi") || !slices.Contains(dependencies, modulePath+"/pi/ai") {
        t.Fatalf("pi/mcp dependencies = %v", dependencies)
    }
}
```

- [ ] **Step 2: Add the opt-in Exa test**

```go
func TestExaHostedMCP(t *testing.T) {
    if os.Getenv("GO_REAGENT_EXA_INTEGRATION") != "1" {
        t.Skip("set GO_REAGENT_EXA_INTEGRATION=1 to run")
    }
    apiKey := strings.TrimSpace(os.Getenv("EXA_API_KEY"))
    if apiKey == "" {
        t.Skip("set EXA_API_KEY to run")
    }
    transport, err := NewHTTPTransport(HTTPTransportOptions{
        Endpoint: "https://mcp.exa.ai/mcp",
        Headers:  http.Header{"X-Api-Key": []string{apiKey}},
        Timeout:  60 * time.Second,
    })
    if err != nil {
        t.Fatal(err)
    }
    client, err := NewClient(transport, "go-reagent-integration-test", "1")
    if err != nil {
        t.Fatal(err)
    }
    t.Cleanup(func() { _ = client.Close(context.Background()) })
    if err := client.Initialize(context.Background()); err != nil {
        t.Fatal(err)
    }
    tools, err := client.ListTools(context.Background())
    if err != nil {
        t.Fatal(err)
    }
    requireRemoteTools(t, tools, "web_search_exa", "web_fetch_exa")
    search, err := client.CallTool(context.Background(), "web_search_exa", json.RawMessage(`{"query":"official Go programming language website","numResults":1}`))
    if err != nil || search.IsError || len(search.Content) == 0 {
        t.Fatalf("search content count = %d, isError = %v, error = %v", len(search.Content), search.IsError, err)
    }
    fetched, err := client.CallTool(context.Background(), "web_fetch_exa", json.RawMessage(`{"url":"https://go.dev/"}`))
    if err != nil || fetched.IsError || len(fetched.Content) == 0 {
        t.Fatalf("fetch content count = %d, isError = %v, error = %v", len(fetched.Content), fetched.IsError, err)
    }
}
```

The helper prints tool names only, never API Key, Headers or full content.

- [ ] **Step 3: Run offline verification**

```bash
gofmt -w pi/extension.go pi/extension_runtime.go pi/extension_test.go pi/extension_runtime_test.go pi/tool_registry.go pi/tool_registry_test.go pi/tool_runtime.go pi/register.go pi/register_test.go pi/mcp config/config.go config/validate.go config/config_test.go application/web/mcp.go application/web/mcp_test.go application/web/register.go application/web/register_test.go pi/test/package_boundaries_test.go
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
git diff --check
```

Expected: all commands exit 0; the live Exa test reports SKIP.

- [ ] **Step 4: Run the live smoke test when credentials are available**

```bash
GO_REAGENT_EXA_INTEGRATION=1 go test ./pi/mcp -run '^TestExaHostedMCP$' -count=1 -v
```

Expected: PASS after discovering and calling both tools. If credentials or outbound network are unavailable, record only this check as skipped; do not weaken offline tests.

- [ ] **Step 5: Compare against the design and commit**

Map every spec acceptance bullet to a passing test or live-smoke status. Update the design only if the implemented contract factually differs, then run `git diff --check`.

```bash
git add pi/mcp/exa_integration_test.go pi/test/package_boundaries_test.go docs/superpowers/specs/2026-08-18-pi-http-mcp-extension-design.md
git commit -m "test(mcp): verify Exa integration boundaries"
```

- [ ] **Step 6: Report final evidence**

Report exact offline test/race/vet/diff results, live Exa status, final commit range, and confirmation that unrelated pre-existing worktree changes were neither staged nor altered.
