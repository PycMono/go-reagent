# 企业微信群 Reporter 通知设计

## 目标

沿用 `bot.md` 的 Reporter 思路，把当前 Agent 生命周期逐条通知到企业微信群。当前阶段只做单向群通知，不接收企业微信回调，也不接入飞书。

## 数据流

```text
cmd/reagent 主动调用 AgentEngine.Run
  -> AgentEngine 触发 Reporter 生命周期方法
  -> WeComReporter 调用企业微信群机器人 Webhook
  -> 群内展示 Agent 运行过程和模型回复
```

## Reporter 契约

Reporter 保留 `bot.md` 定义的四类事件，不做聚合：

- `OnThinking(ctx)`：每次进入 Thinking 阶段时发送提示。
- `OnToolCall(ctx, toolName, args)`：每次工具开始执行时单独发送通知。
- `OnToolResult(ctx, toolName, result, isError)`：每次工具执行结束时单独发送通知。
- `OnMessage(ctx, content)`：每次 Action 返回非空文本时发送模型回复。

并发工具的 `OnToolCall` 和 `OnToolResult` 按实际触发顺序发送，因此群内顺序可能与 Tool Call 原始顺序不同，这是并发执行的正常表现。

## 组件

- `engine.Reporter`：平台无关的生命周期接口。
- `TerminalReporter`：保留现有 Thinking Trace 和对外回复的终端展示。
- `WeComReporter`：把四类事件转换成企业微信 Markdown 消息并调用群机器人 Webhook。
- `MultiReporter`：按注册顺序把同一事件广播给终端和企业微信 Reporter，使现有终端输出与群通知同时保留。

四个方法保持 `bot.md` 中不返回错误的签名。Reporter 发送失败时记录脱敏错误日志，但不终止 Agent Run。HTTP 请求使用调用方的 `context.Context`，设置请求超时并关闭响应体。

## 配置与安全

- Webhook 地址使用当前 Configor 配置体系中的 `bot.wecom.webhookURL` 字段。
- 本地且已被 Git 忽略的 `config.json` 保存真实地址，`config.example.json` 只提供空值。
- `webhookURL` 为空时只启用 `TerminalReporter`，保证当前 CLI 仍能运行。
- 非空地址必须是带 Host 的 HTTPS URL。
- 不在配置样例、源码、测试、日志或提交记录中输出真实 Webhook 地址。
- 模型 Provider、模型配置、工具注册和 `go-logger-sdk` 保持不变。

## 消息格式

- Thinking：`🤔 模型正在慢思考 (Thinking)...`
- Tool Call：包含工具名和参数。
- Tool Result：成功时报告工具名；失败时包含经过长度限制的错误信息。
- Message：发送模型返回的文本内容。

所有消息使用企业微信群机器人 `markdown` 类型。单条内容限制在 4096 字节以内；超长内容在合法 UTF-8 边界截断并添加截断标记，保证每个 Reporter 事件仍只发送一条消息。

## 测试

- Reporter 接口的四类事件分别产生一次 HTTP 请求。
- 并发事件不会造成请求体数据竞争。
- 企业微信非成功响应和网络错误会记录脱敏错误日志，且不影响 Engine 继续运行。
- 未配置 Webhook 时只执行终端 Reporter。
- Webhook URL 不出现在结构化日志中。

## 不在本期范围

- 企业微信回调、验签和消息解密。
- 用户在群里发消息触发 Agent。
- 通知聚合、消息更新和流式回复。
- 会话历史、幂等去重和持久化。
- 飞书 Reporter。
