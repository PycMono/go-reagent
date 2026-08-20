# Run Budget 与循环护栏设计

## 状态

方案已完成讨论，并根据当前 `pi` 实现、会话持久化契约以及 Claude Code、DeepSeek Harness、OpenClaw 的公开实现完成复核。2026-08-20 已根据需求审计补充早期终止、唯一运行入口、应用配置注入和 Web 流式提交契约。本文件等待修订后规格审阅；不包含代码实现。

## 目标

为一次 `pi.Runner.Run` 增加请求级运行预算，防止模型通过反复调用工具、反复自我纠正或压缩后重新进入旧循环而持续消耗资源。

第一阶段提供三项确定性上限：

- `MaxTurns`：一次 Run 最多进入多少个 Agent turn；
- `MaxCostUSD`：一次 Run 中所有已完成且可计量的模型调用累计成本；
- `MaxTotalTokens`：一次 Run 中所有已完成且可计量的模型调用累计输入、输出 Token。

同时建立结构化终止结果和完整的越界调用记账规则。行为型循环识别作为第二阶段增强，不能替代绝对预算。

## 当前实现事实

本设计基于当前代码，而不是抽象 Agent Loop 假设。

### Run 与 turn 边界

`pi.Agent.Run` 是同步、无状态的请求入口。它完成请求校验和 Context 构造后进入 `Loop.runDetailed`，并把 Loop 返回的业务消息和模型调用复制到 `RunResult`。

`Loop.runDetailed` 的外层 `for` 每次迭代是一个现有日志意义上的 turn。一个 turn 包含：

```text
可选 Thinking 模型调用
    -> Action 模型调用
    -> 无工具调用：完成 Run
    -> 有工具调用：执行完整工具批次并进入下一 turn
```

默认 `pi.Register` 启用 Thinking，因此一个 turn 通常包含两次成功模型调用；Web Chat 显式关闭 Thinking，因此通常每个 turn 只有一次 Action 调用。`MaxTurns` 必须约束外层 Agent turn，不能误当成物理模型调用次数。

### 已有成本总账

默认装配强制使用 `observability.CostTracker`。每个成功 Provider 响应会得到：

- `InputTokens`；
- `OutputTokens`；
- 配置快照中的输入、输出单价；
- `CostUSD`；
- `LatencyMS`；
- `PlatformID` 和 `Model`。

Loop 会再次执行 `validateMeteredUsage`，拒绝缺失、负数、NaN、无穷值和成本公式不一致的 Usage。成功 Thinking、Action、Compaction 随后按真实完成顺序生成 `ModelInvocation`。

当前 `ai.Usage` 不包含 cache read、cache write 或 reasoning token 独立字段。因此第一版 `MaxTotalTokens` 的准确语义只能是：

```text
SUM(InputTokens + OutputTokens)
```

不能把它命名为 `MaxBilledTokens`，也不能声称覆盖 Provider 未返回的 Token 类别。未来扩展 `ai.Usage` 后再同步扩展累计口径。

### Retry 与 Compaction

`generateWithRetry` 对瞬态错误和限流错误最多重试两次，等待 500ms、1s。Provider SDK 自身重试已经关闭。

失败响应目前没有可靠 Usage，`CostTracker` 也只计量 `err == nil` 的成功响应。因此预算只能约束“已完成且具有可信 Usage 的调用”；不能声称覆盖 Provider 可能收费但没有返回 Usage 的失败请求。重试次数已有固定上限，所以该缺口不会形成无限重试循环。

`generate` 遇到结构化 Context Overflow 时，会：

1. 构造一次 Compaction Plan；
2. 发起一次无工具的摘要模型调用；
3. 摘要成功后立即重试原 Thinking 或 Action 请求；
4. 通过 `generationResult.compactionUsage` 把摘要 Usage 返回外层记录。

这意味着只在外层 `for` 末尾检查预算是不正确的：Compaction 可能已经耗尽预算，但现有调用链会在返回外层前继续发起一次业务模型调用。

### 业务消息与模型调用不是同一份记录

`RunResult.NewMessages` 是调用方可以直接追加到会话存储的业务增量，只包含已完成的 Action 和 Tool 消息；Thinking、Compaction 和内部过渡消息不会进入其中。

`RunResult.Invocations` 是模型调用总账，包含 Thinking、Compaction、Action 的计量数据，不包含模型正文。

因此，预算达到后的 Action 响应不能一律写入 `NewMessages`：

- 没有工具调用的最终 Action 是一条完整业务消息，可以保留；
- 带工具调用但工具尚未执行的 Action 不能写入 `NewMessages`，否则会形成缺少对应 Tool Result 的残缺会话；
- 无论是否写入 `NewMessages`，只要该响应已成功计量，Invocation 都必须保留。

### 当前持久化缺口

`conversation.runner` 当前在 `runErr != nil && len(NewMessages) == 0` 时直接返回。若 Thinking 或 Compaction 恰好耗尽预算，Run 会只有 Invocation、没有业务消息，这条分支会丢失已经发生的成本总账。

第一阶段必须同步把持久化条件改为：

```text
没有 NewMessages 且没有 Invocations：可以直接返回
存在 NewMessages 或 Invocations：保存 Input、已有消息和全部 Invocations
```

该修复是预算正确性的组成部分，不是独立的会话功能扩张。

## 行业参考

调研基线为 2026-08-19：

- Claude Code：`anthropics/claude-code@c3d2e35e554060b5a20ee6b28140fbdbd4eb0048`；
- DeepSeek Harness：`deepseek-ai/DeepSeek-Harness@99f6f02fecdb7dff40c3fbc9470f5907c29f74ca`；
- OpenClaw：`openclaw/openclaw@ae55a4090c4e77260a67b86960589830fa03228d`。

### Claude Code / Agent SDK

Claude Agent SDK 提供 `max_turns`、`max_budget_usd`，并在结果中提供 `terminal_reason`、`num_turns`、`total_cost_usd`、累计 `model_usage` 以及 `error_max_turns`、`error_max_budget_usd` 等明确子类型。

客户端成本预算只能在响应返回后检查，因此允许越界一个完整调用。触发预算的调用仍进入累计成本和模型 Usage；达到预算后停止新任务和后台子 Agent。较新的 API-side `task_budget.total` 可以比客户端统计更早执行 Token 上限，但 go-reagent 当前 Provider 契约没有对应能力。

参考：

- <https://code.claude.com/docs/en/agent-sdk/python>
- <https://code.claude.com/docs/en/agent-sdk/cost-tracking>
- <https://code.claude.com/docs/en/headless>

### DeepSeek Harness

DeepSeek Harness 核心明确不内置 turn budget，部署方应通过 `agent/turn-stopping` 等 lifecycle extension 取消运行。它的优势是结构化生命周期和持久化终止原因：`completed`、`aborted`、`blocked`、`error`、`max-tokens`、`interrupted`。

其 `max-tokens` 是单次生成输出限制，不是整个 Run 的累计 Token 预算。被长度截断的响应会保留 Usage，但其中的工具调用不会执行。重复工具保护默认采用提醒阈值 `[3,5,8]`，属于模型可见的劝阻，不是绝对熔断。

参考：

- <https://github.com/deepseek-ai/DeepSeek-Harness/blob/99f6f02fecdb7dff40c3fbc9470f5907c29f74ca/packages/core/agent-loop/README.md#L129-L134>
- <https://github.com/deepseek-ai/DeepSeek-Harness/blob/99f6f02fecdb7dff40c3fbc9470f5907c29f74ca/packages/core/session/src/types.ts#L150-L177>
- <https://github.com/deepseek-ai/DeepSeek-Harness/blob/99f6f02fecdb7dff40c3fbc9470f5907c29f74ca/packages/guard/repeat-tool-reminder/README.md#L1-L35>

### OpenClaw

OpenClaw 没有通用的 max-turn 或 max-cost 契约，但具有分层超时和更成熟的行为循环检测。其检测覆盖：

- 相同工具和规范化参数重复；
- unknown-tool 重试；
- polling；
- 参数轻微抖动；
- A/B ping-pong；
- 稳定工具结果上的重复调用；
- Compaction 后重新进入同一 `(tool,args,result)` 循环。

OpenClaw 的成本累计主要用于终止时观测，而不是通用预算执行。这说明行为检测适合作为提前止损，绝对 turn/cost/token 预算仍需要独立存在。

参考：

- <https://github.com/openclaw/openclaw/blob/ae55a4090c4e77260a67b86960589830fa03228d/docs/tools/loop-detection.md>
- <https://github.com/openclaw/openclaw/blob/ae55a4090c4e77260a67b86960589830fa03228d/src/agents/tool-loop-detection.ts>
- <https://github.com/openclaw/openclaw/blob/ae55a4090c4e77260a67b86960589830fa03228d/src/agents/embedded-agent-runner/post-compaction-loop-guard.ts>

以上行业行为仅作为设计佐证，调研结论以固定 commit 和参考链接为准；go-reagent 的规范性行为只由本文后续契约定义，不依赖外部产品保持兼容。

## 设计原则

1. 预算属于一次 Run，所有状态必须是 request-local；共享的 `Agent` 和 `Loop` 继续支持并发 Run。
2. 所有成功且可信计量的模型调用都必须先记账，再判断是否允许继续。
3. 触发预算的 Invocation 不能因为 Run 返回错误而丢失。
4. 预算达到后，不得执行该响应中尚未开始的工具调用。
5. `MaxTurns` 约束逻辑 Agent turn；成本和 Token 约束物理模型调用。
6. `context.Context` 取消和 deadline 继续作为独立的时间边界，不混入额度结构。
7. 绝对预算是最终安全网；行为循环检测只能更早停止明显无进展的运行。
8. `Runner.Run` 的零 Limits 行为保持向后兼容；删除旧 `Loop.Run` 和 bundled application 强制非零配置是明确记录的 breaking change。

## 公共契约

### RunLimits

在 `pi/contract.go` 增加：

```go
type RunLimits struct {
    // MaxTurns 是外层 Agent turn 上限。0 表示不限制。
    MaxTurns int `json:"max_turns,omitempty" yaml:"max_turns,omitempty" toml:"max_turns,omitempty"`

    // MaxCostUSD 是所有已完成且可计量模型调用的累计美元成本上限。
    // 0 表示不限制。
    MaxCostUSD float64 `json:"max_cost_usd,omitempty" yaml:"max_cost_usd,omitempty" toml:"max_cost_usd,omitempty"`

    // MaxTotalTokens 是所有已完成且可计量模型调用的
    // InputTokens + OutputTokens 累计上限。0 表示不限制。
    MaxTotalTokens int64 `json:"max_total_tokens,omitempty" yaml:"max_total_tokens,omitempty" toml:"max_total_tokens,omitempty"`
}

type RunRequest struct {
    History []Message     `json:"history,omitempty"`
    Input   Message       `json:"input"`
    Context []ContextBlock `json:"context,omitempty"`
    Limits  RunLimits     `json:"limits,omitempty"`
}
```

校验规则：

- 三个值均不允许为负；
- `MaxCostUSD` 不允许 NaN、Inf；除有限且非负外，不复用单次 Invocation 的账本数值上界；
- `0` 只表示该维度不限制，不同时承担“使用默认值”的含义；
- 非法 Limits 在 Context 构造和任何 Provider 调用前返回 `request_invalid`。

单次输出上限必须继续由 Provider 参数表达，不能复用 `MaxTotalTokens`。Anthropic 当前固定 `MaxTokens: 4096`，OpenAI 当前没有在公共 Provider Options 中暴露同名配置；统一单次输出限制不属于本设计。

### RunTotals 与 Termination

`ai.Usage` 描述单次、单模型、单价格快照，不适合作为跨调用汇总类型。根 `pi` 增加独立汇总：

```go
type RunTotals struct {
    Turns          int     `json:"turns"`
    Invocations    uint32  `json:"invocations"`
    InputTokens    int64   `json:"input_tokens"`
    OutputTokens   int64   `json:"output_tokens"`
    TotalTokens    int64   `json:"total_tokens"`
    CostUSD        float64 `json:"cost_usd"`
}

type RunTerminationReason string

const (
    RunTerminationCompleted      RunTerminationReason = "completed"
    RunTerminationError          RunTerminationReason = "error"
    RunTerminationCanceled       RunTerminationReason = "canceled"
    RunTerminationDeadline       RunTerminationReason = "deadline_exceeded"
    RunTerminationMaxTurns       RunTerminationReason = "max_turns"
    RunTerminationMaxCost        RunTerminationReason = "max_cost"
    RunTerminationMaxTotalTokens RunTerminationReason = "max_total_tokens"
    RunTerminationLoopDetected   RunTerminationReason = "loop_detected"
)

type RunLimitKind string

const (
    RunLimitTurns       RunLimitKind = "turns"
    RunLimitCostUSD     RunLimitKind = "cost_usd"
    RunLimitTotalTokens RunLimitKind = "total_tokens"
)

type RunTermination struct {
    Reason RunTerminationReason `json:"reason"`
    Limit  RunLimitKind         `json:"limit,omitempty"`
    Totals RunTotals            `json:"totals"`
}

type RunResult struct {
    NewMessages []ai.Message      `json:"new_messages,omitempty"`
    Invocations []ModelInvocation `json:"invocations,omitempty"`
    Termination RunTermination    `json:"termination"`
}
```

`Termination.Reason` 对 `Agent.Run` 的每个返回路径都必须有值，包括进入 Loop 之前的失败。`Totals.Invocations` 必须等于 `len(Invocations)`；`TotalTokens` 必须等于 `InputTokens + OutputTokens`。累加使用检查型整数加法，不能静默溢出。

`ai.MaxUsageDecimalExclusive` 继续只约束单次 Invocation 的价格和成本字段。请求级 `MaxCostUSD` 与内存中的 `RunTotals.CostUSD` 都是跨 Invocation 值，可以超过该单次上界。成本继续沿用当前公共契约中的 `float64`，Governor 使用带补偿的浮点求和降低多次累加误差，并直接以 `>= MaxCostUSD` 判断，不使用可能放宽预算的 epsilon。累计结果出现 NaN 或 Inf 时按内部错误终止，不能继续运行。

### Loop 前的 Termination

Termination 是 `RunResult` 的公共契约，不以 Governor 已经创建为前提。`Agent.Run` 在任何早期返回前都必须填充 Reason，Totals 和 Invocations 保持零值：

| 返回路径 | Termination Reason | Totals |
| --- | --- | --- |
| 调用时 Context 已取消 | `canceled` | 全零 |
| 调用时 Context 已超过 deadline | `deadline_exceeded` | 全零 |
| RunRequest 或 Limits 非法 | `error` | 全零 |
| History/Input 转换失败 | `error` | 全零 |
| Workspace、AGENTS、Skills 或 Context 构造失败 | `error`，若 cause 是取消/deadline 则使用对应原因 | 全零 |
| Context 构造后、进入 Loop 前取消 | `canceled` 或 `deadline_exceeded` | 全零 |

实现可以使用不依赖 Governor 的 `terminationFromError(err, RunTotals{})` 一类私有辅助函数统一映射。非法 Limits 仍返回稳定的 `request_invalid` error code；Termination Reason 使用 `error`，不能伪装成某一种额度耗尽。

预算终止仍然返回非 nil error，因为现有契约中 `err == nil` 表示已经得到无需工具的最终 Assistant 响应。增加：

```go
ErrorCodeRunLimitExceeded ErrorCode = "run_limit_exceeded"
ErrRunLimitExceeded = errors.New("agent run limit exceeded")
```

调用方可以通过 `errors.Is(err, ErrRunLimitExceeded)` 或 `ErrorCodeOf(err)` 判断大类，通过 `RunResult.Termination.Reason` 判断具体额度。取消和 deadline 继续保持原来的 `errors.Is` 行为。

第一阶段不新增数据库 run 表；Termination 是否长期持久化属于上层业务策略。Invocation 总账必须持久化。

### 唯一公开运行入口

当前导出的 `(*Loop).Run` 直接进入状态机，只返回消息和 error，会丢弃 Invocations，也没有接收 Limits 或返回 Termination。它是一条无法满足本设计不变量的无预算旁路。

第一阶段删除导出的 `Loop.Run`。`runDetailed` 保持私有并只由 `Agent.Run` 调用；所有外部调用方统一使用：

```go
type Runner interface {
    Run(context.Context, RunRequest, Reporter) (RunResult, error)
}
```

当前仓库只有 `pi/test` 直接调用 `Loop.Run`，没有生产调用点。相关测试改为构造 `Agent` 后通过公共 `Runner.Run` 验证。`Loop` 和 `NewLoop` 可以继续导出用于组装 `Agent`，但 `Loop` 的注释必须明确：实例可并发复用，消息、计数、Governor 和其他 Run 状态只能保存在方法局部。

删除 `Loop.Run` 是有意的公开 API breaking change。它用于收敛为唯一 Run 契约，不能通过保留一个 unlimited 兼容入口规避。迁移方式是用已有的 `pi.New(...)` 构造 `Agent` 并调用 `Agent.Run`。

## Request-local Governor

增加根 `pi` 私有类型 `runGovernor`，每次 `Agent.Run` 创建一个实例。不得把 counters 放进共享的 `Agent`、`Loop`、Provider 或 CostTracker。

职责：

```go
type runGovernor struct {
    limits RunLimits
    totals RunTotals
}

func (g *runGovernor) beforeTurn() error
func (g *runGovernor) startTurn()
func (g *runGovernor) observe(ModelInvocation) error
func (g *runGovernor) termination(error) RunTermination
```

具体方法名可以调整，但必须维持以下边界：

- `beforeTurn` 在下一 turn 的 Thinking 之前检查 `MaxTurns`；
- `startTurn` 只在确定将进入该 turn 时递增；
- `observe` 接收已经通过 `validateMeteredUsage` 的 Invocation，先累加，再判断 cost/token 是否达到上限；
- 每个 Invocation 只能 observe 一次；
- Governor 不调用 Provider、不操作消息、不执行工具、不负责持久化。

CostTracker 继续负责产生可信单次 Usage；Governor 只负责跨调用累计和准入。两者不能合并，否则会把请求级可变状态放进并发共享 Provider decorator。

## 执行顺序

### Turn 上限

外层循环调整为：

```text
检查 ctx
    -> governor.beforeTurn()
    -> governor.startTurn()
    -> 可选 Thinking
    -> Action
    -> 工具批次或完成
```

判断使用“已开始 turn 数 >= MaxTurns”。例如 `MaxTurns=1`：

- 允许完整执行第一个 turn；
- 如果第一个 Action 产生工具调用，允许执行该工具批次并保存完整的 Action/Tool 消息；
- 在第二个 turn 的 Thinking 或 Action 发起前终止；
- 不会进入第 `MaxTurns+1` 个 Agent turn；同一 turn 内的 Thinking、Compaction、Action 物理调用仍由 cost/token 预算分别约束。

这种语义与当前 `turnCount` 日志一致，也不会把 Thinking 开关变化误解释为 turn 数变化。

### 每次成功模型调用

Thinking、Action、Compaction 均采用相同顺序：

```text
Provider 成功返回
    -> 校验消息结构
    -> 校验 Usage
    -> 创建并追加 ModelInvocation
    -> governor.observe(invocation)
    -> 达到预算：终止，不发起后续模型或工具调用
    -> 未达到预算：继续当前状态机
```

“先追加 Invocation，再检查预算”是硬性顺序。客户端预算只能在实际 Usage 返回后判断，因此允许最多越界一个成功模型调用；该调用必须出现在 `RunResult.Invocations` 和累计 Totals 中。

成本和 Token 使用达到语义，累计值 `>=` 配置上限即停止继续运行。这样即使实际值恰好等于上限，也不会再发起可能产生费用的后续步骤。

### Thinking 达到预算

Thinking 内容继续只存在于本 Run 的内部 Context，不进入 `NewMessages`。Thinking Invocation 被记录后立即返回 `run_limit_exceeded`，不得继续 Action。

### Compaction 达到预算

当前 `generationResult.compactionUsage` 返回外层太晚。实现必须把 Invocation 记录/预算观察能力传入 `generate`/`compact`，或者把模型调用重构为统一的受管入口。

正确顺序：

```text
原请求 Context Overflow
    -> Compaction 成功
    -> 校验并记录 Compaction Invocation
    -> governor.observe
       -> 达到预算：立即返回，不重试原请求
       -> 未达到预算：使用 compacted context 重试原请求
```

未来如果实现主动 Compaction，也必须走同一个观察入口。

### Action 达到预算

Action 需要按响应形态区分：

| Action 结果 | Invocation | NewMessages | 工具执行 | 终止 |
| --- | --- | --- | --- | --- |
| 无工具，未达到预算 | 保存 | 保存最终 Assistant | 无 | `completed` |
| 无工具，达到预算 | 保存 | 保存完整最终 Assistant | 无 | 对应预算原因 + error |
| 有工具，未达到预算 | 保存 | 保存 Action，随后保存 Tool Result | 执行 | 继续或下轮终止 |
| 有工具，达到预算 | 保存 | 不保存未完成 Action | 不执行 | 对应预算原因 + error |

带工具的越界 Action 虽然不进入 `NewMessages`，但其 Invocation 不能删除。第一阶段不新增保存模型原始越界正文的审计表。

Reporter 可能已收到 Action 的 `message_start` 和流式 `message_update`；预算判断发生在完整 Usage 返回之后，无法撤回已经发布的增量。只有被接受为业务消息的 Action 才发送 `message_end`。上层应以 Run 的最终结果和 error 作为完成事实，而不是把流式 delta 当作已提交消息。

### 工具批次

Scheduler 只在 Action 校验和预算检查通过后启动。预算达到后不得调用 `Scheduler.Schedule`，从而保证一个并行批次不会部分启动。

工具批次一旦开始，第一阶段不根据模型预算中途取消，因为三个预算都只统计 Agent turn 或模型 Usage，不统计工具自身费用。调用方 `context.Context` 仍可以取消正在执行的工具。未来若工具具有独立计费，应定义单独的工具预算提供者，不能偷偷并入 `MaxCostUSD`。

## 终止优先级

同一检查点出现多个终止条件时使用确定性顺序：

1. `context.Canceled`；
2. `context.DeadlineExceeded`；
3. Usage 或响应契约错误；
4. `MaxCostUSD`；
5. `MaxTotalTokens`；
6. 下一 turn 开始前的 `MaxTurns`；
7. 第二阶段的行为循环熔断；
8. 无工具 Action 正常完成。

终止原因的优先级不改变记账顺序：如果 Provider 已经返回一个可验证的成功响应，必须先保存该 Invocation 和 Totals，再选择 canceled、deadline 或预算原因。

Cost 和 Token 可能由同一 Invocation 同时达到。优先返回 `max_cost`，但 `Totals` 必须包含同一份完整 Token 与成本数据。该优先级只决定单一终止原因，不影响记账。

## 应用层策略

根 `pi` 是通用 SDK，`RunLimits{}` 保持全部不限制，避免在库层猜测不同业务的费用和任务复杂度。

这不意味着 bundled application 可以继续裸奔。Web/Conversation 装配必须从业务配置解析安全策略，并在构造 `pi.RunRequest` 时传入：

```text
config.Agent limits
    -> conversation runner 构造时注入的默认 RunLimits
    -> pi.RunRequest.Limits
```

具体配置与构造契约为：

```go
type AgentConfig struct {
    WorkspaceDir string       `json:"workspace_dir" yaml:"workspace_dir" toml:"workspace_dir"`
    Limits       pi.RunLimits `json:"limits" yaml:"limits" toml:"limits"`
}

func NewRunner(
    runtime pi.Runner,
    repository conversationrepo.IConversationRepository,
    historyLimit int,
    limits pi.RunLimits,
) Runner
```

`conversation.newRegisteredRunner` 从 `cfg.Agent.Limits` 取得已校验的值并传给 `NewRunner`；`runner.Run` 构造每次 `pi.RunRequest` 时复制该值。`conversation.RunRequest` 和 HTTP StartRun DTO 不增加 Limits 字段，外部客户端不能覆盖服务端策略。

外部 HTTP 客户端第一阶段不能自行提高服务端预算。若以后允许每请求覆盖，只能在服务端默认值以内向下收紧。

具体产品数值不属于 SDK 机制，本设计不硬编码统一美元预算；不同模型价格和业务任务跨度过大。bundled application 的三个配置值必须都大于零，缺失或任一值为零都在启动前返回 `config_invalid`。这项产品策略不改变根 `pi` 中 `0 = unlimited` 的 SDK 语义。

`context.Context` deadline 继续由 HTTP、任务或调用方生命周期设置，是独立 wall-clock 护栏，不加入 `RunLimits`。

### Web 流式提交语义

Reporter 的 `message.delta` 是临时展示，不是持久化提交。Web Chat 必须延续以下规则：

| Run 结果 | SSE 顺序 | 前端行为 | 持久化 |
| --- | --- | --- | --- |
| 带工具 Action 达到预算 | `message.started/delta -> run.failed`，无 `message.completed` | 删除 provisional Assistant | 只保存 Input 和 Invocation |
| 无工具完整 Action 达到预算 | `message.started/delta -> message.completed -> run.failed` | 保留已完成 Assistant，并展示预算终止提示 | 保存 Input、Assistant 和 Invocation |
| 正常完成 | `message.completed -> run.completed` | 保留已完成 Assistant | 正常保存 |

当前前端已在 `run.failed` 时调用 `discardStreamingMessage()`，并在 `message.completed` 后清空 provisional 状态；实现必须用回归测试固定这一行为。

Application Service 不能继续丢弃 `RunResult`。`executeRun` 必须接收 `result, err := s.runner.Run(...)`，并在 `run.failed` 中暴露安全的结构化终止原因。`RunErrorVO` 增加可选字符串字段：

```go
type RunErrorVO struct {
    Code    int    `json:"code"`
    Message string `json:"message"`
    Reason  string `json:"reason,omitempty"`
}
```

`Reason` 通常取自 `result.Termination.Reason`；但当 Termination 是 `completed` 而最终 `err != nil` 时，说明 Agent 已完成、失败发生在持久化或其他上层步骤，SSE Reason 必须改为 `error`，不得发送自相矛盾的 `run.failed + completed`。预算原因映射为明确但不包含价格、Token、模型响应正文的用户消息。普通内部错误继续使用安全的通用文本。第一阶段不通过 SSE 暴露完整 Totals，调用总账仍以持久化 Invocation 为准。

### 兼容性

- 向 `RunRequest` 增加零值 `Limits`、向 `RunResult` 增加 `Termination` 是字段级兼容扩展；现有 `Runner.Run` 调用在 Limits 全零时保持原有无限制执行行为。
- 删除 `Loop.Run` 是公开 API breaking change，迁移到唯一的 `Agent.Run`/`Runner.Run` 契约。
- bundled application 缺失任一正数 Limits 将无法启动，是有意的配置 breaking change；部署配置和 `config.example.json` 必须在同一版本更新。
- Conversation、HTTP 和 SSE 只新增服务端控制与可选响应字段，不接受客户端预算覆盖。

## 第二阶段：行为循环检测

绝对预算只能限定最大损失，不能尽早发现明显无进展的循环。第二阶段增加独立 `loopDetector`，保持 request-local。

### 提醒层

对 `(toolName, canonicalJSON(arguments))` 统计重复次数，在第 3、5、8 次向下一轮模型 Context 注入内部提醒。提醒不进入 `NewMessages`、Reporter 或会话持久化，不自动重放工具。

### 熔断层

维护有界滚动窗口，逐步覆盖：

- 完全相同的工具与参数；
- 相同稳定结果上的重复调用；
- A/B/A/B ping-pong；
- unknown-tool 持续重试；
- 只改变时间戳、随机 ID 等易变字段的参数抖动；
- 无状态变化的轮询。

熔断必须发生在下一批工具启动前。第一次达到 critical 阈值可以阻止该工具批次并允许模型生成一次恢复回答；再次达到 critical 阈值终止 Run。具体阈值与 canonicalization 规则在第二阶段单独设计和测试，避免第一阶段为了“智能检测”延迟绝对预算落地。

### Post-compaction guard

Compaction 会删除部分近期失败细节，模型可能重新进入压缩前的同一循环。Detector 状态不能被 Context Compaction 清空。

对 Compaction 后观察到的 `(toolName, canonicalArgs, stableResult)` 保存运行内签名；连续三次重复时直接以 `loop_detected` 终止。这一保护默认启用，借鉴 OpenClaw 的 post-compaction guard。

## 非目标

第一阶段不实现：

- Provider API-side Token budget；
- Provider 请求前的精确成本预测；
- 失败且无可靠 Usage 的模型请求估价；
- 工具自身费用预算；
- 子 Agent 或跨 Run 共享预算；
- 单次输出 Token 配置统一；
- wall-clock timeout 配置；
- 数据库 run/termination 表；
- 行为循环 Detector 的第二阶段实现；
- 根据用户提示自动提高预算；
- 预算耗尽后自动发起“最后总结”模型调用。

预算耗尽后再调用模型生成总结会继续产生未授权费用，因此第一阶段只返回已经完成的业务消息、Invocations、Termination 和 error。

## 测试设计

### Limits 校验

1. 负数 turn/token、负数/NaN/Inf 成本在 History/Input 转换、ContextBuilder 和 Provider 调用前返回 `request_invalid`。
2. 三项为零保持当前无限制行为。
3. 调用方传入的 Request/Limits 不被修改。
4. 使用会失败的 Workspace 构造器时，非法 Limits 仍优先返回 `request_invalid`，证明没有读取 AGENTS/Skills 或构造 Context。

### Turn

1. `MaxTurns=1` 的直接回答正常进入一个 turn。
2. 第一个 Action 调用工具后，工具结果完整返回；第二 turn 的 Thinking/Action 均不启动。
3. Thinking 开启和关闭时，同样的外层循环次数得到相同 turn 计数。
4. 达到 turn 上限时返回已有消息、Invocations、Totals 和 `run_limit_exceeded`。

### Cost 与 Token

1. Thinking 达到成本上限后不调用 Action。
2. Thinking 达到 Token 上限后不调用 Action。
3. Action 无工具但达到预算时保留最终 Assistant 和 Invocation，并返回预算错误。
4. Action 带工具且达到预算时不执行任何工具、不返回残缺 Action，但保留 Invocation。
5. 单次调用跨越预算时 Totals 保存实际越界值，不截断为配置值。
6. 同一调用同时达到 cost/token 时按既定优先级返回 `max_cost`，两项累计均正确。
7. 每个 Invocation 只累计一次，Sequence 与当前调用顺序保持一致。

### Retry 与 Compaction

1. 瞬态失败后成功的调用只按当前可靠 Usage 记录成功响应，重试次数仍最多两次。
2. Compaction 达到预算后不重试原 Thinking/Action。
3. Compaction 未达到预算时，重试调用继续受同一 Governor 约束。
4. Compaction 成功而重试失败时，Compaction Invocation 和 Totals 仍返回。
5. Thinking 与 Action 两条 Context Overflow 路径都执行相同预算检查。

### Result 与持久化

1. 预取消、deadline、非法 Request、非法 Limits、Context 构造失败和所有 Loop 返回路径都有非空 Termination Reason。
2. `Totals.Invocations == len(Invocations)`，Token 与成本汇总等于 Invocation 明细。
3. 只有 Invocation、没有 NewMessages 的预算错误仍通过 Conversation Runner 保存 Input 和总账。
4. 没有消息也没有 Invocation 的早期失败继续不追加 turn。
5. 持久化失败与运行预算错误继续通过 `errors.Join` 同时返回。
6. 并发 Run 的 Governor、Totals、Termination 互不污染，并通过 `go test -race`。

### 公共入口与应用装配

1. 根 `pi` 不再暴露能直接执行状态机的 `Loop.Run`；所有运行测试通过 `Agent.Run`。
2. bundled application 缺失或任一非正数 Limits 时配置校验失败，三个正数配置能原样进入 `pi.RunRequest`。
3. HTTP DTO 不能提交或覆盖 Limits。
4. `run.failed` 包含安全的 Termination Reason；预算达到的 provisional 消息被丢弃，已完成并持久化的最终消息被保留。
5. Agent Termination 为 `completed` 但持久化失败时，SSE 使用 `reason=error`，不会产生 `run.failed + completed`。

### Reporter

1. 带工具的越界 Action 可以产生 start/update，但不产生 message_end 或 tool 事件。
2. 无工具的完整越界 Action 产生 message_end。
3. Reporter panic 继续与运行结果隔离。

## 实施边界

第一阶段预计涉及：

- `pi/contract.go`：Limits、Totals、Termination 公共类型；
- `pi/agent.go`：Limits 校验、创建 request-local Governor、统一终止结果；
- 新增 `pi/governor.go`：累计与预算判断；
- `pi/loop.go`：删除公开 `Loop.Run`、修正可复用实例注释、增加 turn 前置检查、模型调用后的观察和 Action 接受顺序；
- `pi/recovery.go`：Compaction 记录后、原请求重试前执行预算检查；
- `pi/harness/errors/errors.go`：稳定 limit error code 和 sentinel；
- `config/config.go`、`config/validate.go`、`config.example.json`：声明并校验 bundled application 的非零 Limits；
- `conversation/runner.go`、`conversation/register.go`：构造时注入 Limits，只有 Invocation 时仍保存总账；
- `application/service/chat/run_manager.go`、`common/vo/chat.go`：保留 RunResult 并把安全的 Termination Reason 写入 `run.failed`；
- `frontend/static/js/pages/chat.js` 及对应测试：固定 provisional 丢弃和 completed 保留语义；
- 对应 `pi/test`、Conversation、Config、Application、Frontend 测试与 SDK 文档。

不需要修改 ToolRuntime、Scheduler、具体工具和 Provider SDK adapter 的核心职责。CostTracker 继续是单次调用计量的唯一入口，Governor 只消费其结果。

## 完成标准

第一阶段完成后必须满足：

1. 任意工具自纠正循环都无法超过配置的 Agent turn 数。
2. 每个成功且可计量的 Thinking、Action、Compaction 都参与同一 Run 的成本和 Token 预算。
3. Compaction 无法绕过预算后继续发起原请求。
4. 达到预算的带工具 Action 不会启动工具，也不会污染可持久化消息序列。
5. 触发预算的 Invocation 始终可从 `RunResult` 获取，并在 bundled conversation 路径落入总账。
6. 调用方可以仅通过稳定 error code 和结构化 Termination 区分正常完成、取消、deadline、普通错误及三种预算终止。
7. 现有 `Runner.Run` 的零 Limits 调用保持行为兼容；bundled application 要求三个 Limits 都为正数。
8. 根 `pi` 不保留无 Limits、无 Invocation、无 Termination 的公开 Loop 执行旁路。
9. Web 对未提交的越界流式消息和已提交的完整越界消息采用确定且经过测试的不同展示策略，并能显示安全的预算终止原因。
10. 相关单元测试、全量测试和 race test 通过。
