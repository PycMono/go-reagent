# go-reagent

`go-reagent` 是一个用 Go 编写的企业级 Agent Harness 内核，用于统一承载大模型推理、工具调用与 Agent 生命周期。

项目目前处于核心能力搭建阶段：公共协议、可测试的 ReAct Main Loop、配置驱动的 OpenAI/Anthropic 兼容适配器、真实 Tool Registry，以及最终的 `apply_patch`、`edit`、`exec`、`process`、`read` 和 `write` 六工具闭环已经就位。

## 核心流程

```text
用户消息
   │
   ▼
Engine Main Loop
   │
   ├── 从 Registry 获取 Tool Definition
   │
   ├── Thinking 开启时：隐藏工具，生成规划 Trace
   │
   ├── Action 阶段：恢复工具，调用 LLMProvider.Generate
   │
   ├── 没有 Tool Call ──────────────► 输出最终响应并结束运行
   │
   └── 存在 Tool Call
          │
          ▼
       按 ParallelSafe 分波
          │
          ├── 安全工具：最多 4 个并发执行
          └── 独占/未知工具：作为前后波次屏障
          │
          ▼
       按原 Tool Call 顺序聚合结果
          │
          └─────────────────────────► 再次调用 Provider
```

## 结构化运行协议

当前 Runtime 以一次 `Run` 为状态边界。调用方传入历史消息、当前用户输入、外部上下文和不透明业务元数据；框架只在本次运行期间维护有效消息与工具循环状态，并返回新产生的 Assistant/Tool 消息：

```go
result, err := runtime.Run(ctx, schema.RunRequest{
	RunID:   "run-001",
	History: history,
	Input: schema.Message{
		Role:    schema.RoleUser,
		Content: []schema.ContentBlock{schema.TextBlock("这个订单为什么还没发货？")},
	},
	Context: []schema.ContextBlock{
		{Name: "customer-profile", Content: customerProfile, Priority: 100},
	},
	Metadata: map[string]string{
		"conversationId": conversationID,
	},
}, reporter)
```

`RunResult.NewMessages` 只包含本次 Action/Tool Loop 新增的消息，不重复返回 System Prompt、外部上下文、历史、当前用户输入或内部 Thinking 脚手架。运行中途失败时，`RunResult` 仍会携带失败前已经完成的消息增量，业务方可以自行决定是否持久化。

Runtime 不持有跨 `Run` 的 Conversation、Session、用户长期记忆或业务数据库状态。下一次运行所需的历史和外部上下文必须由调用方重新传入。当前协议仍位于 `internal` 包；公共 SDK 包迁移将在后续单独完成。

## Agent Workspace 契约

每个 Agent 都必须绑定一个 Workspace。Workspace 不是 Coding 项目的同义词，而是该 Agent 的身份、能力说明和可读取资源所在的受限空间；客服、售后、数字员工训练等 Agent 同样使用这套契约：

```text
workspace/
├── AGENTS.md                  # 必需：Agent 身份、职责和行为边界
├── skills/                    # 三种 Skill 根目录至少存在一种
│   └── <skill>/SKILL.md       # 必需：至少一个格式有效且环境匹配的 Skill
└── resources/                 # 可选：业务方准备的知识或其他只读资源
```

Skill 也可以放在 `.agents/skills/` 或 `.claw/skills/`。每次 `Run` 开始时，Runtime 都会重新读取 `AGENTS.md` 并发现当前可用 Skills；缺少或不安全的 `AGENTS.md`、没有有效 Skill，或者 Registry 没有注册受限 `read`，都会在首次 Provider 调用前直接返回错误。

`read` 是所有 Agent 的基础工具，用于按需完整读取选中的 `SKILL.md` 及 Workspace 资源。`write`、`edit`、`apply_patch`、`exec` 和 `process` 只适用于确实需要这些能力的 Agent，可以不注册。SDK 核心不会根据业务类型动态挑选工具，也不会内置客服、订单等业务 Tool；业务封装决定固定注册哪些领域工具。

模型上下文按固定顺序组装：

```text
通用 Runtime 纪律
+ AGENTS.md
+ Skills Catalog
+ 外部 Context Blocks
+ History
+ 当前 Input
```

仓库根目录的 `AGENTS.md` 和 `skills/repository-development/SKILL.md` 仅服务于自带 CLI 示例。业务系统应提供自己的 Workspace，例如把客服身份写入 `AGENTS.md`，把售后流程、训练规范等分别做成领域 Skill，而无需修改 Runtime 核心。

## 项目布局

```text
go-reagent/
├── AGENTS.md                  # 仓库 CLI 的默认 Agent 定义
├── skills/                    # 仓库 CLI 的默认 Skills
├── config.example.json          # 不含真实密钥的平台配置模板
├── config.json                  # 本地平台配置（已被 Git 忽略）
├── cmd/
│   ├── main.go                  # 初始化日志并运行 Fx App
│   ├── main_test.go             # 应用日志测试
│   └── ping/main.go             # 独立 HTTP ping 示例
├── internal/
│   ├── register.go          # 聚合各包 Register 的根 Fx 入口
│   ├── app/                 # 一次性进程生命周期
│   │   ├── register.go      # AgentRunner 与生命周期注册
│   │   ├── runner.go        # AgentRunner 和 Fx 启停钩子
│   │   └── *_test.go        # 取消、退出码与生命周期测试
│   ├── config/              # 启动配置层
│   │   ├── config.go        # 配置结构、加载与当前平台选择
│   │   ├── register.go      # Config、WorkDir 与 Prompt 注册
│   │   ├── validate.go      # 配置规范化与数据校验
│   │   └── config_test.go   # 配置解析与错误处理测试
│   ├── context/             # 工作区 Prompt 与 Skill 上下文
│   │   └── register.go      # PromptComposer 与 SkillLoader 注册
│   ├── dispatch/            # 外部消息渠道输出适配层
│   │   ├── register.go      # Terminal/WeCom Reporter 注册
│   │   ├── wecom.go         # 企业微信群机器人 Reporter
│   │   └── wecom_test.go    # Webhook 协议、长度与并发测试
│   ├── engine/              # 核心引擎层
│   │   ├── engine.go        # AgentRuntime、AgentLoop 与调度构造
│   │   ├── register.go      # Engine Agent 注册
│   │   ├── run_*.go         # ReAct Main Loop、校验、执行与诊断
│   │   ├── reporter.go      # Agent 生命周期 Reporter 与广播实现
│   │   ├── terminal_reporter.go # 终端输出 Reporter
│   │   └── loop_test.go     # 生命周期、并发上限、屏障与取消测试
│   ├── provider/            # 模型适配层
│   │   ├── interface.go     # LLM Provider 接口
│   │   ├── register.go      # 当前平台 Provider 注册
│   │   ├── factory.go       # 根据协议创建 Provider
│   │   ├── openai.go        # OpenAI Chat Completions 兼容适配器
│   │   └── claude.go        # Anthropic Messages 兼容适配器
│   ├── schema/              # 公共数据结构
│   │   └── message.go       # 消息、角色与工具调用类型
│   └── tools/               # 工具与执行层
│       ├── register.go      # 六个工具、Registry 与资源生命周期注册
│       ├── registry.go      # 线程安全的工具注册、发现与执行
│       ├── registry_test.go # Registry 生命周期与错误隔离测试
│       ├── read.go           # WorkDir 内受限文本文件读取工具
│       ├── read_test.go      # 路径与输出预算安全测试
│       ├── edit.go           # WorkDir 内受限的批量唯一匹配编辑工具
│       ├── write.go          # 创建或完整覆盖 UTF-8 文本文件
│       ├── apply_patch*.go   # 结构化多文件补丁解析与应用
│       ├── exec.go           # 有界输出的 shell 命令执行工具
│       └── process*.go       # 后台进程会话与进程组管理
├── tests/
│   └── integration/          # 跨包协作、生命周期和组合根集成测试
├── go.mod
└── README.md
```

每个业务包通过自己的 `Register` 声明 Fx 对象，根 `internal.Register` 只负责统一聚合：

```text
cmd ──► internal.Register
          ├── config / context / provider / tools / dispatch / engine
          └── app（AgentRunner 与 Fx 生命周期）

engine ──► provider ──► schema
   ├─────► context
   └─────► tools ─────► schema
```

一次运行内部按以下边界协作：

```text
RunContextFactory -> AgentLoop -> LLMProvider
                         |
                         v
                   ToolScheduler -> Registry -> Middleware -> Tool
                                                   |
ToolUpdate -> ToolEvent -> AgentEvent -> MultiReporter -> Terminal / WeCom
```

- `schema` 不依赖其他业务包，统一各层交换的数据结构。
- `config` 通过 Configor 加载 JSON、YAML 或 TOML 平台配置，并返回 `currentPlatform` 选中的完整配置。
- `provider` 通过 `LLMProvider.Generate` 隔离协议调用细节，通过工厂选择 OpenAI 或 Anthropic 适配器。
- `tools` 通过 `Tool`、`Registry` 和有序 `Middleware` 分离工具实现、发现、执行与横切策略。
- `engine` 通过 `AgentRuntime` 编排运行上下文、模型推理、工具调度和事件路由，不关心具体厂商和工具实现。
- 各包的 `register.go` 构造本包运行时对象；`schema` 只有纯数据结构，因此不依赖 Fx。
- `app` 只注册 AgentRunner 和进程启停钩子；工具资源生命周期由 `tools.Register` 管理。
- `cmd` 只初始化项目日志并运行 Fx App，不承载依赖组装和业务逻辑。

## 环境要求

- Go 1.26 或更高版本

项目使用 OpenAI Go v3、Anthropic Go SDK、`go-logger-sdk` v1.0.5 和 Uber Fx v1.23.0。

## 快速开始

复制本地配置模板并限制文件权限。默认示例使用 JSON，也可以通过 `CONFIG_PATH` 指定 YAML 或 TOML：

```bash
cp config.example.json config.json
chmod 600 config.json
```

在 `config.json` 中填写各平台的 `apiKey`。默认选择 DeepSeek：

```json
{
  "currentPlatform": "deepseek",
  "platforms": [
    {
      "id": "deepseek",
      "protocol": "openai",
      "baseURL": "https://api.deepseek.com/v1/",
      "apiKey": "填写你的 DeepSeek API Key",
      "model": "deepseek-chat"
    }
  ],
  "bot": {
    "wecom": {
      "webhookURL": ""
    }
  }
}
```

然后启动：

```bash
go run ./cmd
```

切换到其他已配置平台时只需修改 `currentPlatform`：

```json
"currentPlatform": "zhipu-claude"
```

平台字段：

| 字段 | 说明 |
| --- | --- |
| `id` | 平台实例的唯一标识，供 `currentPlatform` 引用 |
| `protocol` | `openai` 或 `anthropic` |
| `baseURL` | 兼容 API 的基础地址 |
| `apiKey` | 与该平台和模型配套的密钥 |
| `model` | 该平台实例使用的模型 ID |

配置文件由 Configor 加载，支持环境叠加、example 回退和 Shell 环境变量覆盖：

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `CONFIG_PATH` | `config.json` | 指定其他平台配置文件 |
| `CONFIGOR_ENV` | `development`（测试时为 `test`） | 加载环境叠加文件，如 `config.production.json` |
| `CONFIGOR_CURRENTPLATFORM` | 配置文件中的值 | 覆盖当前平台选择 |
| `AGENT_PROMPT` | 新建 `ping.go` 并完成 Git 提交 | 覆盖启动测试任务 |

例如使用另一份配置：

```bash
CONFIG_PATH=/secure/reagent/config.json go run ./cmd
```

例如加载 YAML 基础配置及其 `production` 叠加文件：

```bash
CONFIG_PATH=/secure/reagent/config.yaml CONFIGOR_ENV=production go run ./cmd
```

Configor 会先加载基础文件，再加载同目录下的环境文件，例如 `config.production.yaml`。如果基础文件和环境文件都不存在，则尝试同扩展名的 example 文件，例如 `config.example.yaml`。字段也可以通过 `CONFIGOR_` 前缀的环境变量覆盖；数组内字段按结构路径命名，例如 `CONFIGOR_PLATFORMS_0_APIKEY`。

`config.json` 已加入 `.gitignore`，不要把真实 API Key 写入 `config.example.json`。

### 企业微信群通知

在企业微信群中创建群机器人后，将机器人 Webhook 地址写入本地 `config.json`：

```json
{
  "bot": {
    "wecom": {
      "webhookURL": "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=填写本地机器人Key"
    }
  }
}
```

`webhookURL` 为空时只输出到终端；配置后，Terminal 和企业微信群 Reporter 同时启用。运行时事件
统一为 `thinking`、`tool_start`、`tool_update`、`tool_end` 和 `message`；消息、更新与结果的 Content
当前只允许文本块。Terminal 实时显示 `exec` 的 stdout/stderr `tool_update`，同时显示工具开始、最终
状态和模型回复。WeCom 主动过滤 Thinking、所有 update、成功结果和非 Assistant 消息，只发送工具
开始、工具失败和最终 Assistant 回复，避免流式输出刷屏。单条 Markdown 最多 4096 字节，超长内容
会在合法 UTF-8 边界截断。

当前能力仅为单向群通知，不接收群消息，也不需要配置企业微信回调、Token 或 EncodingAESKey。
Webhook 地址等同于发送凭证，只能保存在已被 Git 忽略的本地配置中，不能写入示例配置、日志或提交。

### Fx 启动与退出

启动依赖链由根 `internal.Register` 聚合各包注册项后统一提供：

```text
Config -> WorkDir -> LLMProvider / RunContextFactory / Registry / Reporter -> AgentRuntime -> AgentRunner
```

AgentRunner 在 Fx `OnStart` 中异步执行一次任务，避免模型请求阻塞启动钩子。任务完成后主动关闭 Fx：
成功退出码为 0，Agent 错误退出码为 1。收到 SIGINT/SIGTERM 时，`OnStop` 会先取消并等待 Agent，
随后按 Fx 逆序生命周期关闭文件工具和后台进程管理器，避免运行中的 Tool 使用已释放资源。

当前入口开启慢思考模式，挂载六个真实本地工具，默认要求模型新建 `ping.go`、验证代码并完成 Git 提交。

### 日志输出

运行日志通过 `go-logger-sdk` 以 JSON 写入 stdout，每条日志都包含
`module=go-reagent`，并通过 `component`、`turn`、`phase`、`tool` 和
`tool_call_id` 等结构化字段标识来源与执行上下文。平台启动日志只记录平台 ID、协议和模型，
不会记录 API Key、Authorization Header 或完整平台配置。

模型的内部思考 Trace 和最终回复仍以纯文本输出，因此直接运行命令时会看到 JSON 运行日志与
模型文本结果共存；接入日志平台时应按 JSON 行采集运行日志。

`go-logger-sdk` v1.0.5 的 `caller` 字段目前指向 SDK 内部方法，而不是实际业务调用位置。
排查日志时应以 `component`、`turn`、`phase`、`tool` 和 `tool_call_id` 等结构化字段为准。

### 工具并发调度

- `ToolDefinition.ParallelSafe` 默认是 `false`，未声明和未知工具按独占方式执行。
- 当前只有只读的 `read` 标记为并发安全；其余五个工具保持独占执行。
- 连续的安全工具组成一个波次，默认最多同时执行 4 个；`MaxParallelTools <= 0` 时退化为串行。
- 独占工具会等待前一安全波完成，并阻止后一波提前启动。
- Observation 始终按模型原始 Tool Call 顺序回传，与工具实际完成顺序无关。
- 同一安全波中的调用必须语义独立；需要前序结果的操作仍应拆成多个模型轮次。

### 最终工具协议

Registry 只注册下列六个名称；参数使用驼峰字段，不提供旧名称或旧字段别名：

| 工具 | 参数字段 |
| --- | --- |
| `apply_patch` | `input` |
| `edit` | `path`、非空 `edits[]`；每项为 `oldText`、`newText` |
| `exec` | `command`，可选 `workdir`、`env`、`yieldMs`、`background`、`timeout` |
| `process` | `action`，按动作使用 `sessionId`、`timeout`、`offset`、`limit`、`data`、`eof` |
| `read` | `path`，可选 `offset`、`limit` |
| `write` | `path`、`content` |

### read 安全边界

- 只接受相对于当前 WorkDir 的路径。
- 使用 Go 1.26 `os.Root` 阻止绝对路径、`..` 穿越和外部符号链接逃逸。
- 允许指向 WorkDir 内部文件的符号链接。
- 只读取普通 UTF-8 文本文件，拒绝目录、设备和疑似二进制内容。
- 单页最多返回 2000 行且最终输出不超过 50 KiB；响应提供 `offset` 续读标记。

### edit 安全边界

- 只修改 WorkDir 内已经存在的普通 UTF-8 文本文件，拒绝 NUL 内容、路径穿越和外部符号链接。
- `edits[]` 中每个 `oldText` 都基于原始文件独立做唯一匹配，匹配范围不得重叠或嵌套。
- 全部编辑预检成功后一次写回；`newText` 可以是空字符串以删除片段，但字段不能缺失。
- 替换保留未修改内容、原换行风格和文件权限，并在 Details 返回 diff、patch、替换数和首个修改行。
- 工具未标记为并发安全，调度器会把每次编辑作为独占屏障执行。

### write 与 apply_patch

- `write` 创建或完整覆盖 WorkDir 内的 UTF-8 文本文件，并自动创建父目录；相同内容不会重复写入。
- `apply_patch` 接受 OpenClaw 风格的 `*** Begin Patch` 结构化补丁，支持 Add、Update、Delete 和 Move。
- 补丁先在内存中完成全部路径、冲突和上下文预检，再写入磁盘，避免语法错误造成部分修改。
- 两个工具都拒绝绝对路径、`..` 逃逸、外部符号链接、NUL 和非 UTF-8 文本。

### exec 与 process

- `exec.timeout` 的单位是秒，默认 120、最大 600；`exec.yieldMs` 的单位是毫秒，默认 10000、最大 30000。
- `background=true` 会立即返回 session；前台命令超过 `yieldMs` 也会转为后台 session。
- `process.action` 恰好支持七种动作：`list`、`poll`、`log`、`write`、`kill`、`clear`、`remove`。
- `process.poll.timeout` 的单位是毫秒且最大 30000；`log` 使用 `offset/limit` 分页，`write` 使用 `data/eof` 写入 stdin。
- `kill` 保留最终记录，`clear` 只清理已结束记录，`remove` 会终止并删除指定 session；Registry 关闭时终止全部运行中的进程组并清空记录。
- 每个 session 只保留最后 50 KiB 合并输出，避免长命令无限占用内存。
- WorkDir 只限制 exec 的启动目录，不是文件系统沙箱。命令继承 go-reagent 进程的宿主权限，仍可主动访问工作区外文件和网络。
- 人工审批、命令 allowlist、PTY 和容器/Seatbelt 沙箱尚未实现，不应把 `exec` 暴露给不受信任的调用方。

运行全部测试：

```bash
go test ./...
```

## 当前能力

- 统一的 `system`、`user`、`assistant` 和 `tool` 消息角色；最终工具观察通过原生 `tool` 消息和 `tool_call_id` 关联。
- Provider 无关的消息、Tool Call、Tool Update、Tool Result、Agent Event 和带调度元数据的 Tool Definition 数据结构。
- 使用 `json.RawMessage` 保留工具调用参数，并由具体工具延迟解析。
- 可替换的 `LLMProvider` 接口，每轮接收上下文和可用工具定义。
- Configor 驱动的 JSON/YAML/TOML 平台配置加载，以及项目级规范化与启动前校验。
- 配置驱动的 OpenAI Chat Completions 兼容 Provider。
- 配置驱动的 Anthropic Messages 兼容 Provider。
- 通过 `currentPlatform` 一键切换 DeepSeek、智谱或其他兼容服务。
- 基于 `go-logger-sdk` 的 JSON 运行日志，以及 Bootstrap、Engine 和 Registry 的结构化上下文字段。
- 线程安全、稳定排序、拒绝重复注册的真实 Tool Registry。
- 工具 error、Context 取消和 panic 的统一错误隔离。
- 受 WorkDir 能力边界保护的 `read`、`write`、`edit` 和 `apply_patch` 文件工具。
- 支持超时、后台化、有界输出和进程组终止的 `exec` 与 `process` 工具。
- 由 `RunContextFactory`、`AgentLoop`、`ToolScheduler` 和 `AgentRuntime` 分层驱动的运行时。
- 可选的 Thinking Phase：暂时隐藏工具，将规划 Trace 注入 Action 上下文。
- 支持直接模型响应的 ReAct Main Loop。
- 支持将连续安全 Tool Call 有界并发执行，以独占工具为屏障，并稳定聚合结果。
- 通过 Reporter 广播统一 Agent Event；增量更新只到 Terminal，不进入模型历史或 WeCom。
- 支持配置化企业微信群机器人 Webhook，将工具开始、失败和最终回复发送为 Markdown 通知。
- 基于 Uber Fx 的完整启动依赖注入，以及一次性 Runner 的取消、退出码和资源关闭生命周期。
- Provider 错误和空响应防护。
- 工具调用 ID 的整批前置校验。
- 取消信号的 Provider 与工具执行前置检查。
- 可直接运行的真实 Provider、Registry 与文件工具示例。

## 开发路线图

- [x] 定义公共消息与工具调用结构。
- [x] 定义 LLM Provider 接口。
- [x] 定义工具发现和执行 Registry 接口。
- [x] 实现并测试最小 Agent Runtime ReAct Main Loop。
- [x] 增加可开关的慢思考与行动双阶段循环。
- [x] 实现内存版 Tool Registry 和本地编码工具闭环。
- [x] 实现配置驱动的 OpenAI/Anthropic 兼容 Provider。
- [x] 使用 Uber Fx 组装真实 Config、Provider、Registry、Reporter、Engine 和 Runner。
- [x] 增加按工具安全等级分波的有界并发调度器。
- [ ] 增加可配置的轮次、Token、时间与成本预算。
- [x] 增加支持多格式、环境叠加和字段覆盖的平台启动配置。
- [ ] 增加上下文管理和持久化记忆。
- [ ] 增加飞书等外部消息渠道适配。
- [x] 增加企业微信群机器人单向生命周期通知。
- [ ] 增加企业微信和飞书的双向消息接入。
