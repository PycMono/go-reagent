# go-reagent SDK 架构

## 包边界

go-reagent 采用 Pi 风格的 Core/Harness 分层：根 `pi` 是唯一 Agent Core，`pi/harness` 提供默认工作区能力，`pi/register.go` 负责最终装配。

```text
pi/ai <- pi/harness <- pi
pi/ai <-------------- pi
config -> pi/ai/providers
application -> config + conversation + infrastructure + transport + pi
cmd/reagent -> application
```

- `pi/ai`：公共消息、Usage、内容块、工具定义和统一 `Provider`。
- `pi/ai/providers`：Provider 配置，以及 OpenAI/Anthropic 官方 SDK 适配器。
- `pi`：唯一 Agent Core，包含公共 Run 契约、Agent、Loop、Scheduler、Registry、Middleware、Reporter 和事件，并通过 `register.go` 组装默认 Harness。
- `pi/harness`：AGENTS/Skills 上下文、System Prompt、默认工具、错误分类和成本观测。
- `pi/test`：根 `pi` 的集中式公开 API、运行循环和包边界测试；各 Harness 子包的白盒测试仍与实现放在同一包。
- `config`：业务配置、多个模型平台、当前平台选择和 Configor 加载。
- `application`：CLI 生命周期与业务组件组合。

`pi/ai` 不依赖根 `pi` 或业务包；`pi/harness` 只依赖 `pi/ai` 和自己的子包，不反向依赖根 `pi`。根 `pi` 不依赖 `config`、`application`、数据库或 Transport。

## Pi SDK 契约

上层业务先从业务配置中选择当前平台，再把 `pi.Register` 组合进自己的 Fx App：

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
	Input:   inputText,
	Context: contextBlocks,
}, nil)
```

公共运行契约只有一套：

```go
type Runner interface {
	Run(context.Context, RunRequest, Reporter) (RunResult, error)
}

func New(*harness.ContextBuilder, *Loop, Registry) *Agent
```

`pi.Agent` 是唯一 Agent 类型。直接组合底层组件时调用 `pi.New`；`Agent` 直接使用具体的 `harness.ContextBuilder` 准备每次运行的上下文，不再通过只有一个实现的 Factory 转发。使用默认 Provider、工具、Workspace 和 Loop 时，把 `pi.Register` 加入 Fx 图。不再存在 `pi/agent` 子包或第二套 SDK 门面。

`pi.RunResult` 直接定义在根包；除 `NewMessages` 外，`Invocations` 会按调用顺序返回本次运行所有已完成的 Thinking/Action 模型调用及其 Usage、成本与耗时。

## 配置

业务通过 `config.Load(path)` 加载配置；Pi SDK 不猜测配置路径，也不读取 `CONFIG_PATH`。加载继续只使用 Configor，因此 JSON、YAML、TOML、example 回退、环境叠加和 `CONFIGOR_` 环境变量覆盖保持一致。

多 Provider 列表和 `currentPlatform` 选择只属于 `config.Config`。`pi.Register` 消费选中的单个 `providers.Options`，Provider 工厂在构造前规范化和校验必需字段。

每个平台都必须配置 `pricing.input_usd_per_million_tokens` 和 `pricing.output_usd_per_million_tokens`，单位为 USD/1M tokens。价格必须是小于 100,000,000 的有限非负数，允许 `0` 表示免费模型；计算后的单次成本采用相同上界，以匹配 `DECIMAL(20,12)` 总账。价格是构造 SDK 时的快照。

## Run 数据流

```text
业务加载 History
    -> Agent.Run
    -> 校验调用方数据
    -> 重新读取 AGENTS.md 和发现 Skills
    -> 组装 System + Context + History + Input
    -> AI / Tool Loop
    -> 返回 RunResult.NewMessages + RunResult.Invocations
    -> 业务按自己的事务策略持久化
```

`Run` 是同步且无状态的。SDK 不查询会话、不保存消息、不管理 RunID、UserID 或 ConversationID，也不做会话锁、重试、排队或摘要。`Input` 只接收用户文本；完整的 `ai.Message` 仅用于需要表达 Assistant、Tool 和 Tool Call 的 `History` 与 `NewMessages`。

`NewMessages` 只包含当前 Run 新增的 Assistant/Tool 消息，不包含 System、外部 Context、History、Input 或 Thinking 脚手架。运行中途失败时，已经完成的消息仍与错误一起返回；是否持久化部分结果由业务决定。

默认 SDK 在 Provider 和 Loop 之间强制执行成本计量：每个被接受的 Thinking 或 Action 响应都必须有合法 Usage、按配置价格计算的准确成本，并对应一个有序 Invocation。工具循环中的重复 Action 调用也逐次计量。缺失、负数、NaN、无穷值或成本公式不一致都会返回 `pi/harness/errors.ErrGeneration`，不会把未计量响应作为成功结果。自行直接组合根 `pi` 包时，调用方必须提供能返回完整计量 Usage 的 `ai.Provider`；`pi.Loop` 会独立复核这些字段。

`pi.Runner.Run` 通过最后一个参数接收 Reporter；不需要进度事件时传 `nil`。仓库 CLI 注入 Terminal/WeCom Reporter。

## Workspace

`New` 从进程当前工作目录绑定 Workspace。每次 Run 都会重新读取该 Workspace 中的 `AGENTS.md` 并发现 `.agents/skills`、`.claw/skills` 或 `skills`，因此文件内容更新可以在下一次 Run 生效；构造 Agent 后再切换进程工作目录不会改变已绑定的 Workspace。

业务系统应在自己的 Workspace 提供领域专属 AGENTS.md 和 Skills。仓库根目录中的文件只定义自带 CLI 的默认 Agent。

## 并发与取消

一个长生命周期 `*pi.Agent` 可以并发执行多个 Run：

- 每个 Run 在本地维护需要追加、排序的顶层消息和工具切片，不把运行状态保存在 `Agent` 上；
- 与 Pi agent-core 一样，SDK 不递归复制 History 中的 Message 和工具 JSON 参数；调用方在 Run 结束前不得并发修改这些值；
- 取消或超时一个 Run 不会影响其他 Run；
- SDK 不按 ConversationID 串行化，并发控制仍由业务负责。

## 错误与部分结果

`pi/harness/errors.ErrorCodeOf(err)` 返回稳定字符串枚举：

| ErrorCode | 值 |
| --- | --- |
| `ErrorCodeUnknown` | `unknown` |
| `ErrorCodeConfigLoad` | `config_load_failed` |
| `ErrorCodeConfigInvalid` | `config_invalid` |
| `ErrorCodeInitialization` | `initialization_failed` |
| `ErrorCodeRequestInvalid` | `request_invalid` |
| `ErrorCodeWorkspaceInvalid` | `workspace_invalid` |
| `ErrorCodeAIGeneration` | `ai_generation_failed` |
| `ErrorCodeToolRuntime` | `tool_runtime_failed` |
| `ErrorCodeCanceled` | `canceled` |
| `ErrorCodeDeadlineExceeded` | `deadline_exceeded` |
| `ErrorCodeClosed` | `agent_closed` |
| `ErrorCodeInternal` | `internal` |

`pi/harness/errors.Error` 通过 `Unwrap` 保留原始错误。`errors.Is` 可继续识别 `context.Canceled`、`context.DeadlineExceeded`、`pi/harness/errors.ErrClosed` 和 `pi/harness/errors.ErrGeneration`；`errors.As` 可以获得 OpenAI 或 Anthropic 官方 SDK 错误。Pi SDK 不把厂商状态码扩展成新的公共 ErrorCode。

## 生命周期

`pi.Agent` 不拥有 Fx App，也不定义第二套 Close 协议。Provider、Workspace 和后台进程等资源全部由调用方 Fx App 的 `Start`/`Stop` 生命周期管理；取消某个 Run 只影响该次调用。

## CLI 与会话存储

自带 CLI 通过 `application.Register` 组合 `pi`、基础设施、Conversation 业务和 Transport：

```text
Find Conversation -> List Messages -> pi.Run -> AppendTurn
```

CLI 可以通过 MySQL 加载有界 History，并在同一事务中保存 Input、`NewMessages` 与 `Invocations`；Terminal 和 WeCom 通过 Reporter 接收事件。这些业务适配不属于 `pi` SDK 的状态或扩展接口。隐藏 Thinking 文本和完整 Provider 请求没有持久化路径。
