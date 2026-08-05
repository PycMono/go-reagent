# MySQL 会话持久化

go-reagent 可以在 CLI 内部将多用户、多会话的完整消息历史保存到 MySQL。该功能默认关闭；关闭时不创建 MySQL 连接，CLI 保持原有无状态执行方式。

## 部署

1. 通过部署系统先执行 `migrations/0001_conversation_persistence.up.sql`，再执行 `migrations/0002_model_invocation_observability.up.sql`。程序不会调用 GORM `AutoMigrate`。
2. 在每个平台配置中填写 `pricing.input_usd_per_million_tokens` 和 `pricing.output_usd_per_million_tokens`，单位均为 USD/1M tokens。
3. 在配置中填写 `mysql` 的所有字段，然后将 `conversation.enabled` 设为 `true`。
4. 运行一次性 CLI 任务前设置会话归属：

```bash
export AGENT_USER_ID="user-1"
export AGENT_CONVERSATION_ID="conversation-1"
```

`user_id` 和 `conversation_id` 共同确定会话；相同 `conversation_id` 可以安全地被不同用户重用。

## 历史窗口

MySQL 保存每条完整消息。每次运行只将最近 `conversation.history_message_limit` 条安全消息传给 `agent.Run`，默认值为 `100`。如果窗口边界落在一个 turn 中间，该不完整 turn 会被整体排除。当前版本不做摘要或 token 压缩。

`mysql.conn_lifetime` 的单位是分钟，因此 `3600` 表示 60 小时。

## 并发与冲突

CLI 会话路径固定为：

```text
LoadOrCreate -> agent.Run -> AppendTurn
```

每轮使用乐观 `version` 条件更新，并在同一事务内写入当前 Input、本轮 `RunResult.NewMessages` 和已完成的 `RunResult.Invocations`。消息或任一 invocation 写入失败时，整个事务都会回滚。内部 `conversation.ErrConflict` 是安全失败，当前实现不会自动重试、重排或排队；调用方应保证同一会话串行执行。

如果 `agent.Run` 在已经产生部分结果后失败，CLI 仍会尝试保存 Input 和非空 `NewMessages`，并使用 `errors.Join` 同时返回运行错误与持久化错误。运行在产生任何新消息前失败时不会追加 turn。

## 模型调用总账

客户原始输入与可见的 Action/Tool 输出保存在消息历史中。对每个已追加的 turn，所有已完成且通过严格 Usage 校验的 Thinking 或 Action 调用都会按运行顺序单独写入 `agent_model_invocations`，包括平台、模型、输入/输出 Token、调用时的 USD 单价、成本和耗时。一次用户请求触发多轮工具循环时会产生多条流水，不会覆盖或合并；没有产生任何可见新消息的失败运行不会追加 turn 或 ledger 行，其调用失败/计量诊断仍保留在结构化日志中。

`agent_model_invocations` 是 Token、成本与耗时聚合的唯一权威来源。可见 Assistant 消息 JSON 中的 Usage 仅用于检查单条消息，不能再与流水表相加，否则会重复计费。按会话聚合可使用：

单价和单次调用成本以 `DECIMAL(20,12)` 保存，应用层要求它们是小于 100,000,000 的有限非负数；超出范围会在 Run 或事务开始前被拒绝。

```sql
SELECT
    SUM(input_tokens) AS input_tokens,
    SUM(output_tokens) AS output_tokens,
    SUM(cost_usd) AS cost_usd,
    SUM(latency_ms) AS latency_ms
FROM agent_model_invocations
WHERE conversation_pk = ?;
```

Thinking 调用只保存阶段和计量指标，不保存隐藏 Thinking 文本。系统也不会保存完整 Provider 请求、工具定义、API Key 或 Authorization Header。

## 业务 SDK 调用方

根 `reagent` SDK 不读取或保存会话。业务系统应在 SDK 外部实现同样的事务边界：

```text
业务 Store 加载 History
    -> reagent.Agent.Run(History, Input)
    -> 业务 Store 原子保存 Input + RunResult.NewMessages + RunResult.Invocations
```

UserID、ConversationID、并发串行化、冲突重试和是否保存部分结果都属于业务策略。根 `RunRequest` 不增加这些专用字段；需要追踪时可由业务放入 `Metadata`。业务总账也应只聚合 invocation 记录，避免与消息 Usage 重复相加。

Redis 队列、摘要压缩和自动重试不在当前实现范围内。如果 CLI 暂时不需要持久化，将 `conversation.enabled` 设为 `false` 即可使用无 MySQL 路径。
