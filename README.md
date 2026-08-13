# go-reagent

`go-reagent` 是一个用 Go 编写的企业级 Agent Harness 内核，用于统一承载大模型推理、工具调用与 Agent 生命周期。

项目目前处于核心能力搭建阶段：公共协议、可测试的 ReAct Main Loop、配置驱动的 OpenAI/Anthropic 兼容适配器、真实 Tool Registry，以及最终的 `apply_patch`、`edit`、`exec`、`process`、`read` 和 `write` 六工具闭环已经就位。

## 核心流程

```text
用户消息
   │
   ▼
Agent Loop
   │
   ├── 从 Registry 获取 Tool Definition
   │
   ├── Thinking 开启时：隐藏工具，生成规划 Trace
   │
   ├── Action 阶段：恢复工具，调用 ai.Provider.Generate
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
          └─────────────────────────► 再次调用 AI Client
```

## SDK 快速开始

上层业务读取业务配置并选择当前模型平台，再通过 `pi.Register` 组装唯一的 `pi.Runner`。历史加载和消息持久化由业务负责；Runner 只同步执行一次无状态运行：

```go
cfg, err := config.Load("config.json")
if err != nil {
	return err
}

platform, err := cfg.CurrentPlatformOptions()
if err != nil {
	return err
}

var runner pi.Runner
app := fx.New(
	fx.NopLogger,
	fx.Supply(platform, pi.WorkDir(workDir)),
	pi.Register,
	fx.Populate(&runner),
)
if err := app.Start(ctx); err != nil {
	return err
}
defer app.Stop(context.Background())

result, err := runner.Run(ctx, pi.RunRequest{
	History: history,
	Input:   "Review the current workspace",
	Context: []pi.ContextBlock{
		{Name: "customer-profile", Content: customerProfile, Priority: 100},
	},
}, nil)
// 业务事务负责持久化 Input 和 result.NewMessages；err 非 nil 时，
// 如果 NewMessages 非空，业务可按自身策略保留已经完成的部分结果。
```

`RunResult.NewMessages` 只包含本次 Action/Tool Loop 新增的 Assistant/Tool 消息，不重复返回 System Prompt、外部 Context、History、Input 或内部 Thinking 脚手架。一个 `Agent` 支持并发调用；取消一个 Run 不会取消其他 Run。

`RunResult.Invocations` 按真实模型调用顺序记录所有已完成的 Thinking 和 Action 调用。每条记录包含阶段、输入/输出 Token、平台、模型、配置单价、USD 成本和耗时。一次工具循环触发多次模型调用时会产生多条独立记录；隐藏 Thinking 文本不会进入 `NewMessages`，但其成本不会漏记。默认 SDK 会强制计量每次成功响应；上游缺少 Usage、Usage 非法或成本计算不一致都会让本次 Run 以 AI generation error 失败。

`pi` 不开放 Store，也不把业务配置、会话和数据库职责带进 SDK。Agent Core 直接位于根 `pi` 包，模型协议位于 `pi/ai`；完整默认 Agent 由 `pi.Register` 组合 `pi/harness` 中的 Workspace、Skills、工具和观测实现。

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
├── pi/                        # 唯一 Agent Core 与默认 Fx 注册
│   ├── agent.go               # Agent 与 Runner
│   ├── contract.go            # Run 请求、结果与模型调用记录
│   ├── context.go             # Agent 与 Loop 共享的运行上下文
│   ├── loop.go                # 模型/工具循环
│   ├── scheduler.go           # 工具调度
│   ├── register.go            # 默认 Harness 装配
│   ├── ai/                    # 消息协议、Tool 契约与 Provider
│   │   └── providers/         # OpenAI、Anthropic 和协议工厂
│   ├── harness/               # 默认 Workspace Harness
│   │   ├── context.go         # AGENTS/Skills 运行上下文
│   │   ├── prompt.go          # System Prompt 组合
│   │   ├── errors/            # Pi 稳定错误分类
│   │   ├── observability/     # Usage、USD 成本与耗时跟踪
│   │   ├── skills/            # Skill 发现与加载
│   │   └── tools/             # 六个默认工具和进程监督器
│   └── test/                  # 根 pi 公共 API 与包边界测试
├── config/                    # 业务配置、平台列表与 Configor 加载
├── domain/                    # 业务实体与 Repository 接口
├── infrastructure/           # MySQL 驱动和持久化实现
├── application/              # CLI 应用生命周期与用例组合
├── conversation/             # 会话业务编排
├── transport/                # Terminal 与 WeCom
├── cmd/
│   ├── reagent/              # 自带 CLI 入口
│   └── ping/                 # 独立 HTTP ping 示例
├── AGENTS.md                 # 自带 CLI 的默认 Agent 定义
├── skills/                   # 自带 CLI 的默认 Skills
├── config.example.json       # 不含真实密钥的配置模板
├── migrations/               # CLI MySQL 会话迁移
└── tests/integration/        # 跨包组合与边界测试
```

公共依赖方向固定为：

```text
pi/ai <- pi/harness <- pi
pi/ai <-------------- pi
config -> pi/ai/providers
application -> config + conversation + infrastructure + transport + pi
cmd/reagent -> application
```

`pi/ai` 定义模型与 Tool 的底层协议；根 `pi` 是唯一 Agent Core；`pi/harness` 提供默认 Workspace 能力，并由 `pi/register.go` 组装。业务配置、会话存储和消息渠道不会进入 SDK `Run` 路径。详见 [SDK 架构](docs/sdk-architecture.md)。

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
      "model": "deepseek-chat",
      "pricing": {
        "input_usd_per_million_tokens": 0.15,
        "output_usd_per_million_tokens": 0.60
      }
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
go run ./cmd/reagent
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
| `pricing.input_usd_per_million_tokens` | 输入 Token 单价，单位 USD/1M tokens，必须为小于 100,000,000 的有限非负数 |
| `pricing.output_usd_per_million_tokens` | 输出 Token 单价，单位 USD/1M tokens，必须为小于 100,000,000 的有限非负数 |

`pricing` 是每个平台的必填配置，允许两个价格均为 `0` 表示免费模型。单价和计算后的单次调用成本都必须小于 100,000,000 USD，以匹配总账 `DECIMAL(20,12)` 的可表示范围。示例中的 `0.15`/`0.60` 只演示格式，不代表厂商官方或实时价格；部署时必须按实际模型账单填写并维护。

配置文件由 Configor 加载，支持环境叠加、example 回退和 Shell 环境变量覆盖：

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `CONFIG_PATH` | `config.json` | 指定其他平台配置文件 |
| `CONFIGOR_ENV` | `development`（测试时为 `test`） | 加载环境叠加文件，如 `config.production.json` |
| `CONFIGOR_CURRENTPLATFORM` | 配置文件中的值 | 覆盖当前平台选择 |
| `AGENT_PROMPT` | 新建 `ping.go` 并完成 Git 提交 | 覆盖启动测试任务 |

例如使用另一份配置：

```bash
CONFIG_PATH=/secure/reagent/config.json go run ./cmd/reagent
```

例如加载 YAML 基础配置及其 `production` 叠加文件：

```bash
CONFIG_PATH=/secure/reagent/config.yaml CONFIGOR_ENV=production go run ./cmd/reagent
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

SDK 与 CLI 复用 `pi.Register` 提供的 Fx 图：

```text
Options -> ai.Provider -> CostTracker -> pi.Loop / Registry / Runner
WorkDir        -> Workspace / AGENTS / Skills / 默认工具
```

调用方在自己的 Fx App 中组合 `pi.Register`，并由 Fx 生命周期统一启动和停止资源。自带 CLI 在同一个 Fx App
中组合 `pi.Register` 与业务模块，不会嵌套创建第二个 App。CLI 的 AgentRunner
在 `OnStart` 中异步执行一次任务；收到 SIGINT/SIGTERM 时，`OnStop` 会先取消并等待运行，再按 Fx
逆序生命周期关闭后台进程管理器。

当前入口开启慢思考模式，挂载六个真实本地工具，默认要求模型新建 `ping.go`、验证代码并完成 Git 提交。

### 日志输出

运行日志通过 `go-logger-sdk` 以 JSON 写入 stdout，每条日志都包含
`module=go-reagent`，并通过 `component`、`turn`、`phase`、`tool` 和
`tool_call_id` 等结构化字段标识来源与执行上下文。平台启动日志只记录平台 ID、协议和模型，
不会记录 API Key、Authorization Header 或完整平台配置。

模型的内部思考 Trace 和最终回复仍以纯文本输出，因此直接运行命令时会看到 JSON 运行日志与
模型文本结果共存；接入日志平台时应按 JSON 行采集运行日志。

每次成功模型调用都会输出包含平台、模型、输入/输出 Token、USD/1M tokens 单价、`cost_usd` 和 `latency_ms` 的结构化计量日志。上游缺少或返回非法 Usage 时会输出 `usage_missing`/`usage_invalid` 并终止 Run，不会估算或伪造零成本；Provider 调用失败只记录失败耗时，不生成成本记录。日志不包含消息正文、工具参数、API Key 或完整 Provider 请求。

启用 MySQL 会话持久化后，形成可持久化 turn 的运行会把每次已完成的 Thinking/Action 调用各自写入 `agent_model_invocations`。该表是 Token、成本和耗时统计的唯一权威来源；不要再叠加可见 Assistant 消息 JSON 中的 Usage。隐藏 Thinking 文本和完整 Provider 请求不会持久化。部署顺序和聚合 SQL 见 [MySQL 会话持久化](docs/conversation-persistence.md)。

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
- 公共 `pi/ai`、根 `pi` Agent Core 和 `pi/harness` 默认能力，以及同步、无状态、并发安全的默认 SDK。
- 模型无关的消息、Tool Call、Tool Update、Tool Result、Agent Event 和带调度元数据的 Tool Definition 数据结构。
- 使用 `json.RawMessage` 保留工具调用参数，并由具体工具延迟解析。
- `ai.Provider` 每轮接收上下文和可用工具定义；`pi.Register` 使用业务选择后的单个 `providers.Options` 创建官方 SDK 适配器。
- Configor 驱动的 JSON/YAML/TOML 平台配置加载，以及项目级规范化与启动前校验。
- 配置驱动的 OpenAI Chat Completions 兼容 Provider。
- 配置驱动的 Anthropic Messages 兼容 Provider。
- 通过 `currentPlatform` 一键切换 DeepSeek、智谱或其他兼容服务。
- 基于 `go-logger-sdk` 的 JSON 运行日志，以及 Bootstrap、Engine 和 Registry 的结构化上下文字段。
- 按平台配置 USD/1M tokens 单价，逐次追踪每个 Thinking/Action 模型调用的 Token、成本和耗时。
- 线程安全、稳定排序、拒绝重复注册的真实 Tool Registry。
- 工具 error、Context 取消和 panic 的统一错误隔离。
- 受 WorkDir 能力边界保护的 `read`、`write`、`edit` 和 `apply_patch` 文件工具。
- 支持超时、后台化、有界输出和进程组终止的 `exec` 与 `process` 工具。
- 由 `ContextBuilder`、`Loop`、`Scheduler` 和 `pi.Agent` 分层驱动的运行时。
- 可选的 Thinking Phase：暂时隐藏工具，将规划 Trace 注入 Action 上下文。
- 支持直接模型响应的 ReAct Main Loop。
- 支持将连续安全 Tool Call 有界并发执行，以独占工具为屏障，并稳定聚合结果。
- 通过 Reporter 广播统一 Agent Event；增量更新只到 Terminal，不进入模型历史或 WeCom。
- 支持配置化企业微信群机器人 Webhook，将工具开始、失败和最终回复发送为 Markdown 通知。
- 基于 Uber Fx 的私有默认图、SDK `Close` 生命周期，以及 CLI 一次性 Runner 的取消和退出码处理。
- 模型生成错误和空响应防护，并保留官方 SDK 错误解包链。
- 工具调用 ID 的整批前置校验。
- 取消信号的 Provider 与工具执行前置检查。
- 可直接运行的真实 AI Client、Registry 与文件工具示例。

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
- [x] 发布 `ai -> agent -> reagent` 公共 SDK 包结构。
- [x] 在自带 CLI 中增加可选 MySQL 会话持久化。
- [ ] 增加飞书等外部消息渠道适配。
- [x] 增加企业微信群机器人单向生命周期通知。
- [ ] 增加企业微信和飞书的双向消息接入。
