# Pi MCP stdio Transport 设计

日期：2026-08-27

## 背景

`go-reagent` 已在 `pi/mcp` 中实现 MCP Client、Streamable HTTP Transport、启动期工具发现和远端工具代理。当前 `NewExtension` 固定创建 `HTTPTransport`，业务配置也只表达 URL 与 HTTP Header，因此只能连接 Exa 等远端 MCP Server，不能连接通过本地子进程提供能力的标准 stdio MCP Server。

MCP 规范把 stdio 与 Streamable HTTP 定义为两种标准 Transport，并建议 Client 在条件允许时支持 stdio。stdio 模式下由 Client 启动 Server 子进程，通过 stdin/stdout 交换 UTF-8、单行分隔的 JSON-RPC 消息；stderr 只承载日志，不属于协议流。

Pi Coding Agent 本身把 MCP 保持在 Extension 边界之外，pi.dev 收录的 `pi-mcp-adapter` 也以独立 Extension 接入 MCP。该适配器使用常见的 `command`、`args`、`env`、`cwd` 配置表达 stdio Server。本文借鉴这一配置形态，但不引入其单代理工具、懒启动、磁盘元数据缓存或多宿主配置发现能力。

## 决策

在现有 `pi/mcp` 包内增加原生 `StdioTransport`，继续复用当前 `Client`、`mcpExtension`、`allow_tools` 白名单、Tool Registry 和 Fx 生命周期。

本次不是新建第二套 MCP Runtime，也不以第三方 MCP SDK 替换现有 HTTP Client。HTTP 与 stdio 只在 Transport 和进程生命周期处不同；initialize、tools/list、tools/call、工具命名、白名单校验和错误包装保持同一条路径。

## 目标

- 连接通过本地可执行程序启动的标准 MCP stdio Server。
- 遵守 MCP stdio 的 UTF-8、JSON-RPC、单行消息和 stderr 隔离要求。
- 支持多个并发 `tools/call`，按 JSON-RPC ID 正确路由乱序响应。
- 把 HTTP 与 stdio 收敛为一套显式、无默认分支的 Transport 配置契约。
- 保持 MCP Server 启动期初始化、工具发现和必需工具 fail-fast 语义。
- 在请求取消、Server 异常退出和应用停止时确定性释放子进程及其后代。
- 配置错误、进程错误和协议错误不得泄漏环境变量值、请求参数或工具结果。
- 在 Linux、macOS 和 Windows 上提供等价的子进程树清理语义。

## 非目标

本期不实现：

- 把 go-reagent 暴露为 stdio MCP Server；
- `.mcp.json`、Claude、Cursor、Codex 等外部配置文件的自动发现或合并；
- Pi MCP Adapter 的单一 `mcp` 代理工具、按需工具搜索或元数据磁盘缓存；
- MCP Server 懒启动、空闲关闭、自动重连或运行时热更新；
- resources、prompts、sampling、elicitation 或 roots；
- tools/list change notification 驱动的 Registry 热替换；
- shell 命令字符串、管道、重定向或 shell 插值；
- 用第三方 Go MCP SDK 替换现有协议实现；
- 改变 `required: true`、`allow_tools` 或 `tool_prefix` 的现有产品语义。
- 为当前仓库内尚未稳定发布的 MCP 配置或 Go 构造 API 保留兼容层。

## 参考与约束来源

- MCP Transport 规范：<https://modelcontextprotocol.io/specification/2025-06-18/basic/transports#stdio>
- Pi Extension 文档：<https://pi.dev/docs/latest/extensions>
- pi.dev MCP Adapter 条目：<https://pi.dev/packages/pi-mcp-adapter>
- Pi MCP Adapter 配置与实现：<https://github.com/nicobailon/pi-mcp-adapter>

参考内容用于确认标准线路和生态配置习惯，不形成对 TypeScript 实现或其完整功能集的兼容承诺。

## 总体架构

```text
config.MCPServerConfig
        │
        ▼
infrastructure/driver/mcp.NewExtensions
        │  按必填 transport 创建具体实现
        ├── transport=http  ──► HTTPTransport
        │
        └── transport=stdio ──► StdioTransport
                                  │
                                  ├── child stdin  ◄── JSON-RPC JSONL
                                  ├── child stdout ──► reader/dispatcher
                                  ├── child stderr ──► bounded drain
                                  └── process group ─► close/force kill

具体 Transport
        ▼
pi/mcp.NewExtension(Transport)
        ▼
pi/mcp.Client
        ▼
initialize → notifications/initialized → tools/list → tools/call
        ▼
mcpExtension → allow_tools → proxyTool → Tool Registry
```

`Transport` 接口保持不变：

```go
type Transport interface {
	Send(context.Context, Request) (Response, error)
	Close(context.Context) error
}
```

并发路由、Server 消息处理和子进程状态全部封装在 `StdioTransport` 内，上层 Client 不需要感知连接类型。

## 配置模型

### 字段

在 `MCPServerConfig` 中增加：

```go
type MCPServerConfig struct {
	Name       string            `json:"name" yaml:"name" toml:"name"`
	Enabled    bool              `json:"enabled" yaml:"enabled" toml:"enabled"`
	Required   bool              `json:"required" yaml:"required" toml:"required"`
	Transport  string            `json:"transport" yaml:"transport" toml:"transport"`
	URL        string            `json:"url" yaml:"url" toml:"url"`
	Command    string            `json:"command" yaml:"command" toml:"command"`
	Args       []string          `json:"args" yaml:"args" toml:"args"`
	Env        map[string]string `json:"env" yaml:"env" toml:"env"`
	CWD        string            `json:"cwd" yaml:"cwd" toml:"cwd"`
	Timeout    int               `json:"timeout" yaml:"timeout" toml:"timeout"`
	HeaderEnv  map[string]string `json:"header_env" yaml:"header_env" toml:"header_env"`
	AllowTools []string          `json:"allow_tools" yaml:"allow_tools" toml:"allow_tools"`
	ToolPrefix string            `json:"tool_prefix" yaml:"tool_prefix" toml:"tool_prefix"`
}
```

`transport` 是已启用 Server 的必填字段，只接受 `http` 或 `stdio`。空值、未知值一律在配置加载期失败，不根据 `url` 或 `command` 猜测 Transport。

### 互斥规则

| Transport | 必填 | 可选 | 必须为空 |
|---|---|---|---|
| `http` | `url` | `header_env`、`timeout` | `command`、`args`、`env`、`cwd` |
| `stdio` | `command` | `args`、`env`、`cwd`、`timeout` | `url`、`header_env` |

所有已启用 Server 仍必须设置 `required: true` 和非空 `allow_tools`。禁用的 Server 延续当前行为，不校验其 Transport 专属字段。

### stdio 示例

```json
{
  "mcp": {
    "servers": [
      {
        "name": "filesystem",
        "enabled": true,
        "required": true,
        "transport": "stdio",
        "command": "npx",
        "args": [
          "-y",
          "@modelcontextprotocol/server-filesystem",
          "./workspaces/chat"
        ],
        "env": {
          "MCP_TOKEN": "${MCP_TOKEN}",
          "LOG_LEVEL": "warn"
        },
        "cwd": ".",
        "timeout": 60,
        "allow_tools": ["read_file", "list_directory"],
        "tool_prefix": "fs"
      }
    ]
  }
}
```

### command、args 和 cwd

- `command` 是单个可执行文件名或路径，使用 `exec.Command` 类 API 直接启动，不经过 shell。
- `args` 每个元素原样作为一个 argv 传递，不做分词、变量展开、通配符展开或命令替换。
- `command`、`args` 和 `cwd` 拒绝 NUL 字节。
- `cwd` 为空时继承 go-reagent 进程工作目录。
- `cwd` 为相对路径时相对 go-reagent 进程工作目录解析；校验后写回绝对路径。
- 非空 `cwd` 必须在配置加载期确认存在且为目录。
- 不把 `cwd` 强制限制在 Agent Workspace 内。stdio Server 是部署方显式配置的受信任扩展进程，其能力边界由 Server 参数、操作系统账户和部署沙箱决定。

### env

子进程继承 go-reagent 的环境，再由 `env` 中的同名键覆盖。环境变量名必须符合 `[A-Za-z_][A-Za-z0-9_]*`。

值支持两种形式：

- 普通字符串：按字面值传入；
- 完整引用 `${NAME}`：从父进程环境读取 `NAME`，不存在或为空时配置加载失败。

本期不支持字符串中的部分插值，例如 `prefix-${NAME}`，也不支持 `$NAME`、`$env:NAME` 或命令取密。错误只可包含环境变量名称，不可包含解析后的值。配置结构和 Extension Options 不得通过 `%#v` 等方式整体写入日志。

## Transport 创建与 Extension Options

配置分支和具体 Transport 的创建属于基础设施装配职责。`NewExtension` 不再接收 URL、Header、Command 等 Transport 专属字段，只依赖已经构造完成的 `Transport`：

```go
type ExtensionOptions struct {
	Name       string
	Transport  Transport
	AllowTools []string
	ToolPrefix string
}
```

`NewExtension` 要求 `Transport` 非 nil，使用它创建 `Client`，然后组装 `mcpExtension`。这会替换当前由 `NewExtension` 固定创建 HTTPTransport 的构造 API，不保留旧签名或兼容包装函数。

基础设施驱动执行唯一的 Transport 分支：

```go
switch server.Transport {
case "http":
	transport, err = pimcp.NewHTTPTransport(pimcp.HTTPTransportOptions{
		Endpoint: server.URL,
		Headers:  headers,
		Timeout:  timeout,
	})
case "stdio":
	transport, err = pimcp.NewStdioTransport(pimcp.StdioTransportOptions{
		Command: server.Command,
		Args:    server.Args,
		Env:     env,
		WorkDir: server.CWD,
		Timeout: timeout,
	})
}

extension, err := pimcp.NewExtension(pimcp.ExtensionOptions{
	Name:       server.Name,
	Transport:  transport,
	AllowTools: server.AllowTools,
	ToolPrefix: server.ToolPrefix,
})
```

基础设施驱动负责把配置中的 `env` 合并为 `KEY=value`，但不得修改父进程环境。Transport 分支不设置 default；配置层已保证只可能进入 `http` 或 `stdio`。

`NewStdioTransport` 只校验并保存选项，不立即启动进程。第一次 `Send` 必然是 `initialize`，此时同步完成一次性启动。这样进程仍在 Extension Runtime 的启动阶段创建，构造 Fx 依赖图或执行 `fx.ValidateApp` 不会产生外部进程副作用。

## StdioTransport 结构

建议新增：

```go
type StdioTransportOptions struct {
	Command string
	Args    []string
	Env     []string
	WorkDir string
	Timeout time.Duration
}

type StdioTransport struct {
	options StdioTransportOptions

	stateMu sync.Mutex
	state   stdioState
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	done    chan struct{}
	err     error

	writeMu sync.Mutex

	pendingMu sync.Mutex
	pending   map[int64]chan stdioResult
}
```

实际实现可把状态、进程和 pending 字段拆为更小的内部结构，但必须保持以下职责边界：

- `stateMu` 只保护启动、关闭和最终错误状态；
- `writeMu` 保证一条 JSON-RPC 消息及其换行被原子写入；
- `pendingMu` 只保护 ID 到响应通道的映射；
- stdout 只有一个 reader goroutine；
- `cmd.Wait` 只有一个所有者，任何关闭路径都等待同一个 `done`；
- 不在持锁状态下等待子进程、执行阻塞 I/O 或向 pending channel 发送。

## 状态机

```text
new ── first Send ──► starting ── started ──► running
 │                       │                       │
 │                       └── failure ──► failed │
 │                                               │
 └──────────────── Close ────────────────────────┤
                                                 ▼
                                              closing
                                                 │
                                                 ▼
                                               closed
```

- `new`：选项已校验，尚未产生子进程。
- `starting`：只允许一个 goroutine 创建 pipe、启动命令和 reader/wait goroutine，其他调用等待相同启动结果。
- `running`：接受请求和 notification。
- `failed`：保存一个不含协议内容和环境变量值的稳定终端错误，之后的 `Send` 直接返回该错误。
- `closing`：拒绝新请求，关闭 stdin，并开始有界回收。
- `closed`：`Close` 幂等，之后 `Send` 返回 `mcp transport is closed`。

`Close` 与首次 `Send` 并发时，最多启动一个进程；如果关闭先取得状态所有权，则不得再启动进程。

## 消息编码与读取

### 写入

每条 `Request` 使用 `json.Marshal` 编码。编码结果必须是有效 UTF-8 JSON 且自身不含原始换行；随后在同一次受 `writeMu` 保护的写入中追加一个 `\n`。禁止向 stdin 写入日志、诊断文本或其他非 MCP 内容。

Go JSON Encoder 会把字符串内部换行编码为 `\n` 转义，因此不会违反“消息不得包含内嵌换行”的 Transport 要求。

### 读取

stdout 按换行切分，每一行都必须解码为一个 JSON-RPC message；空行同样属于非法协议输出。最大单条消息与 HTTP 响应上限保持一致，为 16 MiB。超过上限、非法 UTF-8、非 JSON 内容、非法 JSON-RPC 版本或无法分类的消息均视为 Transport 级协议错误。EOF 前最后一条消息如果没有换行也按完整消息解码，随后再处理进程退出。

Transport 级错误会：

1. 原子进入 `failed`；
2. 使所有 pending 请求收到同一个终端错误；
3. 关闭 stdin；
4. 回收完整子进程树；
5. 拒绝后续请求。

不能跳过 stdout 中的普通日志行。MCP 标准要求 Server 不得向 stdout 输出非 MCP 内容，静默容错会掩盖不兼容 Server 并可能破坏消息边界。

## 并发请求路由

现有 Client 为每个有响应请求生成单调递增的整数 ID。stdio 的 `Send` 对带 ID 的请求执行：

1. 确保进程已启动；
2. 创建容量为 1 的结果 channel；
3. 在 `pending` 中注册 ID；
4. 原子写入消息；
5. 等待对应响应、调用方 Context、每请求 Timeout 或 Transport 终止；
6. 删除仍属于本次调用的 pending 项。

必须先注册 pending 再写入，防止 Server 极快响应时丢失消息。reader 收到 Response 后，在锁内取得并删除对应 pending 项，解锁后再发送结果。Server 可以乱序返回响应，不影响调用结果归属。

调用方取消或单次 Timeout 只移除该请求，不关闭共享进程。之后到达的迟到响应因找不到 pending ID 而被丢弃。取消不会向 Server 发送 `notifications/cancelled`；该能力不在本期范围内。

未知响应 ID 可能来自已取消请求，记录无内容的 Debug 计数即可，不视为 Transport 失败。

## Notification 与 Server-to-client 消息

`notifications/initialized` 没有 ID。`Send` 只需完成原子写入并立即返回零值 `Response`，不得等待 stdout。

stdout reader 对 Server 发来的消息按以下策略处理：

- Response：按 ID 路由给 pending 请求；
- Notification：验证为合法 JSON-RPC 后忽略；不得阻塞 reader；
- `ping` Request：返回同一 ID、空对象 result；
- 其他 Server Request：返回 JSON-RPC `-32601 Method not found`。

为响应 Server Request，需要增加只用于 stdio wire 层的 message envelope，其 ID 使用 `json.RawMessage`，从而能原样回传规范允许的整数或字符串 ID。现有出站 `Request` 和入站业务 `Response` 仍可保持整数 ID，不扩大 Client API。

此策略保证不支持 sampling、elicitation 等能力时明确失败，而不是让 Server 永久等待；它不表示本期支持这些能力。

## stderr

Server 可以向 stderr 写 UTF-8 日志。Transport 必须用固定大小的读缓冲持续排空并丢弃 stderr，避免 pipe 填满导致子进程阻塞。Transport 不保留 stderr 尾部，也不把 stderr 转发给默认日志、Metrics 或 Span，因为 Server 可能错误地打印凭据、参数或业务数据。

公开错误只包含：Server 名称、操作、进程是否异常退出、退出码或 signal，以及错误类别。不得包含完整命令行、环境、stdin/stdout 消息或 stderr 文本。未来若增加显式 Debug 内容模式，必须另行设计脱敏与部署开关。

## Timeout 与 Context

- `timeout <= 0` 使用现有 `DefaultTimeout`，即 60 秒。
- Timeout 是每次 `Send` 的期限，包括 initialize、tools/list 和 tools/call，不是 Server 进程总寿命。
- 调用方已有更早 Deadline 时，以更早 Deadline 为准。
- 首次 `initialize` 在启动或握手期间取消时，本次 Extension 注册失败，并关闭刚启动的 Transport。
- 运行期单次 tools/call 取消不终止 Server，其他并发调用可继续。
- `Close(ctx)` 尊重调用方 Deadline；强制终止子进程后仍需启动一个独立的有界 reap 等待，避免僵尸进程。

## 子进程生命周期

### 启动

1. 使用 `exec.Command` 直接构造进程，不经过 shell。
2. 设置经过校验的 cwd 和合并后的环境。
3. 创建 stdin、stdout、stderr pipe；任一 pipe 创建失败时关闭已创建资源。
4. 配置独立进程组。
5. 启动进程。
6. 启动唯一 stdout reader、stderr drainer 和 wait goroutine。
7. 由当前 `initialize` 请求继续握手。

创建失败必须回收所有已经取得的 pipe。`cmd.Start` 成功后，无论后续在哪一步失败，都必须最终调用一次 `cmd.Wait`。

### 正常关闭

1. 状态切换为 `closing`，拒绝新请求；
2. 使所有 pending 请求返回关闭错误；
3. 关闭 stdin，向遵守规范的 Server 发送 EOF；
4. 等待子进程自然退出，最长为 `min(5 秒, Close Context 剩余时间)`；
5. 未退出则强制终止完整进程组；
6. 等待 wait goroutine 完成并关闭剩余 pipe；
7. 状态切换为 `closed`。

不向 Server 发送不存在于 MCP 生命周期规范中的自定义 shutdown RPC。

### 异常退出

子进程在 Transport 关闭前退出时，Transport 进入 `failed`。所有 pending 请求收到稳定的“stdio server exited”错误，并携带退出码或 signal，但不携带 stderr。Extension 不自动重启进程，应用启动期因此 fail-fast，运行期则由当前 Run 得到工具错误；自动重连留给后续设计。

### 跨平台进程树清理

新增 MCP 专用的小型平台文件：

- Unix：启动时设置独立 process group，强制关闭时向负 PID 发送 `SIGKILL`；
- Windows：优先使用 Job Object；若本期不引入 Job Object 实现，则沿用仓库已验证的 `taskkill /PID <pid> /T /F`，失败后再 `Process.Kill`。

`pi/mcp` 不依赖 `pi/harness/tools.ProcessSupervisor`。后者面向 Agent 可调用的长期 shell 任务，会合并 stdout/stderr 并绑定 Workspace；stdio Transport 要求严格区分协议 stdout 和日志 stderr，生命周期也由 MCP Client 独占。可以复用其平台策略，但不能复用其会话模型。

## Extension 生命周期与 fail-fast

现有 Extension Runtime 在应用开始监听前执行：

```text
NewExtension
  → Register
    → Client.Initialize
    → Client.ListTools
    → 校验 allow_tools 全部存在
    → 注册 proxy tools
```

stdio 保持相同顺序。进程在 `Client.Initialize` 的第一次 Send 中启动。以下任一条件使应用启动失败：

- command 不存在或无法执行；
- cwd 不可用；
- 环境引用缺失；
- Server 在握手前退出；
- initialize 超时或协议版本不兼容；
- tools/list 失败；
- `allow_tools` 中任一工具不存在；
- stdout 违反 MCP framing 或 JSON-RPC 约束。

Extension 注册失败时，Extension Runtime 的现有回滚必须调用 `Close`，确保已经启动的 stdio 进程被回收。应用停止时继续通过 `extension.Closer` 关闭 Client 和 Transport。

## 协议版本

本次不改变 `ProtocolVersion = "2025-03-26"`。Client 继续在 initialize 中声明该版本，并只接受自身实现支持的版本；Server 返回其他版本时明确失败。

Transport 标准与 MCP Feature 版本是两个维度：增加 stdio 不等于自动支持较新协议版本中的 resources、sampling 或 elicitation。协议版本升级应单独设计并补齐能力与兼容测试。

## 错误模型

沿用 `transportError` 的“不含远端正文”原则，并为 stdio 增加稳定类别：

- `start process`；
- `write request`；
- `read response`；
- `invalid JSON-RPC message`；
- `response too large`；
- `request timeout`；
- `server exited`；
- `transport closed`；
- `close process`。

错误必须保留 Context 的 `context.Canceled` / `context.DeadlineExceeded` 解包链。进程启动错误可保留操作系统错误链，但错误格式不得包含环境或完整 argv。JSON-RPC 远端错误继续只暴露 code，不拼接可能包含敏感内容的 message/data。

## 安全边界

stdio 配置等价于授权 go-reagent 以自身操作系统身份执行指定程序。它比连接远端 HTTP MCP Server 拥有更强的本机权限，因此：

- 只读取用户显式提供的项目主配置，不自动扫描或执行第三方 MCP 配置；
- 不通过 shell 启动，避免 shell 注入；
- 不把 Server command、args、env、stderr 或协议 payload 写入默认日志；
- 不允许模型在运行期修改或新增 MCP Server 配置；
- 不把 MCP Server 自动限制为 Agent Workspace，因为本地 Server 的权限模型可能不是文件系统；部署方应使用容器、专用账户或操作系统沙箱限制高风险 Server；
- `allow_tools` 仍是必须显式声明的最小暴露面；远端新增工具不会自动进入模型上下文；
- 工具名冲突仍由 Tool Registry 拒绝，stdio 不绕过现有权限、中间件、Scheduler 或预算机制。

## 可观测性

本期不新增包含 MCP payload 的日志或 Span。现有 Client/Tool 侧观测保持不变；stdio Transport 可记录以下低基数字段：

- transport：`stdio`；
- MCP Server 配置名称；
- operation/method；
- outcome；
- duration；
- exit code（存在时）。

禁止记录 command line、cwd、env、JSON-RPC params/result 和 stderr。stdio 没有 HTTP Client Span，不伪造网络 Span；若未来增加 MCP 协议 Span，应与全局 OTel 规范单独统一，避免与 Tool Span 重复。

## 文件布局

预计新增或修改：

```text
config/
├── config.go                       # transport/command/args/env/cwd 字段
├── validate.go                     # Transport 分支、互斥、env/cwd 校验
└── config_test.go                  # 统一配置契约、安全与错误测试

pi/mcp/
├── extension.go                    # 注入 Transport，不再固定构造 HTTP
├── extension_test.go               # Transport 必填与公共扩展行为
├── protocol.go                     # stdio wire envelope / server response 类型
├── transport_stdio.go              # framing、pending 路由、状态机与生命周期
├── transport_stdio_test.go         # helper-process 集成式单元测试
├── transport_stdio_process_unix.go # Unix 进程组
└── transport_stdio_process_windows.go # Windows 进程树终止

infrastructure/driver/mcp/
├── mcp.go                          # 唯一 Transport 分支、env 解析与扩展装配
└── mcp_test.go                     # HTTP/stdio 精确装配测试

config.example.json                 # Exa 显式 http，并增加 disabled stdio 示例
README.md                           # MCP Transport 能力说明
```

如果 Windows Job Object 需要新增依赖或较多平台代码，应在实现计划中作为独立任务评估；不允许因此削弱 Unix 清理或跳过 Windows 测试编译。

## 测试策略

### 配置测试

覆盖：

- 缺少 `transport` 失败；
- 显式 HTTP 配置通过；
- stdio 最小配置通过；
- transport 未知值失败；
- HTTP 缺 URL、携带 stdio 字段失败；
- stdio 缺 command、携带 URL/Header 失败；
- env 名称非法失败；
- `${ENV}` 不存在或为空失败且错误不含其他环境值；
- literal env 原样保留；
- cwd 归一化、缺失、非目录和 NUL 失败；
- 禁用 Server 延续不校验行为；
- 错误不得泄漏测试 secret。

### StdioTransport 测试 Server

测试使用当前 Go 测试二进制的 helper-process 模式模拟 MCP Server，不依赖 Node、npx、公网或真实第三方包。通过环境变量选择脚本行为，helper 只存在于 `_test.go`。

覆盖：

- initialize、initialized notification、tools/list、tools/call 完整握手；
- JSONL 每条消息单行且 UTF-8；
- 两个及以上并发请求乱序返回，结果仍按 ID 对应；
- notification 写入后立即返回；
- Server notification 不阻塞 reader；
- Server ping 得到空 result；
- 未知 Server Request 得到 `-32601`；
- Context 取消只取消对应请求，其他请求仍成功；
- 迟到响应被安全丢弃；
- 单次 Timeout 保留 `context.DeadlineExceeded`；
- stderr 大量输出不会阻塞协议，且错误不包含 stderr secret；
- stdout 非 JSON、错误 JSON-RPC 版本、超大行导致 Transport 失败；
- Server 异常退出使全部 pending 失败；
- 并发 Send 与 Close 不死锁、不 panic、不重复 Wait；
- Close 先 EOF，Server 不退出时强制终止进程树；
- Close 幂等；
- 创建后从未 Send 的 Transport 可无副作用关闭；
- `go test -race` 下 pending、状态和写入无竞态。

### Extension 与装配测试

覆盖：

- `NewExtension` 拒绝 nil Transport；
- 基础设施驱动对 `http` 精确创建 HTTPTransport；
- 基础设施驱动对 `stdio` 精确创建 StdioTransport；
- stdio 仍只注册 `allow_tools`；
- 缺少必需工具时注册失败并关闭进程；
- 配置 env 解析进入子进程，但测试失败信息不含值；
- Fx Stop 关闭 stdio Server；
- 多个配置 Server 各自拥有独立 Client、进程和工具前缀。

### 平台与全仓验证

验收命令：

```bash
go test ./config ./pi/mcp ./infrastructure/driver/mcp
go test -race ./pi/mcp ./infrastructure/driver/mcp
go test ./...
go test -race ./...
go vet ./...
GOOS=windows go test -c -o /tmp/go-reagent-pi-mcp-windows.test.exe ./pi/mcp
git diff --check
```

交叉编译只验证 Windows 平台文件能够构建，不运行生成的 Windows 测试二进制；生命周期用例仍需在 Windows CI 或人工环境执行。

## 统一契约与仓库内迁移

本功能按全新 MCP Transport 契约一次性交付，不提供兼容期、弃用期、双读或字段推断：

- 所有已启用 MCP Server 都必须显式配置 `transport`；现有 Exa 示例和测试夹具同步增加 `"transport": "http"`。
- `NewExtension` 直接改为接收 `Transport`；仓库内调用点同步迁移，不保留旧构造函数或适配器。
- Transport 专属字段仍采用一层扁平配置，但由必填 `transport` 严格判别；不同分支字段混用直接失败。
- `Transport` 接口、Client、proxyTool、`ProtocolVersion`、工具白名单和前缀语义继续作为统一内部架构，不属于兼容承诺。
- 不引入 MCP SDK 依赖，不增加对 Node/npm 的运行时硬依赖；只有用户选择以 `npx` 作为某个 Server command 时才需要 Node。

## 验收标准

- 配置一个遵守 MCP stdio 的本地 Server 后，应用在开始提供服务前完成 initialize 和 tools/list。
- `allow_tools` 中的工具以现有 proxyTool 形式出现在 Registry，并可完成 tools/call。
- 并发 tools/call 即使乱序响应也不会串包、泄漏或死锁。
- stdout 严格按 JSONL 处理，stderr 不污染协议且不会造成 pipe 背压。
- 单次请求取消不影响其他并发请求；应用停止和 Transport 失败不会遗留子进程或僵尸进程。
- HTTP MCP 单元测试和 Exa opt-in 集成测试迁移到显式 `transport: "http"` 后保持原有运行行为。
- 配置、错误、日志和观测数据不包含环境变量值、协议参数、结果或 stderr 内容。
- Linux/macOS 原生测试、Race 测试、Vet、Windows 编译检查和 `git diff --check` 通过。

## 后续演进

完成本设计后可独立评估：

- 标准 `.mcp.json` 的显式导入命令；
- lazy、keep-alive、自动重连和工具元数据缓存；
- tools/list changed 与 Registry 原子快照替换；
- resources/prompts 的受控 Tool 映射；
- cancellation notification；
- Windows Job Object 的原生实现；
- MCP 协议版本升级；
- 与 Pi MCP Adapter 类似的单代理工具，用于超大工具目录的 Context 成本控制。

这些演进不得削弱本期确立的边界：Agent Core 只依赖 `ai.Tool`，Transport 细节留在 `pi/mcp`，本地可执行配置必须显式受信，白名单和生命周期由服务端统一控制。
