# Pi HTTP MCP 扩展与 Exa 接入设计

日期：2026-08-18

## 背景

`pi` 当前通过 Fx 的 `agent_tools` value group 收集编译期工具，`NewToolRuntime` 在依赖图构建时把这些工具固化为不可变映射。这个模型适合本地 Go 工具，但无法表达需要在应用启动时先连接远端、再通过 `tools/list` 发现工具的 MCP Server。

本期参考 Pi SDK 与 `pi-web-access` 的分层方式，但不照搬 TypeScript 的磁盘动态加载能力：

```text
Pi SDK extension framework
  -> pi-web-access extension entry
    -> Exa provider implementation

go-reagent
  -> pi Extension Runtime
    -> HTTP MCP Extension
      -> Exa Hosted MCP configuration
```

Exa 是本期第一个真实接入，不是未来示例或空接口占位。

## 目标

- 在 `pi` 中建立 Go 编译期扩展契约和 Fx 生命周期运行时。
- 扩展能够在启动阶段注册 `ai.Tool`，并在应用停止时释放资源。
- 实现通用 MCP Streamable HTTP 客户端和工具代理。
- 通过 `https://mcp.exa.ai/mcp` 发现并调用 `web_search_exa` 与 `web_fetch_exa`。
- 保持 Agent、Loop、Scheduler 与模型 Provider 不感知工具来自本地还是 MCP。
- 保持现有 `agent_tools` 注册方式和 `NewToolRuntime` 公共构造方式可用。
- 不影响未配置 MCP 的现有 CLI、Web 和测试启动行为。

## 非目标

本期不实现：

- 从磁盘加载第三方 Go 代码；
- Go Plugin、WASM 或独立扩展进程；
- 运行中的扩展热加载、卸载或工具热更新；
- MCP stdio transport；
- MCP resources、prompts、sampling 或 server-to-client 请求；
- 完整 OAuth 登录流程；
- 自动向模型暴露远端以后新增的所有工具；
- `pi-web-access` 的 Provider 路由、Exa REST API 快速路径和跨 Provider fallback。

## 方案选择

采用 Go 编译期扩展：扩展实现作为 Go 包参与编译，通过 Fx value group 注入。扩展代码受 Go 类型系统和现有依赖图约束，远端能力则通过标准 MCP 协议在运行时发现。

没有选择以下方案：

- **仅实现 Exa 专用客户端**：交付快，但把 Exa 协议细节带入 Agent Core，下一家 MCP Server 需要重新设计。
- **直接实现磁盘动态插件**：更接近 TypeScript Pi，但需要解决 ABI、隔离、升级、安全和部署问题，超出本期目标。

## 目标目录

```text
pi/
├── extension.go                    # 扩展、ExtensionAPI 和生命周期契约
├── extension_runtime.go            # Fx 生命周期、启动顺序、回滚和冻结
├── extension_test.go
├── extension_runtime_test.go
├── tool_registry.go                # 启动阶段可写、运行阶段只读的工具注册表
├── tool_registry_test.go
├── tool_runtime.go                 # 通过 ToolRegistry 发现与执行工具
├── register.go                     # CoreRegister 的 Fx 组装
└── mcp/
    ├── extension.go                # MCP Server 发现和代理工具注册
    ├── client.go                   # initialize/list/call/close
    ├── protocol.go                 # 本期所需 JSON-RPC/MCP 类型
    ├── transport_http.go           # Streamable HTTP、JSON/SSE 和 Session
    ├── tool.go                     # 远端 MCP Tool 到 ai.Tool 的适配
    ├── errors.go                   # 协议、传输与远端调用错误
    ├── extension_test.go
    ├── client_test.go
    ├── transport_http_test.go
    └── tool_test.go

pi/test/
└── package_boundaries_test.go        # 声明 MCP 扩展的允许依赖方向

config/
├── config.go                       # MCPConfig、MCPServerConfig
├── validate.go                     # MCP 配置规范化与校验
└── config_test.go

application/web/
├── register.go                     # 挂载 MCP 组合模块
└── mcp.go                          # 业务配置到 MCP Extension 的 Fx 适配

config.example.json                 # Exa 示例配置
```

`pi` 根包不导入 `pi/mcp`。`pi/mcp` 可以依赖 `pi` 和 `pi/ai`，最终由 `application/web/mcp.go` 组装，因此不会形成 Go import cycle。

## 扩展契约

公共契约保持最小：

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

语义如下：

- `Name` 是扩展实例的稳定唯一标识，去除首尾空白后不能为空。
- `Register` 由 `ExtensionRuntime` 的 Fx `OnStart` 调用，可以执行远端发现并注册工具。
- 实现 `ExtensionCloser` 的扩展由运行时在 Fx `OnStop` 逆序关闭。
- `ExtensionAPI` 本期只开放工具注册；以后增加命令或其他能力时通过新增小接口演进，而不向扩展暴露整个 Fx 容器。
- 同名扩展、nil/typed-nil 扩展和空名称在启动前报错。

生命周期由 `ExtensionRuntime` 统一拥有，而不是让扩展直接操作 `fx.Lifecycle`。这样扩展包保持 Fx 无关，运行时可以统一处理顺序、回滚和日志脱敏。

## Extension Runtime

Fx 使用独立 value group 收集扩展：

```go
fx.Annotate(
    NewMCPExtension,
    fx.As(new(pi.Extension)),
    fx.ResultTags(`group:"agent_extensions"`),
)
```

`ExtensionRuntime` 的行为：

1. 构造时收集和校验扩展，并按名称排序，保证启动与测试稳定。
2. Fx `OnStart` 依次调用扩展 `Register`。
3. 每个扩展只能通过绑定到自身的 `ExtensionAPI` 注册工具，运行时记录工具归属。
4. 任一必需扩展启动失败时，逆序关闭当前及之前已启动的可关闭扩展，然后返回错误，使 Fx 启动失败。
5. 所有扩展成功后冻结 `ToolRegistry`。
6. Fx `OnStop` 逆序关闭已经成功启动的扩展；关闭错误被汇总返回，但不跳过其他扩展。

`ToolRuntime` 的 Fx 构造依赖 `ExtensionRuntime`，形成以下依赖顺序：

```text
static agent_tools
  -> ToolRegistry
    -> ExtensionRuntime (append lifecycle hook)
      -> ToolRuntime
        -> Agent
          -> Web handlers/server
```

因此 ExtensionRuntime 的启动钩子先于 Web Server 的启动钩子执行，HTTP 服务不会在 MCP 工具发现完成前接收请求。

## Tool Registry 与 ToolRuntime

新增的 `ToolRegistry` 在启动阶段可写，成功启动后只读。它持有 `map[string]toolEntry` 和 `sync.RWMutex`，但不会在持锁状态执行工具。

注册规则：

- 拒绝 nil 和 typed-nil 工具；
- 工具名去除首尾空白后不能为空，且原名称不能包含首尾空白；
- 同名工具直接返回错误，不覆盖本地或其他扩展的工具；
- 注册时编译 JSON Schema validator，失败则不写入注册表；
- 冻结后任何注册都返回明确错误；
- 扩展注册失败时移除该扩展本次已注册的工具，避免部分提交。

运行规则：

- `Definitions` 返回按名称排序的稳定快照；
- `Execute` 在读锁下完成查找后立即释放锁；
- Scheduler 和中间件继续作用于本地与 MCP 工具；
- MCP 工具一期默认 `ParallelSafe: false`。远端 annotations 只是提示，不能据此假设未知工具无副作用；Exa 搜索的并行优化留到有显式本地策略时再做。

兼容规则：

- `agent_tools` value group 继续有效；
- `NewToolRuntime(ToolRuntimeOptions)` 保持现有公共行为：从传入工具构建并冻结一个独立注册表；
- Fx 图使用共享 `ToolRegistry` 的内部构造路径，以便 ExtensionRuntime 在启动阶段补充工具；
- Agent、Loop、Scheduler 和 `ai.Tool` 接口不变。

## 通用 HTTP MCP 扩展

一个 MCP Server 配置对应一个 `mcp.Extension` 实例。扩展启动过程：

1. 创建带超时和认证 Header 的 HTTP Transport。
2. 使用 MCP protocol version `2025-03-26` 发送 JSON-RPC `initialize`，并校验服务端协商结果。
3. 校验响应并记录可选的 `Mcp-Session-Id`。
4. 发送 `notifications/initialized`。
5. 调用 `tools/list`，跟随分页 cursor 直到结束。
6. 只保留 `allow_tools` 中明确允许的工具。
7. 验证远端工具名称、描述和 `inputSchema`。
8. 为每个远端工具创建 MCP proxy `ai.Tool`。
9. 全部代理构建成功后再注册，避免部分工具可见。
10. 停止时使用带 Session ID 的 HTTP DELETE 尝试关闭会话，并关闭空闲连接。

扩展不会硬编码 Exa 工具协议。Exa 只通过 URL、认证和允许列表进入系统。

## HTTP Transport

Transport 实现 MCP Streamable HTTP 的本期子集：

- 请求使用 HTTP POST、`Content-Type: application/json`；
- `Accept` 同时声明 `application/json` 与 `text/event-stream`；
- 后续请求携带服务端返回的 `Mcp-Session-Id`；
- 支持单个 JSON-RPC JSON 响应；
- 支持 SSE 事件，解析每个 `data:` payload，并忽略注释和非 `data` 字段；
- 通过 JSON-RPC id 选择当前请求的最终响应；
- notification 请求允许服务端返回无响应体的 202 或 204；
- HTTP 非 2xx、非法 Content-Type、畸形 JSON/SSE、JSON-RPC error 和响应 id 不匹配均返回分类错误；
- 所有请求继承调用方 Context，并额外受 Server timeout 限制；
- 单个 HTTP 响应体最多读取 16 MiB，超限返回错误，防止异常服务端无限占用内存；
- 日志不得输出认证 Header、完整请求参数或完整远端内容。

HTTP 客户端禁止自动把认证 Header 转发到跨 Host 重定向目标。远端 HTTPS Server 可以重定向到同 Host；跨 Host 重定向应返回错误。明文 HTTP 只允许 loopback 地址，便于本地 MCP Server 和测试使用。

## MCP Tool 适配

远端定义映射：

```text
MCP name         -> ai.ToolDefinition.Name
MCP title/name   -> ai.ToolDefinition.Label
MCP description  -> ai.ToolDefinition.Description
MCP inputSchema  -> ai.ToolDefinition.InputSchema
```

执行时，`ai.Tool.Execute` 把原始 JSON arguments 作为 `tools/call.params.arguments` 转发。参数仍先经过现有 ToolRuntime 的 JSON Schema 校验。

本期支持 MCP `content` 中的文本块。Exa 的搜索和抓取结果属于这个范围。若成功响应只包含 `structuredContent`，适配器把它序列化为 JSON 文本；无法表示的非文本内容返回明确的 unsupported-content 错误，不静默丢失。

MCP `isError: true` 转换为工具执行错误；它只使当前 ToolResult 标记为错误，不终止 Agent 或应用。Context 取消和 deadline 保持现有 ToolRuntime 语义。

## 配置

在顶层配置增加：

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

`timeout` 单位为秒，启用时默认 60。`header_env` 的 key 是 HTTP Header 名，value 是环境变量名，密钥不写入版本控制中的配置文件。

Exa 示例：

```json
{
  "mcp": {
    "servers": [
      {
        "name": "exa",
        "enabled": true,
        "required": true,
        "url": "https://mcp.exa.ai/mcp",
        "timeout": 60,
        "header_env": {
          "x-api-key": "EXA_API_KEY"
        },
        "allow_tools": [
          "web_search_exa",
          "web_fetch_exa"
        ],
        "tool_prefix": ""
      }
    ]
  }
}
```

规范化与校验规则：

- 没有 Server 或所有 Server 均禁用时不创建 MCP 扩展，现有行为不变；
- 启用的 Server 名称、URL 和允许工具列表不能为空；
- 启用的 Server 名称不能重复；
- URL 不允许 userinfo、query 中的秘密或 fragment；
- 非 loopback Server 必须使用 HTTPS；
- timeout 必须大于 0，零值规范化为 60 秒；
- Header 名和环境变量名必须非空，禁止配置 Host、Content-Length、Mcp-Session-Id 等协议控制 Header；
- 配置了 Header 环境变量但运行时找不到值时，扩展启动失败；
- `allow_tools` 去空白、去重后必须非空；
- `tools/list` 必须发现允许列表中的全部工具，缺少任一工具都视为启动失败；
- `tool_prefix` 为空时保留远端名称；非空时生成 `<prefix>_<remote-name>`；
- 最终工具名称冲突由 ToolRegistry 拒绝。

Exa 支持匿名限流访问，但上面的生产示例明确要求 `EXA_API_KEY`。若要匿名访问，应删除 `header_env`，而不是把空密钥发送给远端。

## Fx 业务组装

`pi.CoreRegister` 增加 ToolRegistry 和 ExtensionRuntime，但不选择任何具体扩展。`application/web/mcp.go`：

- 读取 `config.MCP.Servers`；
- 为每个启用的 Server 构造一个名为 `mcp:<server-name>` 的通用 `mcp.Extension`；
- 使用 `group:"agent_extensions,flatten"` 把扩展切片放入 value group；
- 不把业务 `config.Config` 引入 `pi` 或 `pi/mcp`。

本期只在长期运行的 Web 应用组合根挂载 Exa。默认 Coding CLI 不自动获得网络搜索权限；未来若 CLI 需要 MCP，可在 CLI 组合根复用同一个模块。

## 启动、调用与停止数据流

### 启动

```text
load + validate config
  -> collect static agent_tools
  -> create ToolRegistry
  -> collect agent_extensions
  -> Exa MCP initialize
  -> notifications/initialized
  -> tools/list
  -> filter allow_tools
  -> create + register proxy tools
  -> freeze ToolRegistry
  -> start Web server
```

### 调用

```text
Agent prepares ToolRuntime.Definitions
  -> model selects web_search_exa or web_fetch_exa
  -> Scheduler invokes MCP proxy ai.Tool
  -> schema validation
  -> MCP tools/call
  -> Exa JSON or SSE response
  -> MCP content to ai.ToolOutput
  -> ToolResult returned to model
```

### 停止

```text
stop accepting HTTP work
  -> wait/cancel in-flight Agent runs through existing lifecycle
  -> close MCP extensions in reverse order
  -> DELETE remote session when available
  -> close idle HTTP connections
```

关闭会话是 best effort，但关闭错误必须记录并由 Fx Stop 返回；不得因为 DELETE 失败而跳过本地连接清理。

## 错误策略

### 启动错误

本期采用 fail-fast：配置中启用的 MCP Server 若发现失败，应用启动失败。`required` 在一期对所有启用 Server 均要求为 `true`；保留该字段是为了后续增加可选扩展策略，但一期拒绝 `enabled: true, required: false`，避免引入后台重试和运行时工具集合变化。

启动错误包括：

- 配置或密钥环境变量无效；
- 网络连接、TLS、超时或 Context 取消；
- initialize、initialized notification 或 tools/list 失败；
- 服务端未提供允许列表中的全部工具；
- 远端 schema 无法作为本地 ToolDefinition 使用；
- 扩展名或工具名冲突。

错误包含 Server 名、操作阶段和可诊断根因，但不包含 API Key、认证 Header 或完整远端响应。

### 调用错误

- HTTP/协议/JSON-RPC/MCP `isError` 转成当前工具的错误结果；
- Context canceled/deadline exceeded 保持现有向上返回行为；
- 单次工具失败不移除工具，也不重启 MCP Session；
- 本期不自动重试 `tools/call`，避免对未知幂等性的工具重复执行；
- Session 失效返回明确错误，应用重启后重新发现；本期不运行时重建 Session。

## 测试策略

### Extension 与 Registry 单元测试

- 扩展排序、重复名称、nil/typed-nil 和空名称；
- 启动成功、部分失败回滚、逆序关闭和关闭错误汇总；
- 本地工具与扩展工具共同可见；
- 注册事务、名称冲突、非法 schema 和冻结后注册；
- `Definitions` 稳定排序；
- 并发读取和执行通过 race 检查；
- `NewToolRuntime(ToolRuntimeOptions)` 兼容行为。

### MCP 协议与 Transport 单元测试

使用 `httptest.Server` 覆盖：

- JSON initialize/list/call；
- SSE 单行、多行、注释、多个事件和结束；
- Session ID 保存、后续发送和 DELETE；
- tools/list 分页；
- API Key Header 从环境变量注入且错误中不泄漏；
- HTTP 非 2xx、错误 Content-Type、畸形 JSON/SSE、响应 id 不匹配；
- JSON-RPC error、MCP `isError`、超时和主动取消；
- 响应体预算和跨 Host 重定向拒绝。

### MCP Tool 与 Fx 集成测试

- 远端 definition 正确转换为 `ai.ToolDefinition`；
- arguments 原样进入 `tools/call`；
- text 与 structuredContent 转换；
- unsupported content 返回错误；
- Web Fx 图在 MCP 发现成功后启动；
- 必需 MCP 失败时 Web Server 不开始监听；
- 未配置 MCP 时完整 Fx 图保持现有行为；
- Exa allowlist 缺少任一工具时启动失败。

包边界测试同时确认：`pi/mcp` 可以依赖 `pi` 与 `pi/ai`，`pi`、`pi/ai` 和 `pi/harness` 不得反向依赖 `pi/mcp`。

### 真实 Exa smoke test

提供默认跳过的集成测试或独立验证命令，仅在显式设置 `EXA_API_KEY` 和 opt-in 环境变量时执行：

1. 连接 Exa Hosted MCP；
2. 发现 `web_search_exa` 与 `web_fetch_exa`；
3. 执行一个有界搜索；
4. 验证返回至少一个非空文本内容；
5. 关闭 Session。

普通 CI 和单元测试不得依赖公网或真实 Exa 配额。

## 验收标准

- 不配置 MCP 时，现有应用行为和工具集合不变；
- 启用 Exa 配置后，Web 应用在开始监听前完成 MCP 初始化和工具发现；
- 模型可见 `web_search_exa` 与 `web_fetch_exa` 的远端 schema；
- 两个工具均可通过 `tools/call` 到达 Exa 并把文本内容返回模型；
- JSON 与 SSE 响应均可解析；
- 超时和调用方取消能够中止 HTTP 请求；
- 配置、发现、工具冲突和会话错误不会泄漏密钥；
- 必需 Exa 不可用时应用 fail-fast，不以缺少工具的降级状态运行；
- Fx 停止时关闭 MCP Session 和本地 HTTP 资源；
- `go test ./...`、`go test -race ./...`、`go vet ./...` 和 `git diff --check` 通过。

## 后续演进

一期完成后可以独立评估：

- `required: false`、后台重连与原子替换工具快照；
- MCP stdio transport；
- OAuth credential provider；
- tools/list change notification；
- 运行时扩展进程、WASM 或受控磁盘加载；
- 对上层暴露稳定别名工具，例如把多个 Provider 统一为 `web_search`。

这些演进不得改变一期的核心边界：Agent 只依赖 `ai.Tool`，协议和 Provider 细节留在扩展实现中。
