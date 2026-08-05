# go-reagent SDK 架构

## 包边界

go-reagent 采用 Pi 风格的分层思想：模型协议、Agent 运行机制和完整默认产品逐层组合，但不照搬 TypeScript 目录名称。

```text
ai <- agent <- reagent
              |-- internal/bootstrap
              |-- internal/workspace
              `-- internal/tools

internal/cli -> reagent/agent + internal/bootstrap
cmd/reagent  -> internal/cli + internal/bootstrap
```

- `ai`：公共消息、Usage、平台定价、内容块、工具定义、协议枚举、统一 `Client`，以及 OpenAI/Anthropic 官方 SDK 适配器。
- `agent`：公共 Run 契约、Loop、Scheduler、Registry、Middleware、Tool、Reporter 和底层事件。
- 根 `reagent`：面向上层业务的完整默认 SDK，负责配置、构造、同步 Run、错误分类和 Close。
- `internal/workspace`：当前工作目录、AGENTS.md、Skills、Context 组装等产品策略。
- `internal/tools`：`apply_patch`、`edit`、`exec`、`process`、`read`、`write` 六个默认工具。
- `internal/observability`：强制校验 Usage，并按平台价格计算每次模型调用的 USD 成本与耗时。
- `internal/cli`：一次性 CLI 生命周期、会话持久化、MySQL、Terminal 和 WeCom。

`ai` 不依赖 `agent` 或产品包；`agent` 只依赖 `ai`，不依赖根包和 `internal`。集成测试会检查这一依赖方向，并拒绝旧 internal 包重新出现。

## 根 SDK 契约

上层业务通常只需要导入根包：

```go
config, err := reagent.LoadConfig("config.json")
if err != nil {
	return err
}
sdk, err := reagent.New(config)
if err != nil {
	return err
}
defer sdk.Close(context.Background())

result, err := sdk.Run(ctx, reagent.RunRequest{
	RunID:   runID,
	History: history,
	Input:   reagent.UserMessage(input),
	Context: contextBlocks,
	Metadata: map[string]string{
		"conversation_id": conversationID,
	},
})
```

根 API 只有以下生命周期入口：

```go
func LoadConfig(path string) (*Config, error)
func New(config *Config) (*Agent, error)
func (a *Agent) Run(context.Context, RunRequest) (RunResult, error)
func (a *Agent) Close(context.Context) error
```

`New` 不接收 Provider、Tool、Registry、Middleware、Reporter、Store 或 Fx Option。默认组件在私有 Fx 图中完成组装，避免上层业务依赖产品内部结构。

根 `RunResult` 是 `agent.RunResult` 的别名；除 `NewMessages` 外，`Invocations` 会按调用顺序返回本次运行所有已完成的 Thinking/Action 模型调用及其 Usage、成本与耗时。

## 配置

业务明确调用 `LoadConfig(path)`，SDK 不猜测配置路径，也不在 `New` 中读取 `CONFIG_PATH`。加载继续只使用 Configor，因此 JSON、YAML、TOML、example 回退、环境叠加和 `CONFIGOR_` 环境变量覆盖保持一致。

`New` 会复制并校验 Config。调用方在构造完成后修改原 Config，不会改变已经运行的 Agent。

每个平台都必须配置 `pricing.input_usd_per_million_tokens` 和 `pricing.output_usd_per_million_tokens`，单位为 USD/1M tokens。价格必须是有限非负数，允许 `0` 表示免费模型；价格是构造 SDK 时的快照。

## Run 数据流

```text
业务加载 History
    -> Agent.Run
    -> 校验并复制调用方数据
    -> 重新读取 AGENTS.md 和发现 Skills
    -> 组装 System + Context + History + Input
    -> AI / Tool Loop
    -> 返回 RunResult.NewMessages + RunResult.Invocations
    -> 业务按自己的事务策略持久化
```

`Run` 是同步且无状态的。SDK 不查询会话、不保存消息、不管理 UserID/ConversationID，也不做会话锁、重试、排队或摘要。业务可把标识放在 `Metadata` 中用于追踪，但 SDK 不解释其含义。

`NewMessages` 只包含当前 Run 新增的 Assistant/Tool 消息，不包含 System、外部 Context、History、Input 或 Thinking 脚手架。运行中途失败时，已经完成的消息仍与错误一起返回；是否持久化部分结果由业务决定。

默认 SDK 在 Provider 和 Loop 之间强制执行成本计量：每个被接受的 Thinking 或 Action 响应都必须有合法 Usage、按配置价格计算的准确成本，并对应一个有序 Invocation。工具循环中的重复 Action 调用也逐次计量。缺失、负数、NaN、无穷值或成本公式不一致都会返回 `ai.ErrGeneration`，不会把未计量响应作为成功结果。自行直接组合公共 `agent` 包时，调用方必须提供能返回完整计量 Usage 的 `ai.Client`；`agent.Loop` 会独立复核这些字段。

根 `Run` 不提供进度事件。底层 `agent.Reporter` 只供自行使用 `agent` 包的调用方和仓库自带 CLI 使用，Terminal/WeCom 事件不会进入根 SDK API。

## Workspace

`New` 从进程当前工作目录绑定 Workspace。每次 Run 都会重新读取该 Workspace 中的 `AGENTS.md` 并发现 `.agents/skills`、`.claw/skills` 或 `skills`，因此文件内容更新可以在下一次 Run 生效；构造 Agent 后再切换进程工作目录不会改变已绑定的 Workspace。

业务系统应在自己的 Workspace 提供领域专属 AGENTS.md 和 Skills。仓库根目录中的文件只定义自带 CLI 的默认 Agent。

## 并发与取消

一个长生命周期 `*reagent.Agent` 可以并发执行多个 Run：

- 每个 Run 拥有独立消息历史、调度结果和元数据副本；
- SDK 不修改调用方传入的 Slice、Map 或工具 JSON 参数；
- 取消或超时一个 Run 不会影响其他 Run；
- SDK 不按 ConversationID 串行化，并发控制仍由业务负责。

## 错误与部分结果

`ErrorCodeOf(err)` 返回稳定字符串枚举：

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

`reagent.Error` 通过 `Unwrap` 保留原始错误。`errors.Is` 可继续识别 `context.Canceled`、`context.DeadlineExceeded`、`reagent.ErrClosed` 和 `ai.ErrGeneration`；`errors.As` 可以获得 OpenAI 或 Anthropic 官方 SDK 错误。根 SDK 不把厂商状态码扩展成新的公共 ErrorCode。

## Close

`Agent.Close(ctx)` 首先拒绝新 Run，然后等待已经接纳的 Run 完成，最后停止 Fx 资源和后台进程。Close 是幂等的，后续调用返回第一次 Close 的结果；如果第一次因 Context deadline 失败，Agent 仍保持关闭且不会重新接纳 Run。

调用某个 Run 的 cancel 不会关闭 Agent。已经关闭的 Agent 不能重新启动。

## CLI 与会话存储

自带 CLI 使用同一个 `internal/bootstrap.Module`，再组合 `internal/cli.Module`：

```text
LoadOrCreate -> agent.Run -> AppendTurn
```

CLI 可以通过 MySQL 加载有界 History，并在同一事务中保存 Input、`NewMessages` 与 `Invocations`；Terminal 和 WeCom 通过底层 Reporter 接收事件。这些适配都位于 `internal/cli`，不属于根 SDK 的状态或扩展接口。隐藏 Thinking 文本和完整 Provider 请求没有持久化路径。
