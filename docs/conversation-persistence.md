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

MySQL 保存每条完整消息。每次运行只将最近 `conversation.history_message_limit` 条安全消息传给 Runtime，默认值为 `100`。如果窗口边界落在一个 turn 中间，该不完整 turn 会被整体排除。当前版本不做摘要或 token 压缩。

`mysql.conn_lifetime` 的单位是分钟，因此 `3600` 表示 60 小时。

## 并发与冲突

每轮使用乐观 `version` 条件更新，并在同一事务内写入本轮所有消息。`conversation.ErrConflict` 是安全失败，当前实现不会自动重试、重排或排队；调用方应保证同一会话串行执行。

Redis 队列、摘要压缩和公共 SDK API 不在本期范围内。如果暂时不需要持久化，将 `conversation.enabled` 设为 `false` 即可保留旧的无 MySQL 路径。
