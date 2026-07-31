# 结构化 Tool Runtime 全链路重构设计

日期：2026-07-31

## 1. 背景

go-reagent 当前对模型暴露 `read_file`、`edit_file`、`write_file`、`apply_patch`、`exec` 和 `process` 六个工具。工具接口、Engine 上下文和 Reporter 都以字符串为中心：具体工具返回 `(string, error)`，Registry 包装为 `ToolResult.Output`，Engine 再把结果写入 `Message.Content`。工具 Observation 通过 `RoleUser + ToolCallID` 模拟，增量执行状态没有统一协议。

本次重构参考最新版 Pi Agent Runtime 与 OpenClaw 的核心分层思想，建立适合 Go 的结构化工具运行时。go-reagent 继续提供完整默认实现，同时允许仓库内部通过依赖注入扩展 Tool、Middleware 和 Reporter。不会照搬 Pi 的 TUI 字段，也不会引入 OpenClaw 的多主机、审批和沙箱子系统。

## 2. 目标与非目标

### 2.1 目标

- 最终只暴露 `read`、`edit`、`write`、`apply_patch`、`exec` 和 `process` 六个工具。
- 删除 `read_file`、`edit_file`、`write_file` 及旧参数协议，不保留兼容别名。
- 使用结构化文本内容、工具输出、最终结果和生命周期事件。
- 只有 `exec` 实时发送 stdout/stderr 增量更新。
- 将 Agent 回合状态机、工具调度和工具执行运行时拆成独立边界。
- 使用共享 Workspace 和 ProcessSupervisor，并由 Fx 统一管理生命周期。
- 文件工具严格限制在 WorkDir；Shell 只限制启动目录，不宣称是文件系统沙箱。
- 保留工具批次的并发调度、独占屏障和稳定结果顺序。

### 2.2 非目标

- 本轮不增加 `grep`、`find` 和 `ls`；搜索仍通过 `exec` 执行。
- 本轮不支持图片内容块或图片读取。
- 本轮不实现 PTY、Sandbox、人工审批、远程节点或多主机路由。
- 本轮不提供可被其他 Go Module 导入的公共 SDK；扩展契约先保留在 `internal`。
- 本轮不实现事件持久化、会话重放或完整事件溯源。

## 3. 总体架构

运行链路如下：

```text
app.Runner
  -> AgentRuntime
       -> RunContextFactory
       -> AgentLoop
            -> Provider
            -> ToolScheduler
                 -> Tool Registry
                      -> Middleware Chain
                      -> Concrete Tool
                           |- ToolUpdate -> AgentEvent -> Reporter
                           `- ToolOutput -> ToolResult -> Message History -> Provider
```

各层职责：

- `AgentRuntime` 是应用层唯一运行入口，协调运行上下文准备和 AgentLoop。
- `RunContextFactory` 负责 Skill 发现、Prompt 组合及初始消息，不进入 ToolScheduler。
- `AgentLoop` 负责 Thinking/Action、Provider 回合、消息历史和终止条件。
- `ToolScheduler` 只根据 `ParallelSafe` 和最大并发数划分执行波次、调用 Registry，并按模型原始 Tool Call 顺序聚合结果。
- `Registry` 负责工具查找、参数校验、中间件执行、错误规范化和工具事件。
- 具体 Tool 只处理自己的参数和领域行为，不关心 Provider、Reporter 或 ToolCallID。
- Provider 只负责内部结构与具体模型协议之间的转换。
- Reporter 通过统一 AgentEvent 接口订阅运行状态。

这与 Pi/OpenClaw 的分层原则一致：通用 Agent Loop 不硬编码产品安全策略和具体工具；产品层提供完整默认工具、Workspace Guard、进程管理和通知实现。go-reagent 的调用方仍只需使用默认 AgentRuntime，不需要自行组装整套运行时。

## 4. 结构化数据契约

### 4.1 内容与消息

本轮只支持文本内容，但使用可扩展的内容块结构：

```go
type ContentType string

const ContentTypeText ContentType = "text"

type ContentBlock struct {
    Type ContentType `json:"type"`
    Text string      `json:"text"`
}
```

`Message` 增加真正的 Tool Role，并使用内容块承载文本：

```go
const RoleTool Role = "tool"

type Message struct {
    Role       Role           `json:"role"`
    Content    []ContentBlock `json:"content,omitempty"`
    ToolCalls  []ToolCall     `json:"tool_calls,omitempty"`
    ToolCallID string         `json:"tool_call_id,omitempty"`
    ToolName   string         `json:"tool_name,omitempty"`
    IsError    bool           `json:"is_error,omitempty"`
}
```

工具 Observation 不再使用 `RoleUser + ToolCallID` 模拟。`Details` 不进入消息历史，也不发送给模型。

### 4.2 执行输出与最终结果

具体工具返回的领域输出与 Runtime 包装的最终结果必须分离：

```go
type ToolOutput struct {
    Content []ContentBlock
    Details any
}

type ToolUpdate struct {
    Content []ContentBlock
    Details any
}

type ToolResult struct {
    ToolCallID string
    ToolName   string
    Content    []ContentBlock
    Details    any
    IsError    bool
}
```

这一结构对应 Pi 的设计：Tool 执行只返回 `content/details`，Agent Loop 或执行运行时再补充 `toolCallId/toolName/isError`，形成 Tool Result Message。

### 4.3 生命周期事件

统一事件至少包含：

- `thinking`
- `tool_start`
- `tool_update`
- `tool_end`
- `message`

Registry 只产生工具领域事件，AgentLoop 再把它包装成统一 AgentEvent：

```go
type ToolEvent struct {
    Phase  ToolEventPhase
    Call   ToolCall
    Update *ToolUpdate
    Result *ToolResult
}

type AgentEvent struct {
    Type    AgentEventType
    Tool    *ToolEvent
    Message *Message
}
```

`tool_start/tool_update/tool_end` 的 AgentEvent 必须携带 ToolEvent；`message` 必须携带 Message；`thinking` 不要求额外载荷。这个判别式约束由构造函数和测试保证，调用方不直接拼装不完整事件。

ToolEvent 携带 ToolCall、可选 ToolUpdate 或最终 ToolResult。更新事件不写入模型历史；最终结果才生成 RoleTool Message。

`ToolDefinition` 保留 `Name`、`Description`、`InputSchema` 和 `ParallelSafe`，增加可选 `Label`。Pi 的 TUI render、usage、addedToolNames、terminate 和图片字段不进入本轮协议。

## 5. Tool Runtime

### 5.1 Tool 与 Registry 接口

```go
type UpdateEmitter func(schema.ToolUpdate)

type Tool interface {
    Definition() schema.ToolDefinition
    Execute(
        ctx context.Context,
        args json.RawMessage,
        emit UpdateEmitter,
    ) (schema.ToolOutput, error)
}

type ToolEventObserver func(context.Context, schema.ToolEvent)

type Registry interface {
    GetAvailableTools() []schema.ToolDefinition
    Execute(
        ctx context.Context,
        call schema.ToolCall,
        observer ToolEventObserver,
    ) (schema.ToolResult, error)
}
```

具体 Tool 不接收 ToolCallID。Registry 使用模型的 ToolCall 包装 start、update、end 事件和最终 ToolResult。

### 5.2 执行顺序

Registry 对一次调用按以下顺序执行：

1. 检查 Context 并查找工具。
2. 发送 `tool_start`。
3. 使用 ToolDefinition 的 JSON Schema 严格校验参数。
4. 执行全局与扩展 Middleware。
5. 调用具体工具；Emitter 产生的更新被补齐调用信息并转成 `tool_update`。
6. 规范化空内容、Details 和错误。
7. 发送 `tool_end`。
8. 返回最终 ToolResult。

所有参数拒绝未知字段。Runtime 采用统一 JSON Schema 校验，具体工具仍使用强类型结构进行二次解码，防止 Schema 与运行时行为漂移。

### 5.3 Middleware

默认 Middleware 覆盖：

- panic 恢复
- Context 取消
- 参数校验
- 结构化日志
- 输出硬上限
- 事件转发

Middleware 是未来审批、沙箱、审计和策略控制的扩展点。本轮不实现这些策略。

普通参数错误、文件错误、命令非零退出等被规范化为 `IsError=true` 的文本结果，让模型可以修正。Context 取消等控制流错误向 AgentLoop 返回 Go error 并终止本轮。panic 转成通用错误结果，堆栈只写内部日志。

### 5.4 输出边界

输出限制分两层：

- 具体工具负责分页、截断提示和对应 Details。
- Runtime 提供统一硬上限，初始统一为 50 KiB，防止工具遗漏预算控制。

空输出统一转换为明确文本，例如 `(no output)`。日志只记录工具名、调用 ID、阶段、字节数和状态，不记录完整参数、环境变量或工具输出。

## 6. 共享资源与 WorkDir

文件工具共享一个封装 `os.Root` 的 Workspace，`exec/process` 共享一个 ProcessSupervisor。具体工具不再各自打开 WorkDir 或独立负责 Close。

Workspace 保证：

- 只接受 WorkDir 相对路径。
- 拒绝绝对路径、Volume 路径、`..` 穿越和外部符号链接逃逸。
- 文件工具只操作满足各自要求的普通文件或目录。

ProcessSupervisor 保证：

- `exec.workdir` 只能是 WorkDir 内的现有目录。
- Context 取消、超时、kill、remove 和 Fx Stop 都终止完整进程组。
- 会话状态与输出缓冲受锁保护。
- 输出缓冲保持有界，初始沿用当前 50 KiB 上限。

WorkDir 只限制 Shell 的启动目录。没有 Sandbox 时，命令仍可主动访问工作区外文件或网络；工具描述、README 和安全文档必须明确这一点。

## 7. 六个工具协议

### 7.1 read

- 名称：`read`
- 参数：`path`、可选 `offset`、`limit`
- 路径仅允许 WorkDir 相对路径。
- 只读取普通 UTF-8 文本，拒绝目录、设备、NUL 和无效 UTF-8。
- 默认且最大 2000 行，最终文本不超过 50 KiB。
- 截断时返回下一页位置。
- Details 包含路径、输出行数、字节数、截断状态和 `nextOffset`。
- `ParallelSafe=true`。

### 7.2 edit

- 名称：`edit`
- 参数：`path`、非空 `edits[]`；每项包含 `oldText/newText`。
- 所有 oldText 都基于原始文件匹配，不进行逐项累积匹配。
- 每项必须唯一命中；全部替换不得重叠或嵌套。
- 全部预检通过后一次写回，保留原换行风格和文件权限。
- Details 包含 diff、标准 patch、替换数量和首个修改行。
- 不保留旧 `old_text/new_text` 或单替换协议。

### 7.3 write

- 名称：`write`
- 参数：`path`、`content`。
- 创建父目录，创建或完整覆盖 UTF-8 文本文件。
- 相同内容不重复写入。
- Details 包含路径、字节数和是否发生变化。

### 7.4 apply_patch

- 名称：`apply_patch`
- 保持最新版 OpenClaw 的单一 `input` 字段。
- 支持 Add、Update、Delete、Move 和多文件操作。
- 所有语法、路径、冲突和上下文先在内存中预检，再开始提交。
- Details 包含操作数量和涉及文件。
- 预检失败不写磁盘；跨多个文件无法获得操作系统级原子性，提交阶段 I/O 故障仍可能造成部分完成。

### 7.5 exec

- 名称：`exec`
- 参数：`command`、可选 `workdir/env/yieldMs/background/timeout`。
- 使用最新版驼峰字段；`timeout` 单位为秒，`yieldMs` 单位为毫秒。
- 默认 `yieldMs=10000`；省略 timeout 时初始沿用现有 120 秒默认值，最大 600 秒。
- 前台执行实时发送 stdout/stderr 文本更新；更新 Details 标识 stream 类型和字节数。
- `background=true` 立即返回后台 session；前台命令超过 yield 时间自动转为后台 session。
- 最终 Details 包含 status、sessionId、exitCode、command、cwd 和 truncated。
- 非零退出形成 `IsError=true` 的最终结果。
- Context 取消或 timeout 终止完整进程组。
- 不暴露 `pty/host/elevated/security/ask/node`。

### 7.6 process

- 名称：`process`
- action：`list/poll/log/write/kill/clear/remove`。
- 使用驼峰 `sessionId`。
- `poll.timeout` 单位为毫秒，最大 30000。
- `log` 使用 `offset/limit` 在当前保留的有界日志中分页。
- `write` 使用 `data/eof` 写入 stdin 或关闭 stdin。
- `kill` 终止运行中的进程组，但保留最终记录。
- `clear` 只删除已结束记录。
- `remove` 可终止并删除运行中会话，也可删除已结束记录。
- 本轮不暴露 `send-keys/submit/paste`。

并发策略保持保守：只有 read 是并发安全工具，其他五个工具均形成独占屏障。

## 8. Agent Runtime 与调度分层

当前 AgentEngine 同时承担 Skill、Prompt、Provider、上下文、工具调度和 Reporter 职责。重构后拆为：

- `RunContextFactory`：Skill 发现、PromptComposer 和初始消息。
- `AgentLoop`：Thinking/Action 状态机、Provider 回合、上下文历史、终止条件。
- `ToolScheduler`：纯工具调度，负责 serial/parallel/mixed 波次和最大并发限制。
- `AgentRuntime`：向 app.Runner 提供完整默认 Run 门面。

ToolScheduler 不解析工具参数、不构造错误文本、不管理进程，也不处理 Provider 格式。并发事件可以交错，但最终 Tool Result Message 必须按原始 Tool Call 顺序写入历史。

Skills 依赖检查和提示词从 `read_file` 迁移到 `read`。

## 9. Provider 映射

### 9.1 OpenAI

- System/User/Assistant 文本块按顺序连接为文本内容。
- RoleTool 映射为原生 `tool` message。
- ToolCallID 映射为 `tool_call_id`。
- OpenAI Chat Completions 没有独立工具错误标记，因此 IsError 不上送；错误文本保留在 Content。
- Details 和 ToolUpdate 不进入请求。

### 9.2 Claude

- RoleTool 映射为用户消息中的 `tool_result` block。
- ToolCallID 映射为 `tool_use_id`。
- IsError 映射为 Claude `tool_result.is_error`。
- Details 和 ToolUpdate 不进入请求。

Provider 转换必须拒绝无效的内容组合，例如没有文本也没有 ToolCall 的 Assistant Message，或缺少 ToolCallID 的 RoleTool Message。

## 10. Reporter 与 Dispatch

Reporter 收敛为统一入口：

```go
type Reporter interface {
    Report(context.Context, schema.AgentEvent)
}
```

- Terminal 实时显示 exec 的 tool_update，并显示工具开始和最终状态。
- 企业微信忽略 tool_update，只推送工具开始、工具失败和 Agent 最终消息，避免刷屏。
- MultiReporter 按显式排序后的订阅顺序转发事件，并隔离单个 Reporter 的 panic；Reporter 接口不返回错误，不能中断 Agent 执行。
- 日志 Middleware 记录结构化摘要，不重复记录完整内容。

## 11. Fx 注入与内部扩展

本轮扩展契约保持在 internal。仓库内部通过 Fx Value Group 扩展：

- `group:"agent_tools"`
- `group:"tool_middlewares"`
- `group:"reporters"`

`internal/tools/register.go` 提供 Workspace、ProcessSupervisor、六个 Tool、默认 Middleware 和 Registry。Tool 构造函数分别通过 Fx 提供并加入 agent_tools Group，Registry 收集 Group，不在业务代码中直接 New 具体工具。

Fx Value Group 不作为稳定顺序来源。Middleware 和 Reporter 注册项必须携带 `Order` 与稳定 `Name`，收集后按 `Order`、`Name` 排序；相同 Tool Name 在 Registry 启动时直接报错。工具定义对模型仍按 Name 排序。

`internal/context/register.go` 在现有 PromptComposer 和 SkillLoader 之上提供 RunContextFactory；`internal/engine/register.go` 提供 AgentLoop、ToolScheduler 和 AgentRuntime。`app.Runner` 只依赖 AgentRuntime，不直接构造 PromptComposer、SkillLoader、Provider、Registry 或 Reporter。

Fx Stop Hook 按依赖逆序关闭 Workspace，并终止和清理所有后台进程。

## 12. 迁移顺序

1. 建立 ContentBlock、RoleTool、ToolOutput、ToolResult、ToolUpdate 和 AgentEvent。
2. 建立 Registry、Middleware、Emitter、JSON Schema 校验与测试替身。
3. 拆分 RunContextFactory、AgentLoop、ToolScheduler 和 AgentRuntime。
4. 更新 OpenAI、Claude、Terminal、MultiReporter 和企业微信 Dispatch。
5. 将现有底层算法迁移到六个新 Tool，并升级 edit 与 process 协议。
6. 改造 tools/engine 的 Fx Value Group 注册和资源生命周期。
7. 删除旧字符串 ToolResult、旧工具名、旧字段和临时迁移适配。
8. 更新 Skills 提示词、README、单元测试和跨包集成测试。

迁移可以在实现过程中短暂使用内部适配器保持分层验证，但最终代码不得保留双协议。

## 13. 验证与验收

必须覆盖：

- ContentBlock、RoleTool、ToolResult 和 AgentEvent JSON 契约。
- OpenAI tool message 与 Claude tool_result 转换。
- JSON Schema 未知字段、缺失字段和类型错误。
- Middleware 顺序、panic、普通错误和 Context 取消。
- ToolScheduler 的 serial/parallel/mixed 波次、最大并发和稳定结果顺序。
- exec 的 stdout/stderr 更新、yield 后台化、timeout 和进程组终止。
- process 的七个 action、日志分页和并发状态保护。
- Workspace 的绝对路径、`..`、Volume 路径和外部符号链接逃逸。
- edit 多替换的唯一性、原始内容匹配、重叠冲突和单次写入。
- Fx 依赖图、Workspace Close 和后台进程清理。
- Registry 最终只暴露六个新名称，旧名称全部不可用。
- Skills 存在时依赖 `read`，不再依赖 `read_file`。

最终验证命令：

```bash
go test -count=1 ./...
go test -race ./...
go vet ./...
git diff --check
```

## 14. 验收结果

满足以下条件时视为完成：

- main 通过 internal.Register 获得完整默认 AgentRuntime。
- app.Runner 只调用 AgentRuntime.Run。
- AgentLoop、ToolScheduler、Registry 和具体 Tool 职责独立。
- 六个工具使用新名称和新协议，旧协议不可调用。
- Tool Result 使用 RoleTool 和结构化文本内容进入模型上下文。
- exec 更新能够实时到达 Terminal，但不会刷屏企业微信或进入模型历史。
- 文件工具不能逃逸 WorkDir；Shell 非沙箱边界被明确记录。
- 所有单元、集成、race 和 vet 验证通过。
