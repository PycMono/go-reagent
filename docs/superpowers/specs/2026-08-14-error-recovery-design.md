# Pi Error Recovery 设计

## 状态

本设计已完成方案讨论，等待规格审阅。实现必须以本文档为准，不保留旧行为兼容层。

## 目标

为一次 `pi.Runner.Run` 增加三类恢复能力：

1. Provider 瞬态错误和限流错误的有限重试。
2. Provider 明确返回上下文超限后，对旧上下文生成一次摘要并重新请求。
3. 工具执行失败后，根据稳定错误码只向下一轮模型上下文注入可操作的 Recovery Hint。

错误恢复必须保持现有分层：Provider 负责识别厂商错误，Loop 负责模型调用和恢复编排，ToolRuntime 负责产生原始工具结果，Harness 负责上下文选择和工具领域错误。

## 非目标

第一版不实现：

- 统一处理所有异常的 `RecoveryManager`。
- Provider、Context 或 Tool Recovery 接口体系。
- 新的 Recovery 配置项。
- 模型容量表、`ContextWindow` 或 `MaxOutputTokens` 配置。
- 请求前主动压缩。
- 同一次模型请求的多轮或递归压缩。
- 工具自动重放。
- 进程退出后的 Session Resume。
- 未知工具错误的通用提示。
- 通过模糊匹配错误文本决定恢复动作。

## 当前问题

当前 `pi/ai.Provider` 只返回 `(*ai.Message, error)`。OpenAI 和 Anthropic Provider 把 API 错误统一包装成 `ai_generation_failed`，Loop 无法区分网络瞬断、限流、鉴权失败、配额耗尽和上下文超限。

Thinking 与 Action 直接调用 `Provider.Generate`，任何错误都会立即结束运行。Harness 没有上下文摘要逻辑。

ToolRuntime 会把工具错误转成 `ToolResult{IsError:true}`，Reporter 通过 ToolEnd Event 收到原始结果。Loop 随后使用同一条 Tool Message 同时更新模型上下文和 `RunResult.NewMessages`，目前没有仅面向模型的增强位置。

`recoveryMiddleware` 实际只捕获 panic，名称容易与本设计中的错误恢复混淆。

## 总体架构

采用分层恢复策略，不引入中央管理器：

```text
Provider SDK error
    -> Provider-specific extraction
    -> Shared provider classification
    -> pi/harness/errors.ErrorCode
    -> Loop retry / compact / terminate

Tool error
    -> ToolRuntime normalized ToolResult with ErrorCode
    -> Reporter receives raw ToolResult
    -> RunResult.NewMessages receives raw Tool Message
    -> Loop adds Recovery Hint only to model context
```

模型错误和工具错误共享现有 `ErrorCode` 类型，但恢复动作由各自所在层决定。错误码只描述失败事实，不携带 `Retryable` 等可能与错误码矛盾的布尔字段。

## 统一错误码

扩展 `pi/harness/errors.ErrorCode`，不增加 `GenerationFailure` 或 `ToolErrorCode`：

```go
const (
	ErrorCodeAITransient       ErrorCode = "ai_transient"
	ErrorCodeAIRateLimited     ErrorCode = "ai_rate_limited"
	ErrorCodeAIContextOverflow ErrorCode = "ai_context_overflow"
	ErrorCodeAIUnauthorized    ErrorCode = "ai_unauthorized"
	ErrorCodeAIQuotaExceeded   ErrorCode = "ai_quota_exceeded"
	ErrorCodeAIInvalidRequest  ErrorCode = "ai_invalid_request"

	ErrorCodeToolInvalidArguments ErrorCode = "tool_invalid_arguments"
	ErrorCodeToolResourceNotFound ErrorCode = "tool_resource_not_found"
	ErrorCodeToolPermissionDenied ErrorCode = "tool_permission_denied"
	ErrorCodeToolEditNoMatch      ErrorCode = "tool_edit_no_match"
	ErrorCodeToolEditNotUnique    ErrorCode = "tool_edit_not_unique"
	ErrorCodeToolTimeout          ErrorCode = "tool_timeout"
	ErrorCodeToolPanic            ErrorCode = "tool_panic"
)
```

现有 `ErrorCodeAIGeneration` 继续表示无法进一步识别的模型生成错误和模型响应契约错误。`ErrorCodeToolRuntime` 继续表示导致整个调度链失败的工具运行时错误。

删除专用的 `GenerationError` 和 `WrapGeneration`。Provider、CostTracker 和 Loop 统一使用现有 `Error`、`Wrap` 和 `ErrorCodeOf`。

## Provider 错误分类

### 两阶段分类

OpenAI 和 Anthropic 官方 SDK 使用不同错误类型，因此厂商错误对象的读取保留在各自 Provider 中。读取后的 HTTP 状态、厂商错误码和具体 cause 进入一套公共分类逻辑。

`pi/ai/providers/error.go` 定义私有规范化数据和公共分类函数：

```go
type providerErrorInfo struct {
	statusCode     int
	providerCode   string
	contextOverflow bool
	quotaExceeded   bool
	err             error
}

func classifyError(info providerErrorInfo) error
```

两个实现使用相同的方法名：

```go
func (p *OpenAIImpl) classifyError(err error) error
func (p *AnthropicImpl) classifyError(err error) error
```

这两个 receiver 方法只负责从对应 SDK 错误中构造 `providerErrorInfo`，随后共同调用包级 `classifyError`。不得在两个 Provider 中重复 HTTP 状态映射。

### 分类规则

公共分类遵循以下顺序：

| 条件 | ErrorCode | 行为 |
| --- | --- | --- |
| `context.Canceled` | `ErrorCodeCanceled` | 立即终止 |
| `context.DeadlineExceeded` | `ErrorCodeDeadlineExceeded` | 立即终止 |
| 厂商稳定错误码表示 Context Overflow | `ErrorCodeAIContextOverflow` | 压缩一次 |
| 厂商稳定错误码表示 Billing/Quota | `ErrorCodeAIQuotaExceeded` | 立即终止 |
| HTTP 429 | `ErrorCodeAIRateLimited` | 有限重试 |
| HTTP 408、409 | `ErrorCodeAITransient` | 有限重试 |
| HTTP 500–599 | `ErrorCodeAITransient` | 有限重试 |
| 标准网络、DNS、连接中断、EOF 错误 | `ErrorCodeAITransient` | 有限重试 |
| HTTP 401、403 | `ErrorCodeAIUnauthorized` | 立即终止 |
| 其他 HTTP 400 | `ErrorCodeAIInvalidRequest` | 立即终止 |
| 其他错误 | `ErrorCodeAIGeneration` | 立即终止 |

不得通过 `strings.Contains(err.Error(), ...)` 判断 Context Overflow、Quota 或瞬态错误。自定义 OpenAI-compatible Provider 如果不返回可识别的状态或稳定错误码，将得到 `ErrorCodeAIGeneration`，不会被猜测为可恢复错误。

OpenAI 和 Anthropic 官方 SDK 的内置重试必须关闭，防止 SDK 内部重试与 Loop 重试叠加。

## 模型重试

`pi/recovery.go` 增加 Loop 私有辅助方法：

```go
func (l *Loop) generateWithRetry(
	ctx context.Context,
	messages []ai.Message,
	tools []ai.ToolDefinition,
) (*ai.Message, error)
```

只重试：

- `ErrorCodeAITransient`
- `ErrorCodeAIRateLimited`

固定策略为初始请求加两次重试：

```text
attempt 1
    -> failure
wait 500 ms
attempt 2
    -> failure
wait 1 s
attempt 3
    -> return final result
```

第一版不解析或等待 `Retry-After`，也不增加 jitter 或外部配置。等待必须使用可被 `context.Context` 取消的 Timer，不得使用裸 `time.Sleep`。

Thinking、Action 和 Compaction 摘要调用共同使用 `generateWithRetry`。失败请求没有可靠 Usage，因此不生成 `ModelInvocation`；成功响应继续由 Loop 和 CostTracker 执行现有 Usage 校验和成本记录。

## Context Overflow 恢复

### 触发方式

不配置模型容量，也不主动估算当前模型是否接近上限。只有 Provider 返回 `ErrorCodeAIContextOverflow` 时才执行压缩。

一次模型请求的完整流程：

```text
generateWithRetry(messages)
    -> success: return
    -> context overflow:
         compact(messages) once
         generateWithRetry(compactedMessages)
         return success or final error
    -> other error: return
```

压缩后的请求如果仍然返回 Context Overflow，直接结束运行，不进行第二次压缩。

### 压缩计划

`pi/harness/compaction.go` 只负责选择消息和应用摘要，不调用 Provider：

```go
type CompactionPlan struct {
	SummaryMessages   []ai.Message
	PreservedMessages []ai.Message
}

func BuildCompactionPlan(messages []ai.Message) (CompactionPlan, error)
func ApplySummary(plan CompactionPlan, summary string) []ai.Message
```

选择规则：

1. 保留所有开头连续的 System Message。
2. 找到最近一条 User Message，并保留它以及它之后的全部当前轮消息。
3. Assistant ToolCall 和对应 Tool Result 必须作为完整消息组保留或摘要，不能从中间切断。
4. 最近 User Message 之前的非 System 历史作为摘要候选。
5. 摘要候选从较新的完整消息组向前选择，序列化后的输入最多 32 KiB。
6. 超过 32 KiB 的更旧历史不进入摘要，也不进入压缩后的上下文。

32 KiB 是 SDK 内部固定保护值，不是模型容量声明，也不开放配置。它的作用是避免把已经超限的完整历史再次原样发送给摘要请求。由于 SDK 不维护模型容量，无法保证所有未知模型都接受该大小；摘要调用如果仍然返回 Context Overflow，直接返回错误。

如果不存在可摘要的旧消息，例如单条当前用户消息本身已经超限，压缩无法解决问题，直接返回原始 Context Overflow。

### 摘要生成

Loop 根据 `CompactionPlan.SummaryMessages` 构造无工具的摘要请求。内部提示必须要求保留：

- 用户目标和明确要求。
- 已确认的决策和限制。
- 已修改的文件和关键标识符。
- 重要工具结果与错误码。
- 已完成工作和未完成工作。
- 精确文件路径、函数名和错误信息。

摘要不得回答用户问题或继续执行任务。

成功摘要以内部 System Message 形式替换旧历史：

```go
ai.Message{
	Role: ai.RoleSystem,
	Content: []ai.ContentBlock{
		ai.TextBlock("# Earlier conversation summary\n" + summary),
	},
}
```

该摘要只更新本次 Run 的 `contextHistory`，不进入 Reporter 或 `RunResult.NewMessages`。

摘要调用是实际模型调用。成功后新增：

```go
ModelInvocationPhaseCompaction ModelInvocationPhase = "compaction"
```

并按真实完成顺序记录摘要 Usage。摘要失败时返回其错误，不执行裁剪兜底或第二次摘要。

## Tool Error Recovery

### 结构化工具错误

Harness Tool 使用相同的 `pi/harness/errors.Error` 和 `ErrorCode`。标准错误按 `errors.Is` 分类：

- `fs.ErrNotExist` -> `ErrorCodeToolResourceNotFound`
- `fs.ErrPermission` -> `ErrorCodeToolPermissionDenied`
- `context.DeadlineExceeded` -> `ErrorCodeToolTimeout`

Edit Tool 使用明确错误码返回无匹配和多匹配错误。不得根据中文错误文本分类。

`ToolResult` 增加：

```go
ErrorCode pierrors.ErrorCode `json:"error_code,omitempty"`
```

ToolRuntime 必须在原始 `error` 仍存在时提取错误码，再生成 ToolResult。成功结果的 ErrorCode 为空值或 `ErrorCodeUnknown`，不得影响成功路径。

### 原始结果和模型上下文分离

Loop 对每个 ToolResult 构造两条用途不同的消息：

```go
rawMessage := toolResultMessage(result)
modelMessage := toolRecoveryMessage(rawMessage, result.ErrorCode)

newMessages = append(newMessages, rawMessage)
contextHistory = append(contextHistory, modelMessage)
```

因此：

- ToolEnd Event 和 Reporter 始终看到原始 ToolResult。
- `RunResult.NewMessages` 始终保存原始 Tool Message。
- Conversation 持久化不会保存 Recovery Hint。
- 只有下一轮发送给模型的 Tool Message 包含 Recovery Hint。

第一版只定义三个高价值提示：

| ErrorCode | Hint |
| --- | --- |
| `ErrorCodeToolEditNoMatch` | 先使用 `read` 获取文件最新内容，再用精确 `oldText` 编辑。 |
| `ErrorCodeToolEditNotUnique` | 增加 `oldText` 的相邻代码，保证匹配唯一。 |
| `ErrorCodeToolResourceNotFound` | 不要继续猜测路径，先检查真实目录结构和文件名。 |

未知错误不添加提示。框架不自动重新执行工具，因为工具可能有写入或外部副作用。

### Panic 命名

当前 `recoveryMiddleware` 改名为 `panicRecoveryMiddleware`，默认注册名称由 `recovery` 改为 `panic_recovery`。Panic 记录堆栈到日志，对模型和 Reporter 只暴露通用失败内容与 `ErrorCodeToolPanic`，不得泄露 panic 值。

## 数据与成本

恢复期间的数据归属：

| 数据 | Reporter | NewMessages | Model Context | Invocations |
| --- | --- | --- | --- | --- |
| 原始 Tool Result | 是 | 是 | 是 | 否 |
| Tool Recovery Hint | 否 | 否 | 是 | 否 |
| Compaction Summary | 否 | 否 | 是 | 否 |
| Compaction Usage | 否 | 否 | 否 | 是 |
| 失败的 Provider Attempt | 日志 | 否 | 否 | 无可靠 Usage 时不记录 |

运行在压缩后失败时，已经完成的 Assistant/Tool 原始消息和 Compaction Invocation 仍按现有部分结果语义返回。业务自行决定是否持久化部分结果。

## 可观测性

第一版不增加 Reporter Event 类型。使用结构化日志记录：

- 模型错误码、阶段、重试序号和等待时长。
- Context Overflow、摘要开始、摘要成功或失败。
- 压缩前消息数、摘要消息数和保留消息数。
- Tool ErrorCode 以及是否注入 Hint。

日志不得包含 API Key、完整 Provider 请求或 panic 原始值。

## 文件变更

新增：

```text
pi/recovery.go
pi/harness/compaction.go
pi/ai/providers/error.go
```

修改：

```text
pi/ai/providers/openai.go
pi/ai/providers/anthropic.go
pi/harness/errors/errors.go
pi/harness/errors/errors_test.go
pi/harness/tools/edit.go
pi/loop.go
pi/contract.go
pi/event.go
pi/middleware.go
pi/tool_runtime.go
pi/test/*
docs/sdk-architecture.md
```

不新增 Recovery 配置，不修改 `providers.Options` 的模型容量字段。

## 测试要求

### Provider 分类

1. OpenAI 和 Anthropic SDK 错误都通过同名 receiver 方法进入公共分类。
2. 408、409、429、5xx、标准网络错误得到正确 ErrorCode。
3. Context Overflow、Auth、Quota 和普通 400 得到正确 ErrorCode。
4. 未知错误返回 `ErrorCodeAIGeneration`。
5. 错误 cause 可通过 `errors.Is` 或 `errors.As` 继续获得。

### Retry

1. 瞬态错误失败两次后成功，总共调用三次。
2. 限流错误使用相同固定退避。
3. Auth、Quota、Invalid Request 不重试。
4. Context Overflow 不进入普通重试。
5. 退避期间取消能立即退出。
6. 重试耗尽后返回最后一次具体 cause 和稳定 ErrorCode。

### Compaction

1. 只有 Context Overflow 触发压缩。
2. 正常请求不会估算容量或主动压缩。
3. System Message 和当前 User Turn 被保留。
4. ToolCall 与 Tool Result 不被拆开。
5. 摘要输入不超过 32 KiB。
6. 摘要成功后原始请求只重新执行一次。
7. 摘要失败直接返回，不做第二套兜底。
8. 压缩后仍超限时直接返回，不再次压缩。
9. 摘要不进入 NewMessages，Usage 以 `compaction` Invocation 返回。

### Tool Recovery

1. ToolRuntime 从结构化错误和标准错误提取 ErrorCode。
2. Reporter 和 NewMessages 收到原始错误内容。
3. 下一轮 Provider Context 收到原始错误加 Hint。
4. Hint 不进入 NewMessages。
5. 未知错误不注入 Hint。
6. Panic 使用 `ErrorCodeToolPanic` 且不泄露 panic 值。

### 全量验证

```bash
go test ./... -count=1
go vet ./...
git diff --check
```

## 验收标准

1. Provider 瞬态失败可以在固定次数内恢复，取消始终优先终止。
2. Provider 明确报告 Context Overflow 时，SDK只压缩一次并重新请求一次。
3. SDK 不需要模型容量配置，也不使用错误字符串猜测错误类别。
4. Tool Recovery Hint 只影响模型下一轮行为，不污染 Reporter、RunResult 或数据库原始轨迹。
5. 所有恢复产生的成功模型调用继续经过 Usage 校验和成本统计。
6. 公共错误码稳定、错误 cause 可追踪，且没有新增万能 Recovery 抽象。
