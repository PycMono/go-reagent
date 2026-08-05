# MySQL 会话持久化

go-reagent 可以在 CLI 内部将多用户、多会话的完整消息历史保存到 MySQL。该功能默认关闭；关闭时不创建 MySQL 连接，CLI 保持原有无状态执行方式。

## 部署

1. 通过部署系统执行 `migrations/0001_conversation_persistence.up.sql`。程序不会调用 GORM `AutoMigrate`。
2. 在配置中填写 `mysql` 的所有字段，然后将 `conversation.enabled` 设为 `true`。
3. 运行一次性 CLI 任务前设置会话归属：

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

每轮使用乐观 `version` 条件更新，并在同一事务内写入当前 Input 和本轮 `RunResult.NewMessages`。内部 `conversation.ErrConflict` 是安全失败，当前实现不会自动重试、重排或排队；调用方应保证同一会话串行执行。

如果 `agent.Run` 在已经产生部分结果后失败，CLI 仍会尝试保存 Input 和非空 `NewMessages`，并使用 `errors.Join` 同时返回运行错误与持久化错误。运行在产生任何新消息前失败时不会追加 turn。

## 业务 SDK 调用方

根 `reagent` SDK 不读取或保存会话。业务系统应在 SDK 外部实现同样的事务边界：

```text
业务 Store 加载 History
    -> reagent.Agent.Run(History, Input)
    -> 业务 Store 保存 Input + RunResult.NewMessages
```

UserID、ConversationID、并发串行化、冲突重试和是否保存部分结果都属于业务策略。根 `RunRequest` 不增加这些专用字段；需要追踪时可由业务放入 `Metadata`。

Redis 队列、摘要压缩和自动重试不在当前实现范围内。如果 CLI 暂时不需要持久化，将 `conversation.enabled` 设为 `false` 即可使用无 MySQL 路径。
