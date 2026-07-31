# Fx Register 分层聚合设计

## 背景

当前应用只通过 `internal/app.Module` 组装 Fx 依赖，配置、Provider、工具、Reporter 和 Engine 的构造逻辑集中在 `internal/app/providers.go`。这使 `app` 同时承担应用生命周期和各业务包对象构造两类职责。

本次重构将对象注册职责下沉到对象所属包，并由根级 `internal.Register` 统一聚合，最终只向 `cmd/main.go` 暴露一个 Fx Option。

## 目标

- `config`、`context`、`dispatch`、`engine`、`provider`、`tools` 分别提供 `Register`。
- `app` 保留，且只注册 `AgentRunner` 和 Fx 启停生命周期。
- `internal.Register` 只聚合各子包的 `Register`，不承载具体构造逻辑。
- `cmd/main.go` 通过 `internal.Register` 启动完整应用。
- 保持现有运行流程、错误处理、工具集合、退出码和资源关闭顺序不变。
- `schema` 只有纯数据结构，不创建没有实际作用的 `register.go`。

## 非目标

- 不改变 Agent 的 Thinking/Action 主循环。
- 不改变模型协议、工具参数或 Reporter 的输出格式。
- 不把现有平铺的工具实现拆成子包。
- 不引入自动扫描或反射式注册。
- 不改变配置文件和环境变量名称。

## 最终依赖入口

```text
cmd/main.go
    └── internal.Register
          ├── config.Register
          ├── context.Register
          ├── provider.Register
          ├── tools.Register
          ├── dispatch.Register
          ├── engine.Register
          └── app.Register
```

Fx 根据构造函数参数解析实际初始化顺序，`fx.Options` 中的书写顺序只用于表达从基础设施到应用层的阅读顺序。

## 各包职责

### config.Register

`config` 成为进程运行配置的基础包，负责提供：

- `*Config`：继续根据 `CONFIG_PATH` 加载配置，默认路径仍为 `config.json`。
- `WorkDir`：当前进程工作目录的强类型值。
- `Prompt`：优先读取 `AGENT_PROMPT`，否则使用现有默认任务。

`WorkDir` 和 `Prompt` 从 `app` 移入 `config`，避免其他业务包为了取得运行参数反向依赖应用层。

### context.Register

`context` 负责从 `config.WorkDir` 构造并提供：

- `*PromptComposer`
- `*SkillLoader`

Engine 的 Fx 构造路径显式注入这两个对象。现有 `NewAgentEngine` 便捷构造函数保留，用于单元测试和非 Fx 调用，并继续根据工作目录自行创建默认 Context 组件。

### provider.Register

`provider` 负责根据 `*config.Config`：

- 选择当前平台。
- 调用现有 Provider 工厂。
- 提供 `LLMProvider`。
- 保留现有平台初始化日志和错误包装。

### tools.Register

`tools` 负责完整的工具运行时组装：

- 根据 `config.WorkDir` 创建 `read_file`、`edit_file`、`write_file`、`apply_patch`。
- 创建共享的 `ProcessManager`，并基于它创建 `exec` 和 `process`。
- 创建线程安全 Registry，将六个工具注册进去。
- 同时提供 Engine 所需的只读 `tools.Registry` 接口。
- 通过 Fx Lifecycle 逆序关闭工具和 `ProcessManager`。

具体工具仍保留在当前 `internal/tools` 包中；`tools/register.go` 聚合的是同一包内的构造与注册流程，不新增工具子目录。

### dispatch.Register

`dispatch` 负责根据 `*config.Config` 提供唯一的 `engine.Reporter`：

- 未配置企业微信时只返回 Terminal Reporter。
- 配置企业微信时返回 Terminal 与 WeCom 的 MultiReporter。
- 保留现有 Webhook 构造错误和发送失败处理。

依赖方向为 `dispatch -> engine` 的 Reporter 接口与实现，不允许 `engine` 反向导入 `dispatch`。

### engine.Register

`engine` 负责提供：

- `Agent` 接口，声明 `Run(context.Context, string, Reporter) error`。
- 由 `LLMProvider`、`tools.Registry`、`config.WorkDir`、`PromptComposer` 和 `SkillLoader` 组装的 `AgentEngine`。
- 以 `Agent` 接口类型暴露 Engine，供 `app.AgentRunner` 使用。

Fx 专用构造路径保持 Thinking 默认开启、工具并发上限默认值不变。

### app.Register

`app` 只负责应用层编排：

- 构造 `AgentRunner`，注入 `engine.Agent`、`engine.Reporter` 和 `config.Prompt`。
- 调用 `RegisterAgentLifecycle` 注册 OnStart/OnStop。
- 保留成功退出码 0、Engine 错误退出码 1、停止时取消并等待 Agent 的行为。

原 `app.Agent`、`app.WorkDir` 和 `app.Prompt` 分别由 `engine.Agent`、`config.WorkDir` 和 `config.Prompt` 替代。`app/providers.go` 中的构造逻辑迁移完成后删除，`app.Module` 由 `app.Register` 取代。

### internal.Register

根 `internal/register.go` 导入并聚合所有实际模块：

```go
var Register = fx.Options(
    config.Register,
    context.Register,
    provider.Register,
    tools.Register,
    dispatch.Register,
    engine.Register,
    app.Register,
)
```

根包不得包含具体对象构造函数，也不允许任何子包反向导入根 `internal` 包。

### schema

`schema` 保持纯数据结构包，不添加 `register.go`，也不依赖 Fx。

## 生命周期与失败处理

- 任意构造函数失败时，Fx 启动失败，Agent 不运行。
- 工具构造过程中部分成功、后续失败时，已经创建的资源仍按逆序立即关闭。
- 正常停止或收到信号时，依赖关系保证先停止 `AgentRunner`，再关闭工具资源，避免运行中的 Tool 使用已释放对象。
- Reporter 发送错误仍只记录日志，不升级为 Agent 运行错误。

## 测试策略

- 为每个非空 `Register` 添加或调整依赖图/构造测试。
- 将完整依赖图测试改为验证 `internal.Register`，不再验证 `app.Module`。
- 保留各包现有构造函数和行为测试；迁移测试包名与引用类型，但不降低断言范围。
- 保留 `tests/integration` 中的 Registry 生命周期、Reporter 分发和 Engine+真实工具测试。
- 运行 `go test ./...`、`go vet ./...` 和 `git diff --check` 作为最终验证。

## 完成标准

- `cmd/main.go` 只使用 `internal.Register` 作为业务依赖入口。
- 除 `schema` 外，约定的每个业务包都有承担实际注册职责的 `register.go`。
- `internal/app/providers.go` 不再集中持有其他包的构造逻辑。
- 不存在 Fx 重复 Provider、缺失依赖或 Go import cycle。
- 全量测试和静态检查通过。
