# Tool Runtime 收敛设计

## 背景

`pi/toolexec` 当前由 `Registry → Executor → Scheduler` 三个对象串联。`Executor`
只包装 Registry 与中间件，`Scheduler` 又通过 Executor 访问 Registry，同时每次调度
额外接收当前 Run 的可用工具定义。这使对象所有权和“注册全集 / 当轮可见子集”的边界
不够直观。

## 目标

- 对外只保留 `Registry` 与 `Runtime` 两个核心对象。
- Registry 只负责启动期工具注册、扩展回滚、冻结和查询。
- Runtime 负责单次执行、批量调度、并发模式判断和子代理工具识别。
- 保持工具可见性、并发屏障、结果顺序、事件、错误和中间件语义不变。

## 非目标

- 不改变 Tool、ToolOutput、Event 或 middleware 契约。
- 不把 Registry 并入 Runtime。
- 不改变扩展注册与 freeze 时序。
- 不改变主 Agent 与子代理的工具白名单策略。

## 设计

### 对象关系

```text
Extension Runtime ──注册/回滚/冻结──> Registry
                                      │
                                      ▼
Agent / Loop / Subagent ───────────> Tool Runtime
                                      ├── Execute
                                      ├── Schedule
                                      ├── Mode
                                      ├── Definitions
                                      └── IsSubagentTool
```

`toolexec.Runtime` 持有 `*Registry`、中间件快照和最大并发数。原 Executor 的
`Definitions/Execute` 与原 Scheduler 的 `Schedule/Mode/IsSubagentTool` 都成为 Runtime
方法。调度内部直接调用 `Runtime.Execute`，不再经过第二个对象。

文件仍按关注点拆分：`runtime.go` 保存类型、构造和单次执行；`scheduler.go` 保存同一
Runtime 的批量调度方法；`registry.go` 保持注册表实现。文件拆分不再对应三个对象。

### API 变化

删除：

- `Executor`
- `Scheduler`
- `NewExecutor`
- `NewScheduler`

新增：

```go
func NewRuntime(registry *Registry, handlers []middleware.Handler, maxParallel int) *Runtime
```

`Loop`、`Agent`、`SubagentBinder` 改为持有 `*toolexec.Runtime`。这是已确认接受的源码
不兼容变更。

### 注册全集与可见子集

Registry 继续代表已注册工具全集。`Schedule` 与 `Mode` 的 `availableTools` 参数继续
代表当前 Run 可见子集：根 Agent 通常传全集，子代理传白名单。Runtime 不用 Registry
全集替代该参数，避免扩大子代理执行权限。

### 生命周期与错误

Runtime 可在 Registry freeze 前构造，但 Agent 仍只在扩展启动、注册并 freeze 后运行。
未注册工具、参数校验、panic、超时、重试、取消和 End Event 归一化行为保持不变。
并发批次仍保留串行屏障、最大并发限制和结果原始顺序。

## 测试

- 保留 Registry 注册、回滚、冻结和排序测试。
- 保留 Runtime 单次执行、nil observer、生命周期事件、错误归一化和输出限制测试。
- 增加 Runtime 调度模式、并发上限、串行屏障和结果顺序测试。
- 运行 `go test ./pi/... -count=1` 与 `git diff --check`。
- 全仓测试若仍受现有 conversation Fx 装配问题影响，单独报告，不扩大本次修复范围。
