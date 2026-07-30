# go-logger-sdk 日志迁移设计

## 目标

将生产代码中的 Go 标准库 `log` 全部替换为
`github.com/PycMono/go-logger-sdk` v1.0.5，统一输出可供日志平台采集的 JSON 日志，
同时保留模型思考和最终回复的纯文本终端输出。

## SDK 行为结论

- `NewLogrus(Options)` 返回 `logsdk.Logger`，支持 `Debug`、`Info`、`Warn`、`Error`、
  `Fatal` 和实例级 `Panic`。
- 所有日志方法接收 `context.Context` 和零个或多个 `Fields`。
- `LogFormat` 仅在值为 `text` 时输出文本，其他值（包括空值和 `json`）输出 JSON。
- `Module` 固定写入每条日志的 `module` 字段；多个 `Fields` 按传入顺序合并，后值覆盖前值。
- `Any` 会把字节切片转成字符串、时间格式化为 RFC3339、Duration 转成字符串、复杂对象转成
  JSON 字符串；`Err` 使用 `error` 字段并在可用时生成 `errorsStack`。
- SDK 输出到 stdout，内部日志级别固定为 Trace，因此不会按配置过滤等级。
- `Fatal` 写日志后调用 `os.Exit(1)`，与当前标准库 `log.Fatal` 一样不会执行 defer。
- `SetLogger` 直接替换无锁的包级变量，只允许在启动阶段、并发工作开始前调用一次。
- v1.0.5 是当前最新版本。实测其 `caller` 字段指向 SDK 自身方法，例如
  `Info[logrus.go:73]`，而不是真实业务调用处。本次接受此缺陷，用结构化业务字段补足定位；
  修复 SDK 不在本次范围内。

## 初始化与依赖

`cmd/reagent` 在任何可能失败的启动操作之前创建并设置一次默认 Logger：

```go
logsdk.SetLogger(logsdk.NewLogrus(logsdk.Options{
	LogFormat: "json",
	Module:    "go-reagent",
}))
```

`github.com/PycMono/go-logger-sdk v1.0.5` 从间接依赖移入 `go.mod` 的直接依赖块。
它的 `logrus`、`go-errors` 等实现依赖继续由 Go Modules 作为间接依赖管理，不主动修改版本。

## 日志映射

### Bootstrap

`cmd/reagent/main.go` 使用 `context.Background()` 完成初始化并复用同一个 Context：

- 获取工作区失败：`Fatal`，字段 `component=bootstrap`、`error`。
- 配置或 Provider 初始化失败：`Fatal`，字段 `component=bootstrap`、`error`。
- 平台选择成功：`Info`，字段 `component=bootstrap`、`platform_id`、`protocol`、`model`；
  不记录 API Key、Authorization Header 或完整配置。
- Registry 初始化失败：`Fatal`，字段 `component=bootstrap`、`error`。
- Registry 资源关闭失败：`Error`，字段 `component=bootstrap`、`error`。
- Engine 运行失败：`Fatal`，字段 `component=bootstrap`、`error`。

### Engine

`internal/engine/loop.go` 直接调用 SDK 包级函数，并优先复用 `Run` 或工具执行收到的 Context：

- Engine 启动：`Info`，字段 `component=engine`、`work_dir`、`thinking_enabled`。
- 新轮次：`Info`，字段 `component=engine`、`turn`。
- Thinking/Action 阶段开始：`Info`，字段 `component=engine`、`turn`、`phase`。
- 模型直接完成：`Info`，字段 `component=engine`、`turn`。
- 工具调度：`Info`，字段 `component=engine`、`turn`、`tool_count`。
- 单个工具开始：`Info`，字段 `component=engine`、`tool_index`、`tool`、
  `tool_call_id`、`arguments`。
- 单个工具失败：`Error`，字段增加 `result`。
- 单个工具成功：`Info`，字段增加 `result_bytes`。

工具参数和错误结果延续当前日志可见性，不扩大记录范围。已有两处 `fmt.Printf` 分别输出模型内部
思考和最终回复，它们属于用户可见结果而不是运行日志，保持不变。

### Registry

`internal/tools/registry.go` 使用 SDK 包级函数：

- 工具注册成功：`Info`，使用 `context.Background()`，字段 `component=registry`、`tool`。
- 工具执行 panic：`Error`，使用执行时 Context，字段 `component=registry`、`tool`、`stack`。
  保持现有安全边界：只记录工具名和调用栈，不记录 panic 值。

## 测试与验收

- 用实现 `logsdk.Logger` 的记录型 Fake 替换包级 Logger，验证代表性 Engine 和 Registry
  事件的级别、消息与关键字段。相关测试不并行运行，避免无锁 `SetLogger` 产生竞态。
- Fatal 行为不在单元测试进程内直接调用；SDK 自身已定义并实现退出语义，项目通过编译和启动
  调用映射验证使用方式。
- 全仓生产 Go 文件不再 import 标准库 `log`，也不再调用 `log.Print*` 或 `log.Fatal*`。
- `fmt.Printf` 只保留模型思考和最终回复两处用户输出。
- `go test -count=1 ./...`、`go test -race -count=1 ./...`、`go vet ./...` 和
  `git diff --check` 全部通过。
