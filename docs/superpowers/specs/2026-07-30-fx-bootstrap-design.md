# Fx 全链路依赖注入设计

## 目标

使用 `go.uber.org/fx` 管理 `Config -> WorkDir -> LLMProvider -> Tool Registry -> Reporter -> AgentEngine -> AgentRunner` 完整启动链路，同时保持当前模型、工具调度、企业微信群逐事件通知和一次性 CLI 行为不变。

## 方案

采用集中式组合根。新增 `internal/app` 负责 Fx 注册和生命周期编排，现有 `config`、`provider`、`tools`、`dispatch` 和 `engine` 包保持框架无关，不导入 Fx。

不采用参考项目中每个包一个 `register.go` 的方式。`go-reagent` 当前组件数量较少，分散注册会增加文件和依赖追踪成本；等模块规模扩大后再拆分 `fx.Option`。

## 依赖图

```text
cmd/reagent/main.go
  -> fx.New(app.Module).Run()
       -> *config.Config
       -> app.WorkDir
       -> provider.LLMProvider
       -> tools.Registry
       -> engine.Reporter
            -> TerminalReporter
            -> optional WeComReporter
       -> *engine.AgentEngine
       -> *app.AgentRunner
       -> fx.Lifecycle + fx.Shutdowner
```

## 组件边界

### 入口

`cmd/reagent/main.go` 只负责：

- 初始化 `go-logger-sdk`。
- 创建并运行 Fx App。

日志必须在 `fx.New` 前初始化，确保构造函数和 Fx 启动期间使用项目 Logger。Fx 自身日志使用适合 CLI 的精简 Logger，不输出依赖图噪声。

### 组合根

`internal/app/module.go` 暴露 `Module fx.Option`，集中注册全部构造函数和启动 Invoke。构造函数可以返回错误，由 Fx 统一中止启动并提供依赖链错误。

`internal/app/providers.go` 包含以下构造：

- `NewConfig`：按 `CONFIG_PATH` 或默认 `config.json` 加载 Configor 配置。
- `NewWorkDir`：读取当前工作目录，返回专用 `WorkDir` 类型，避免注入裸字符串。
- `NewLLMProvider`：根据当前平台配置创建 Provider，并记录不含密钥的平台元数据。
- `NewRegistry`：创建并注册文件工具，返回 `tools.Registry`。
- `NewReporter`：始终创建 TerminalReporter；配置企业微信地址时追加 WeComReporter。
- `NewAgentEngine`：注入 Provider、Registry 和 WorkDir，保持 Thinking 开启及当前并发设置。
- `NewAgentRunner`：注入 Engine、Reporter 和 Prompt。

### Runner

`AgentRunner` 是一次性任务执行单元。Prompt 从 `AGENT_PROMPT` 读取，空值时使用当前默认并发读取示例。

Runner 不持有 Fx 类型；它只暴露同步的 `Run(context.Context) error`。Fx 生命周期适配函数负责启动 Goroutine、取消和退出码。

## 生命周期

Fx `OnStart` 必须快速返回，不能直接同步运行可能耗时的大模型任务：

1. 创建独立、可取消的 Agent Context。
2. `OnStart` 启动一个 Goroutine 调用 `AgentRunner.Run`，然后立即返回。
3. Agent 正常完成后调用 `fx.Shutdowner.Shutdown()`。
4. Agent 失败时记录错误，并通过 `fx.Shutdowner.Shutdown(fx.ExitCode(1))` 结束应用。
5. `OnStop` 先取消 Agent Context，并等待 Runner 结束；等待时间受 Fx Stop Context 限制。
6. Runner 停止后再关闭 Registry 持有的文件工具。

Registry 的关闭钩子在 Runner 钩子之前注册。Fx 按逆序执行 `OnStop`，因此 Runner 的取消和等待先发生，资源关闭后发生，避免运行中的 Tool 使用已关闭资源。

## 错误处理

- 配置、工作目录、Provider、Registry 或 Reporter 构造失败：Fx 启动失败，不运行 Agent。
- Agent 失败：结构化日志记录根因，进程退出码为 1。
- 企业微信单次通知失败：沿用现有行为，仅记录脱敏错误，不中止 Agent。
- 外部 SIGINT/SIGTERM：取消 Agent，等待退出，再关闭工具。
- 日志和 Fx 错误不得包含 API Key 或企业微信 Webhook URL。

## 配置与行为兼容

- `CONFIG_PATH`、`CONFIGOR_ENV`、`CONFIGOR_*` 和 `AGENT_PROMPT` 行为保持不变。
- `bot.wecom.webhookURL` 为空时只启用终端 Reporter；非空时同时启用企业微信群通知。
- Reporter 的 `OnThinking`、`OnToolCall`、`OnToolResult`、`OnMessage` 仍逐事件发送，不聚合。
- 当前 DeepSeek/智谱等平台选择、Thinking 模式、工具定义和并发调度保持不变。
- 真实 Webhook 继续只存在于被 Git 忽略的本地 `config.json`。

## 测试

- 每个构造函数分别测试成功和错误路径。
- Reporter 构造测试空配置与企业微信配置两种分支。
- Registry 生命周期测试停止时关闭工具资源。
- Runner 生命周期测试正常完成触发 Shutdown、错误完成使用退出码 1、外部停止取消 Context。
- 使用 Fx 测试工具启动完整依赖图，并替换真实 Provider/Reporter，禁止测试访问真实模型和企业微信。
- 保留现有 Engine、Provider、Tool 和 Reporter 测试。
- 完成后运行 `go test -race ./...`、`go vet ./...`、`git diff --check`，再使用固定短 Prompt 做一次真实企业微信群联调。

## 不在本期范围

- 企业微信或飞书入站回调。
- 常驻 HTTP 服务。
- 会话存储和消息队列。
- 按包拆分多个 Fx Module。
- 修改模型、日志 SDK 或 Reporter 消息格式。
