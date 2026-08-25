# Tool Loop Detection 设计

## 状态

方案已在对话中完成架构选择，并根据当前 `pi` 的 Agent Loop、`governor`、Compaction、Scheduler、子代理与消息持久化契约，以及 pi.dev、OpenClaw 的公开实现完成复核。

2026-08-25 需求审计修订：补全 warning ephemeral context 的生成、重试、Compaction 与回写生命周期；形式化 Ping-pong 稳定条件；固定 Tool Result 对齐失败的 Run 级错误语义；明确窗口淘汰、post-compaction 漏报、易变结果文本、依赖方向与配置校验边界。架构选择不变。

本文等待规格审阅，不包含代码实现。用户确认本文后，再单独编写实施计划。

本文是 [2026-08-19 Run Budget 与循环护栏设计](./2026-08-19-run-budget-loop-guardrails-design.md) 中“第二阶段：行为循环检测”的详细规格，并取代该节的概要描述；已经落地的 `MaxTurns`、`MaxCostUSD`、`MaxTotalTokens`、Invocation、Totals、父子预算与 Termination 设计不迁移、不重写。

## 决策摘要

本次增加独立的请求级行为检测器：

```text
pi/loopdetect
```

它只回答“当前工具行为是否呈现重复且无进展的模式”，不拥有 Agent Loop，也不累计 Token、成本或轮次。

现有职责保持不变：

- `pi/governor`：确定性资源预算、模型调用账本、父子预算传播和最终 `Termination`；
- `pi/loopdetect`：工具调用与结果的行为证据、提醒、一次恢复机会和循环熔断干预；
- `pi.Loop`：唯一控制流所有者，按确定性优先级调用两者并决定消息是否提交、工具是否执行、Run 是否退出。

核心 API 使用明确的准入/记账命名，而不是含义宽泛的 `BeforeBatch`/`AfterBatch`：

```go
func (d *Detector) AdmitToolBatch(
    calls ai.ToolCalls,
) Admission

func (d *Detector) RecordToolBatchOutcome(
    calls ai.ToolCalls,
    results []toolexec.Result,
) *Intervention

func (d *Detector) ArmPostCompaction()
```

`Admit` 表示副作用发生前的原子准入；`Record` 表示副作用完成后的事实记账。方法名直接表达调用时机和是否允许产生副作用。

## 目标

1. 在绝对预算耗尽前识别明显无进展的工具循环，减少无效模型调用与工具副作用。
2. 对一次 Action 返回的整批 Tool Calls 做原子准入，禁止同一批次部分执行。
3. 先提醒模型自我纠正；第一次确认严重循环时阻止工具批次并给予一次恢复 turn；恢复失败后终止 Run。
4. 用工具结果判断是否真的取得进展，避免仅凭“参数相同”误杀合法轮询。
5. Detector 状态独立于模型上下文，在 L1/L2 Compaction 后继续有效。
6. 对 Compaction 后立即复发的相同调用与相同结果提供短窗口硬熔断。
7. 保持 `governor` 为唯一 Totals 与最终 Termination 来源，不形成第二套预算或终止系统。
8. 主代理与每个子代理使用相同策略、独立状态，避免不同 Run 之间相互污染。

## 非目标

第一版不实现：

- 把 `MaxTurns`、`MaxCostUSD`、`MaxTotalTokens` 迁入 `loopdetect`；
- 把所有退出条件合并为统一 `RunController`；
- 跨 Run、跨会话或跨进程共享循环历史；
- 检测没有工具调用的纯文本生成循环；
- 对参数做工具专属的语义归一化，例如自动忽略时间戳、随机 ID、游标或 PID；
- 为 polling、exec、message 等工具编写专属检测器；
- 用 LLM 判断两个参数或结果是否“语义相同”；
- 中途取消一个已经开始执行的并行工具批次；
- 新增 Hook、人工审批或动态插件系统；
- 在数据库中持久化原始参数、原始结果或 Detector 状态；
- 允许外部客户端按请求提高服务端循环策略阈值。

参数抖动和工具专属归一化只有在真实误报/漏报样本出现后再扩展。第一版先固定一个可解释、可复现、无额外模型成本的算法。

## 当前实现事实与边界

### Agent Loop 的退出机制本来就不止一种

当前 `pi/loop.go` 的外层 `for` 是控制流，不是一种策略。Run 可以因以下事件结束：

| 退出来源 | 检查点 | 当前所有者 | 最终结果 |
| --- | --- | --- | --- |
| 调用方取消/deadline | turn 前、生成与工具执行路径 | `context.Context` | `canceled` / `deadline_exceeded` |
| `MaxTurns` | 进入下一 turn 前 | `governor` | `max_turns` |
| `MaxCostUSD` / `MaxTotalTokens` | 每次可信 Invocation 入账后 | `governor` | `max_cost` / `max_total_tokens` |
| Provider/响应契约错误 | 模型调用和 Action 校验后 | `Loop` | `error` |
| 无 Tool Calls 的完整 Action | Action 校验与预算检查后 | `Loop` | `completed` |
| Scheduler/工具基础设施错误 | 工具批次结算后 | `Loop` / Scheduler | `error` |
| 行为循环 | 工具批次准入前或结果完成后 | `loopdetect` 提供证据，`Loop` 执行 | `loop_detected` |

这些机制的算法和状态来源不同，但都通过同一个 `Loop.finish` 返回，并由现有 `governor.TerminationFromError` 生成唯一结构化终止结果。增加 Detector 不等于增加第二个 Loop，也不需要把不同性质的策略强行塞进同一类型。

### 当前工具消息提交时机需要调整

当前 `executeTurn` 在 Tool Calls 固有校验和工具调度之前，已经把带 Tool Calls 的 Assistant 写入：

- `state.contextHistory`；
- `state.newMessages`；
- `message_end` 事件。

循环准入可能阻止整批工具，因此实现时必须把 Tool Calls 校验和 Detector 准入提前到带工具 Assistant 的提交之前。否则终止路径会持久化一条没有对应 Tool Results 的残缺消息。

### Scheduler 已提供原子启动边界

`toolexec.Scheduler.Schedule` 是整批工具产生副作用的入口。Detector 只需在调用它之前完成准入；进入 Scheduler 后不再按循环规则取消批次。并行工具可能已经全部开始，中途阻止既不可靠，也无法撤销副作用。

### Compaction 与 Detector 是不同状态域

Compaction 只改变 `state.contextHistory` 和 `compactionRuntime`。Detector 必须由每次 `Loop.run` 单独创建并保存在方法局部，不能放进消息历史、共享 `Loop`、共享 Agent、Provider 或全局变量。

## 行业实现复核

### pi.dev

复核基线：`pi@c5de2cc67f04d2e700617f9452a22a4242aaa1a4`。

pi.dev 的 Agent SDK 使用：

- `beforeToolCall`：工具调用前允许阻断；
- `afterToolCall`：工具完成后观察/修改结果；
- `shouldStopAfterTurn`：turn 结束后决定是否停止。

Coding Agent extension 层对应 `tool_call`、`tool_result`、`turn_end` 等事件。它提供通用拦截点，但没有公开的批次准入 API，也没有内置的通用 Tool Loop Detector。

这说明本项目不应把循环检测伪装成 `pi/event.go` 的观察事件：循环准入必须有返回值并发生在副作用之前。`EventListener` 继续负责流式观察，Detector 是请求内策略对象。

参考：

- <https://github.com/earendil-works/pi/blob/c5de2cc67f04d2e700617f9452a22a4242aaa1a4/packages/agent/src/agent.ts>
- <https://github.com/earendil-works/pi/blob/c5de2cc67f04d2e700617f9452a22a4242aaa1a4/packages/agent/src/agent-loop.ts>
- <https://github.com/earendil-works/pi/blob/c5de2cc67f04d2e700617f9452a22a4242aaa1a4/packages/coding-agent/docs/extensions.md>

### OpenClaw

复核基线：`openclaw@d647a9b6a7772ca595ca775e3ecfec09ef2f1aeb`。

OpenClaw 使用的相关命名包括：

- `detectToolCallLoop`；
- `recordToolCall`；
- `recordToolCallOutcome`；
- `admitToolCallBatch`；
- `InternalBeforeToolBatchHook`；
- `createToolLoopBatchAdmission`；
- `createPostCompactionLoopGuard`；
- `armPostCompaction` / `observe`。

OpenClaw 没有把 retry、timeout、fallback、compaction、预算和 loop guard 合并成统一 Run Controller，而是让运行器显式编排多个边界清楚的机制。它的批次准入与 post-compaction guard 直接支持本设计的两个关键选择：

1. 副作用前用 `AdmitToolBatch` 做整批原子决策；
2. 成功 Compaction 后独立 arm 一个只有 3 次观察机会的短窗口。

本项目采用其清晰的命名与分层思路，不复制其 TypeScript SessionState、工具专属规则、阈值或产品文案。

参考：

- <https://github.com/openclaw/openclaw/blob/d647a9b6a7772ca595ca775e3ecfec09ef2f1aeb/docs/tools/loop-detection.md>
- <https://github.com/openclaw/openclaw/blob/d647a9b6a7772ca595ca775e3ecfec09ef2f1aeb/src/agents/tool-loop-detection.ts>
- <https://github.com/openclaw/openclaw/blob/d647a9b6a7772ca595ca775e3ecfec09ef2f1aeb/src/agents/tool-loop-admission.ts>
- <https://github.com/openclaw/openclaw/blob/d647a9b6a7772ca595ca775e3ecfec09ef2f1aeb/src/agents/embedded-agent-runner/run/tool-loop-recovery.ts>
- <https://github.com/openclaw/openclaw/blob/d647a9b6a7772ca595ca775e3ecfec09ef2f1aeb/src/agents/embedded-agent-runner/post-compaction-loop-guard.ts>
- <https://github.com/openclaw/openclaw/blob/d647a9b6a7772ca595ca775e3ecfec09ef2f1aeb/packages/agent-core/src/internal-hooks.ts>

## 包与文件结构

新增：

```text
pi/loopdetect/
├── config.go                    # Config、默认/固定策略和防御性归一化
├── types.go                     # Admission、Decision、Intervention、Pattern、Level
├── fingerprint.go               # Call/Outcome canonicalization 与 SHA-256
├── detector.go                  # 历史窗口、准入、提醒和恢复状态机
├── post_compaction.go           # Compaction 后 3-outcome guard
├── errors.go                    # typed Error 与 sentinel
├── fingerprint_test.go
├── detector_test.go
└── post_compaction_test.go
```

集成时修改：

```text
pi/loop.go
pi/recovery.go
pi/compaction.go
pi/register.go
pi/governor/governor.go
pi/harness/errors/errors.go
config/config.go
config/load.go
config/validate.go
cmd/server/app.go
application/service/chat/run_manager.go
```

并补充这些文件对应的测试。`pi/governor` 目录不重命名，`pi/loopdetect` 不接收 `governor.Limits`、Invocation 或 Totals。

## 外部配置

第一版只开放启用开关和精确工具排除列表，阈值保持包内常量，避免未经数据验证就形成难以兼容的公共调参面。

```go
package loopdetect

type Config struct {
    Enabled       bool     `json:"enabled" yaml:"enabled" toml:"enabled"`
    ExcludedTools []string `json:"excluded_tools" yaml:"excluded_tools" toml:"excluded_tools"`
}
```

业务配置增加：

```go
type AgentConfig struct {
    WorkspaceDir  string            `json:"workspace_dir" yaml:"workspace_dir" toml:"workspace_dir"`
    Limits        governor.Limits   `json:"limits" yaml:"limits" toml:"limits"`
    LoopDetection loopdetect.Config `json:"loop_detection" yaml:"loop_detection" toml:"loop_detection"`
}
```

示例：

```yaml
agent:
  limits:
    max_turns: 50
    max_cost_usd: 1
    max_total_tokens: 200000
  loop_detection:
    enabled: true
    excluded_tools:
      - process_poll
```

配置语义：

- SDK 与 bundled application 的零值均为关闭，保持兼容；示例部署显式开启；
- `excluded_tools` 使用最终暴露给模型的精确工具名匹配，大小写敏感，不支持 glob 或正则；
- bundled application 的空白名称、前后空格和重复项在 `config` 包的 `normalizeAndValidate` 阶段 fail-fast；
- 不要求排除项一定已注册，因为 MCP/extension 工具可能到启动期才完整出现；
- 被排除的调用不进入任何历史、提醒、critical 或 post-compaction 计数；
- 混合批次只检测未排除调用，但只要其中一个触发阻断，仍按整批原子规则处理全部调用。

业务配置校验只在根 `config` 包执行，不在 `pi/loopdetect` 复制第二套校验。直接使用 SDK 时，`loopdetect.New` 不返回配置错误，只复制配置切片，并对 `ExcludedTools` 做 trim、去空和去重的防御性归一化；这些操作不得修改调用方传入的 Config。

固定第一版参数：

```go
const (
    historySize                   = 16
    warningThreshold              = 3
    criticalThreshold             = 5
    globalCircuitBreakerThreshold = 8
    postCompactionWindowSize      = 3
)
```

固定常量可以通过包内测试演进；对外开放阈值需要先积累误报、漏报和平均止损数据，不在第一版提前设计。

## 公共类型与方法契约

公共契约固定为：

```go
package loopdetect

type Decision string

const (
    DecisionAllow     Decision = "allow"
    DecisionWarn      Decision = "warn"
    DecisionRecover   Decision = "recover"
    DecisionTerminate Decision = "terminate"
)

type Level string

const (
    LevelWarning     Level = "warning"
    LevelRecovery    Level = "recovery"
    LevelTermination Level = "termination"
)

type Pattern string

const (
    PatternRepeatedCall       Pattern = "repeated_call"
    PatternStableOutcome      Pattern = "stable_outcome"
    PatternPingPong           Pattern = "ping_pong"
    PatternGlobalCircuit      Pattern = "global_circuit_breaker"
    PatternPostCompaction     Pattern = "post_compaction_repeat"
)

type Intervention struct {
    Level     Level
    Pattern   Pattern
    Count     int
    ToolNames []string
}

type Admission struct {
    Decision     Decision
    Intervention *Intervention
}

func New(config Config) *Detector

func (d *Detector) AdmitToolBatch(calls ai.ToolCalls) Admission

func (d *Detector) RecordToolBatchOutcome(
    calls ai.ToolCalls,
    results []toolexec.Result,
) *Intervention

func (d *Detector) ArmPostCompaction()
```

命名职责固定为：

- `AdmitToolBatch`：执行副作用前的准入检查，沿用 OpenClaw `admitToolCallBatch` 的 admission-control 语义；
- `Admission`：一次整批准入的完整返回值；
- `Decision`：Loop 必须执行的明确决定；
- `Intervention`：产生 Warn/Recover/Terminate 决定的安全行为证据。

pi.dev 使用的是 `beforeToolCall` + `BeforeToolCallResult.block` 生命周期命名，没有 `Admission` 类型；本设计不是声称与 pi.dev 同名，而是针对批次原子准入采用 OpenClaw 的术语，并用 Go 显式 `Decision` 代替隐式的 nil/undefined 分支。

约束：

- `DecisionAllow` 的 `Intervention` 必须为 nil；
- `DecisionWarn` 允许工具批次执行，并携带一次只对模型可见的提醒证据；
- `DecisionRecover` 和 `DecisionTerminate` 都不允许任何工具开始；
- `RecordToolBatchOutcome` 要求 `len(calls) == len(results)`、`calls[i].ID == results[i].ToolCallID` 且工具名一致；Loop 在调用前检查该不变量，违反时执行“结果对齐失败”终止协议，不进入 Detector；Detector 自身对不匹配输入不修改状态并返回 nil；
- `RecordToolBatchOutcome` 在普通记账时返回 nil，只在完成结果后需要立即干预时返回值；第一版的立即干预只有 post-compaction 终止；
- Config 关闭时，`AdmitToolBatch` 恒返回 `DecisionAllow`，另外两个方法无副作用；
- Detector 不暴露内部 hash、参数或结果，也不返回最终 `governor.Termination`。

`Intervention.ToolNames` 必须去重并按字典序排序，确保日志和测试稳定。它只含模型已经知道的工具名，不含参数、结果、路径、命令、URL 或消息正文。

## 指纹规则

### Call signature

```text
SHA256(toolName + NUL + canonicalJSON(arguments))
```

`canonicalJSON` 规则：

1. `json.Decoder.UseNumber()` 解码，避免先转换为 `float64` 造成大整数精度丢失；
2. 确认只有一个 JSON value，禁止尾随 token；
3. object key 使用 UTF-8 字典序稳定输出；
4. array 顺序保留；
5. string、boolean、null 按标准 JSON 编码；
6. number 保留 `json.Number` 的合法十进制文本，因此 `1` 与 `1.0` 第一版视为不同参数；
7. 忽略 `ToolCall.ID`。

Tool Calls 在调用 Detector 前已经通过 `ai.ToolCalls.Validate()`，因此非法 JSON 属于 Loop 的 Action 契约错误，不由 Detector 降级处理。

Go 的实现可以递归规范化后使用 `json.Marshal`；测试必须证明 object key/空白变化不改变签名，并证明数组顺序、字符串值和数字文本变化会改变签名。

### Outcome signature

```text
SHA256(
    callSignature + NUL +
    IsError + NUL +
    ErrorCode + NUL +
    each(Content.Type + NUL + Content.Text)
)
```

纳入：

- Call signature；
- `IsError`；
- 稳定 `ErrorCode`；
- Content Block 的顺序、Type 和 Text。

不纳入：

- `Details`；
- Usage、duration、PID、timestamp；
- ToolCallID、request ID、trace ID；
- 原始 Arguments；
- Go error 文本或未分类内部错误对象。

不保存 raw canonical JSON 或 raw result，只在内存中保存固定长度 hash 与安全元数据。这样既可识别相同结果，又避免 Detector 成为第二份敏感数据账本。

完整 `Content.Text` 会保留正确的“结果变化即进展”语义，但也带来有意接受的漏报：如果工具每次在正文中加入时间戳、随机 ID、临时路径等易变文本，Outcome signature 将持续变化，stable-outcome 与 global circuit 都不会 critical。第一版只会发出 repeated-call warning，最终由 `MaxTurns`、`MaxTotalTokens`、`MaxCostUSD` 兜底；工具专属 result canonicalizer 留待真实样本驱动。

## 请求内状态

Detector 至少维护：

```text
最近 16 条未排除 Tool Call 记录
    - toolName
    - callSignature
    - optional outcomeSignature
    - outcomeRecorded
    - loopVeto

每个 callSignature 的最近稳定结果与稳定次数
已发送 warning key 集合
criticalInterventions 计数
post-compaction guard 状态
```

内部历史按模型返回的 Tool Calls 原始顺序记录，不按并发工具实际完成顺序记录。这样相同输入在 serial/parallel/mixed Scheduler 模式下得到相同判定。

结果变化代表进展：同一 `callSignature` 的新 `outcomeSignature` 与上次不同，就把该 signature 的 stable-outcome streak 重置为 1，并清除与该 signature 相关的 warning 抑制状态。其他 signature 的状态不受影响。

历史超过 16 条时丢弃最旧记录。某个 Call signature 不再被窗口中任何记录引用时，必须删除它的 stable outcome signature、stable count、repeated-call warning key 和 Ping-pong 辅助状态；Run 级 `criticalInterventions` 与当前 post-compaction window 不随普通历史淘汰而重置。Detector 的内存上限与 Run 长度无关。

因此，间隔超过 16 条工具记录才复现一次的慢循环可能漏报，这是维持请求内存有界的有意取舍；绝对预算继续作为最终兜底。

## 第一版检测算法

### 1. Repeated call warning

在最近 16 条历史中，对同一 Call signature 的出现次数计数，并把本批当前调用计入 projected count。

- projected count 小于 3：无干预；
- projected count 达到 3：返回一次 `DecisionWarn`；
- 之后相同 warning key 不重复提醒；
- 仅凭相同参数不能 critical，因为轮询或幂等读取可能返回新状态。

warning key 至少包含 Pattern 与 Call signature。结果变化或该 signature 离开历史窗口后允许未来重新提醒。

### 2. Stable-outcome critical

同一 Call signature 连续得到相同 Outcome signature，才构成确定的无进展证据。

- stable count 小于 5：不 critical；
- 已经确认 5 个相同结果后，下一批再次请求同一 Call signature 时触发 critical；
- 当前调用尚未执行，不能假设它必然返回第 6 个相同结果，因此阈值定义为“已确认结果数”，不是模型请求序号；
- 结果变化立即把 stable count 重置为 1。

unknown/unavailable tool 的稳定错误、持续权限拒绝、无状态变化的 polling 都会自然形成相同 Outcome signature，不需要通过错误文本正则或工具名特判。

### 3. Ping-pong

对最近历史检查 projected 调用尾部是否形成严格交替的：

```text
A, B, A, B, A ...
```

其中 A/B 是两个不同 Call signature。

- projected 交替长度达到 3 时 warning；
- projected 交替长度达到 5，且 `stableForPingPong(A)` 与 `stableForPingPong(B)` 同时成立时 critical；
- 任一侧结果变化、出现第三个 signature 或顺序不再交替时重置该模式；
- A/B 可以是同一个工具的两组参数，也可以是两个不同工具。

形式化定义：`stableForPingPong(signature)` 当且仅当当前交替尾部在本次 admission 之前，已经包含该 signature 至少 2 个已完成、非 `loopVeto` 的 Outcome，并且这些 Outcome signature 全部相同。当前批次内尚未执行的 projected calls 不提供 Outcome 证据；因此没有历史结果的一批 `A/B/A/B/A` 只能触发 warning，不能直接 critical。

### 4. Global circuit breaker

局部模式可能在多个 signature 之间切换，无法让单一 stable streak 达到 5。为此，在最近 16 条记录中统计“重复了先前相同 Call + Outcome 的无进展记录”。

- 无进展证据少于 8：不触发全局规则；
- 达到 8：产生 `PatternGlobalCircuit` critical；
- 结果变化只会让对应 signature 的新结果不计为重复证据，不会删除窗口中已经发生的事实；事实随 16 条窗口自然淘汰。

该规则仍要求相同结果证据，不会因为 8 次参数相同但结果持续变化而熔断。

### 5. Post-compaction repeat

成功 L2 Compaction 后，Detector 观察接下来最多 3 个未排除工具结果。若这 3 个结果的完整 `(toolName, callSignature, outcomeSignature)` 完全相同，立即返回 `LevelTermination + PatternPostCompaction`。

该规则不提供普通 recovery：Compaction 本身已经是一次改变上下文、打破循环的恢复动作；压缩后仍连续得到 3 个完全相同结果，继续生成只会重复消耗资源。

如果 3 个结果未全部相同，窗口耗尽并自动 disarm。再次成功 L2 Compaction 会重新 arm，并替换尚未结束的旧窗口。

第一版 post-compaction guard 只覆盖单一完整 Outcome 连续重复。压缩后重新出现 `A/B/A/B` 交替模式时，这个 3-outcome 窗口不会终止；普通 16 条历史不会因 Compaction 清空，因此继续由 Ping-pong 与 global circuit 规则兜底。该漏报方向是降低短窗口误杀率的有意取舍，第一版不保存压缩前 baseline 快照或其他仅用于诊断的死状态。

## Critical 恢复状态机

除 post-compaction 规则外，所有 critical 统一进入同一个 Run-local 状态机：

```text
第一次 critical
    -> DecisionRecover
    -> 阻止整个工具批次
    -> 合成完整 Tool Results
    -> 允许模型再生成一个 turn

同一 Run 再次出现任意 critical
    -> DecisionTerminate
    -> 不提交当前 tool-calling Assistant
    -> 不执行工具
    -> 返回 loop_detected
```

“第二次”是 Run 级计数，不要求 Pattern、工具名或 Call signature 与第一次相同。模型已经得到一次明确恢复机会；如果随后又进入任何已确认 critical 模式，继续尝试的收益低于资源风险。

第一次被 veto 的批次由 `AdmitToolBatch` 自己记录为 `loopVeto` 证据，因此：

- Loop 不得再调用 `RecordToolBatchOutcome`；
- veto 不伪装成真实工具结果，不生成普通 Outcome signature；
- veto 不会错误地被解释为“工具结果变化，所以已有循环取得进展”；
- 下一次相同或其他 critical 仍能触发终止。

`globalCircuitBreakerThreshold=8` 是独立的跨模式 critical 证据阈值，不替代“第二次 critical 即终止”。如果在此之前已经发生过一次 critical，它直接成为第二次并终止；否则仍先给一次恢复机会。

## Tool batch 原子准入

`AdmitToolBatch` 对整个 `ai.ToolCalls` 做一次纯内存、无副作用判定：

1. 保留原始 Tool Call 顺序；
2. 跳过 excluded tools；
3. 对剩余调用计算 projected pattern；
4. 汇总整批中最严重的干预，优先级为 `terminate > recover > warn > allow`；
5. 只有最终为 allow/warn 时，才把本批未排除调用以 pending outcome 状态写入历史；
6. recover/terminate 时不允许 Scheduler 启动任何调用；recover 额外记录本批 veto 证据。

如果同一批中一个调用正常、另一个 critical，整个批次都阻止。不能只执行“安全的那部分”，因为模型把一次 Assistant Tool Calls 视为一个协议组，部分执行会制造不可预测的副作用和恢复语义。

## Loop 集成与消息协议

### 固定检查顺序

每个 Action 的顺序调整为：

1. 检查父 `ctx` canceled/deadline；
2. 进入 turn 前执行 `gov.CheckTurnLimit()`，通过后 `gov.StartTurn()`；
3. 完成模型调用，校验可信 Usage，记录 Invocation，并执行 `gov.Observe()`；
4. 如果达到 `MaxCostUSD` / `MaxTotalTokens`，按现有预算协议结束，不调用 Detector；
5. 校验 Action 契约；
6. 无 Tool Calls 时提交完整 Assistant 并正常结束；
7. 执行 `actionResp.ToolCalls.Validate()`；
8. 调用 `detector.AdmitToolBatch(actionResp.ToolCalls)`；
9. allow/warn/recover 路径按下文提交完整协议组；terminate 路径不提交；
10. allow/warn 时执行可见性/子代理批次规划与 Scheduler；
11. 工具批次结算后，父预算错误优先于 Scheduler 基础设施错误；
12. 按原始下标合并 Tool Results，并校验长度、ToolCallID、ToolName 对齐；
13. 对齐失败时执行“结果对齐失败”协议并以 internal error 终止，不调用 Detector；
14. 对齐正常时提交所有 Tool Results；
15. 调用 `RecordToolBatchOutcome`，若 post-compaction 返回终止，则在已完成协议组之后结束 Run。

如果同一个 Action 模型调用已经耗尽 Token/Cost 预算，预算终止获胜；Detector 不检查也不改变终止原因。`MaxTurns`、Token、Cost 与行为检测因此不是两套竞争预算，而是在不同检查点执行的不同安全边界。

### Allow

```text
提交 Assistant
发送 message_end
执行整批工具
提交全部 Tool Results
RecordToolBatchOutcome
进入下一 turn
```

### Warn

与 Allow 相同，但把固定提醒排入下一次 Action 的内部模型上下文。提醒内容只包含 Pattern、次数和工具名，表达：已经观察到重复调用，应检查结果是否变化；没有进展时停止重试、改用其他方法或明确报告阻塞。

提醒必须满足：

- 只进入下一次逻辑 Action 的 Provider Context；
- 不进入 `state.contextHistory` 或 `generationResult.context`；
- 不进入 `state.newMessages`；
- 不触发 `EventListener`；
- 不进入 Reporter/SSE/MySQL；
- 不进入 proactive/reactive Compaction 的摘要输入；
- 不写日志正文；
- generation retry 仍能看到同一提醒；
- 一次 Action 完成后消费，不永久污染后续上下文。

不能依赖“临时 append 后按正文标记剥离”：当前普通生成会把输入 `messages` 原样放入 `generationResult.context`，随后每轮必经 `state.contextHistory = generated.context`；只处理 reactive compaction 分支仍会泄漏提醒。

生成链固定增加 durable/ephemeral 双通道：

```go
type generationInput struct {
    Durable   []ai.Message
    Ephemeral []ai.Message
}
```

- `Durable` 是允许写回 `generationResult.context`、参与 L1/L2 Compaction 和进入后续 turn 的真实历史；
- `Ephemeral` 只在每次物理 Provider 请求前复制并附加到 `Durable`，不属于返回 Context；
- `generateWithRetry` 的每次 retry 使用同一份 Ephemeral，保证一次逻辑 Action 内提醒不丢失；
- `recoverOverflow` 只对 Durable 做 L1/L2，随后在重试 Provider 时重新附加 Ephemeral，提醒不进入摘要输入；
- `generationResult.context` 在普通成功、L1 retry、L2 retry 和失败恢复路径中始终只返回 Durable；
- `Loop` 在一次逻辑 Action 完成后清空 pending reminder；若生成最终失败并结束 Run，request-local 状态随 Run 释放。

Provider 最终看到的是 `clone(Durable) + Ephemeral`，但 Provider 接口和 `ai.Message` 不增加持久化标记。Anthropic 可以继续把临时 System Message 聚合进本次请求的 system prompt；该消息不会进入下一次请求。实现必须复制切片，不能因 append 复用底层数组而修改 Durable 或调用方历史。

### Recover

第一次 critical 不执行 Scheduler，但为了满足 Tool Calling 协议，Loop 提交：

1. 原始 tool-calling Assistant；
2. 与每个 Tool Call 一一对应的合成 Tool Result。

每个结果：

```go
toolexec.Result{
    ToolCallID: call.ID,
    ToolName:   call.Name,
    Content: []ai.ContentBlock{ai.TextBlock(
        "工具循环护栏阻止了本批次执行：检测到重复且无进展的调用。请停止当前重试路径，改用不同方案，或明确说明无法继续。",
    )},
    IsError:   true,
    ErrorCode: pierrors.ErrorCodeRunLoopDetected,
}
```

要求：

- 所有调用都生成结果，包括 excluded tool；因为整批没有任何调用启动；
- 结果按原始下标提交；
- 复用现有 synthetic rejection 的 start/end Event 行为，按原始顺序发送完整事件；
- Assistant 与所有 Tool Results 都进入 `contextHistory` 和 `newMessages`，形成可恢复、可持久化的完整协议组；
- 不调用 Scheduler，不创建 BatchBudget，不调用 `RecordToolBatchOutcome`；
- 下一 turn 仍受 MaxTurns、Token、Cost 和 context 取消约束，不额外绕过预算赠送调用。

“给予一次恢复 turn”只表示循环策略不立即终止；它不保证一定还能调用模型。如果恢复前已经触发其他预算或调用方取消，现有高优先级边界照常结束 Run。

### Terminate

第二次 critical 的当前 Assistant 已经流式发送过 provisional delta，但不能提交：

- 不写 `contextHistory`；
- 不写 `newMessages`；
- 不发送 `message_end`；
- 不生成 Tool Results；
- 不执行 Scheduler；
- 返回带 safe metadata 的 `*loopdetect.Error`。

Web Chat 继续依赖现有 `run.failed` 路径丢弃 provisional Assistant。持久化历史保持 Tool Calling 协议完整。

### Outcome 后直接终止

post-compaction guard 只能在真实结果完成后判定，因此先提交 Assistant 和完整 Tool Results，再返回 typed error。已经发生的工具副作用和消息不能回滚，也不能为了让终止看起来更早而删除。

### Tool Result 对齐失败

`len(calls) != len(results)`、`calls[i].ID != results[i].ToolCallID` 或工具名不一致都表示 Scheduler/合并逻辑违反内部不变量，不属于模型行为，也不能降级为“跳过循环记账后继续”。工具批次可能已经产生副作用，因此处理固定为：

1. 保留所有已经完成的 Invocation 和 Governor Totals；
2. 不调用 `RecordToolBatchOutcome`，Detector 历史保持在 admission 后的 pending 状态，随后随 Run 释放；
3. 不尝试回滚或重放任何工具；
4. 为原始 Tool Calls 各生成一个完整的 `IsError=true` Tool Result，ToolCallID/ToolName 取原始调用，`ErrorCode=ErrorCodeInternal`，安全正文固定说明“工具批次结果对齐失败，执行状态未知，请勿自动重试”；
5. 把这些合成结果追加到 `contextHistory` 与 `newMessages`，闭合已经提交的 tool-calling Assistant；真实工具已经发出的 start/update/end 事件不重复补发；
6. 返回 `pierrors.Wrap(ErrorCodeInternal, "tool batch outcome reconciliation", err)`，`done=true`，最终 Termination 为 `error` 而不是 `loop_detected`。

该路径是内部故障兜底，不声称合成结果代表真实执行结果；它只保证协议闭合、已发生费用不丢失，并阻止模型或调用方基于不可靠状态自动继续。

## Compaction 集成

`ArmPostCompaction()` 只在一次 L2 摘要成功并原子提交 `compacted messages + next CompactionState` 后调用：

- proactive L2 成功：arm；
- reactive overflow recovery 的 L2 成功：arm；
- L1 prune：不 arm；
- L2 计划失败、Provider 失败、Usage/摘要契约失败或 ApplySummary 失败：不 arm；
- 同一 Run 再次 L2 成功：重新 arm，替换旧窗口。

`compactionRuntime` 增加 request-local 的 nil-safe `onCommitted func()`，由 `Loop.run` 设为 `detector.ArmPostCompaction`，在 proactive/reactive 两个状态提交点统一调用。这样 `pi/compaction.go` 不负责循环算法，也不会把 Detector 状态塞入 `CompactionState`。

## 错误与最终 Termination

新增稳定错误码：

```go
ErrorCodeRunLoopDetected ErrorCode = "run_loop_detected"
```

`pi/loopdetect/errors.go` 定义：

```go
var ErrLoopDetected = errors.New("agent tool loop detected")

type Error struct {
    Pattern   Pattern
    Count     int
    ToolNames []string
}

func (err *Error) Error() string
func (err *Error) Unwrap() error
```

`Error` 只保存安全 intervention metadata，不保存 hash、参数、结果或模型正文。Loop 使用 `pierrors.Wrap(ErrorCodeRunLoopDetected, "tool loop detection", err)` 保持统一分类。

现有：

```go
governor.TerminationLoopDetected = "loop_detected"
```

已经存在。`governor.TerminationFromError` 增加对 `*loopdetect.Error` 的 `errors.As` 分支，映射到 `TerminationLoopDetected`。`Termination.Totals` 仍来自同一个 Governor；Detector 不构造、不复制、不修改 Totals。

包依赖方向固定为：

```text
governor  ──> loopdetect     仅用于 TerminationFromError 识别 typed Error
loopdetect ──> toolexec      仅用于归一化 Result
loopdetect -X-> governor     禁止反向依赖
```

`loopdetect` 不得 import `governor`，不得接收 Limits/Totals/Invocation；否则会形成职责倒置，并可能经 `governor -> loopdetect` 产生包循环。

取消/deadline 的映射优先于 loop error。实现不得使用 `errors.Join` 把循环错误和预算错误拼成不确定优先级；每个固定检查点只返回该顺序下的一个错误。

## 主代理与子代理

Loop 保存不可变 Config，每次 `Loop.run` 执行：

```go
detector := loopdetect.New(l.loopDetection)
```

因此：

- 同一个共享 Loop 的并发 Run 各自独立；
- 父 Agent 和每个 subagent 的 Detector 独立；
- Token/Cost 继续通过 Governor/BatchBudget 向父级实时传播；
- 行为历史、warning、critical 次数和 post-compaction window 不向父级传播；
- 一个子代理循环只终止该子 Run，随后按现有 subagent tool 错误结果返回父 Agent；
- 子运行 Invocation 仍按现有流程进入父账本，不因循环终止丢失；
- 子 Loop 继承相同 Config 和 excluded tools，但创建新 Detector。

这与资源预算的语义不同：资源消耗必须向父级结算，局部行为模式必须隔离，否则两个并发子代理调用同一搜索工具会被误判为一个循环。

## 装配方式

`Loop` 增加：

```go
func WithLoopDetection(config loopdetect.Config) LoopOption
```

零值 Option 不改变现有行为。`pi.Register` 的 Loop 构造参数增加 optional `loopdetect.Config`，并把同一配置传给根 Loop 与 subagent loop builder。`config.NewLoopDetectionConfig` 从 `Config.Agent.LoopDetection` 生成装配值，`cmd/server/app.go` 显式 provide。

`application/service/chat/run_manager.go` 不接收 Detector，也不参与算法，只需把新的稳定 error code / `TerminationLoopDetected` 映射为安全的 `run.failed` 响应。外部 HTTP 请求不增加循环配置字段。

## 可观测性

第一版不增加对外 AgentEvent，避免 warning 通过 SSE 泄露或被误当成业务消息。

增加无正文的结构化观测：

- 日志字段：`pattern`、`level`、`count`、排序后的 `tool_names`；
- Turn Span 属性：`reagent.loop_detection.pattern`、`reagent.loop_detection.level`、`reagent.loop_detection.count`；
- Counter：`reagent.loop_detection.interventions`，低基数 label 仅使用 `pattern` 和 `level`，不把工具名作为 metric label；
- 最终 termination 继续走现有 Notifier，`loop_detected` 属于异常终止。

禁止记录 Arguments、Content.Text、canonical JSON、Call/Outcome hash。hash 虽不是明文，但对低熵命令、路径或 URL 仍可能被字典反推，不应进入日志或指标。

## 测试设计

### Config

1. bundled application 对空白、带前后空格和重复 `excluded_tools` 在 `config.Load` 阶段 fail-fast。
2. SDK 直接调用 `loopdetect.New` 时对 ExcludedTools 做 trim、去空、去重，并复制切片；调用方随后修改原 Config 不影响 Detector。
3. Config 零值保持 disabled，所有方法为无增长型状态的 no-op。

### Fingerprint

1. JSON object key 顺序和无意义空白不同，Call signature 相同。
2. ToolCallID 不同，Call signature 相同。
3. tool name、array 顺序、string、boolean、null 或 number 文本不同，Call signature 不同。
4. 超过 JavaScript 安全整数范围的大整数不发生 float64 精度折叠。
5. Outcome 的 IsError、ErrorCode、Content 顺序/Type/Text 任一变化都会改变签名。
6. Details、duration、PID、timestamp、ToolCallID 变化不改变 Outcome signature。
7. Detector 状态中不存在原始参数与结果副本。
8. Content.Text 中时间戳变化会改变 Outcome signature，并用测试固定这一有意漏报边界。

### Admission 与 history

1. disabled 恒 Allow，且不分配增长型历史。
2. 第 3 次相同 Call 只 Warn，不阻止执行。
3. 相同 warning key 只提醒一次；结果变化后未来允许重新提醒。
4. 相同 Call 但结果持续变化，不触发 stable critical 或 global circuit。
5. 确认 5 个相同 outcomes 后再次调用，第一次返回 Recover。
6. Recover 后再次出现任意 critical，返回 Terminate。
7. Recover 批次由 Admit 记录 veto；调用 Outcome recorder 会由 Loop 测试证明不会发生。
8. A/B/A 达到 warning；A/B 各只有一个已完成 Outcome 时不得 critical。
9. 同一批没有历史 Outcome 的 A/B/A/B/A 不得 critical；A/B 各有至少两个相同已完成 Outcome 时可以 critical。
10. A/B 中任一结果变化或插入 C，ping-pong critical 不成立。
11. 16 条窗口中达到 8 条跨 signature 无进展证据，触发 global critical。
12. 第 17 条插入后最旧记录淘汰；signature 完全离窗时清除 stable、warning 与 Ping-pong 状态，但不重置 Run 级 critical 次数。
13. 超过窗口间隔的慢循环不累计 stable count，由预算兜底。
14. excluded tool 不计数；混合批次中的未排除工具仍可阻止整批。
15. Tool Names 去重、排序稳定。

### Batch 原子性与消息协议

1. 批次任一调用 critical 时 Scheduler 调用次数为 0。
2. Warn 批次完整执行，结果按原始 Tool Call 下标记录，不受实际完成顺序影响。
3. Recover 为每个 Tool Call 生成 `IsError=true + run_loop_detected` 结果，包括 excluded call。
4. Recover 的 Assistant 与全部 Tool Results 同时进入 Context/NewMessages，并发送完整 synthetic start/end 事件。
5. Terminate 不提交 Assistant、不生成 Tool Results、不发送 message_end。
6. provisional SSE Assistant 在 run.failed 时被现有前端删除。
7. post-outcome 终止仍保留已经执行并完成的 Assistant + 全部 Tool Results。
8. ToolCall 固有校验失败发生在 admission 前，Detector 历史不变化。
9. Tool Result 数量、ID 或工具名对齐失败时不调用 Detector，为全部原始 calls 合成 `ErrorCodeInternal` 结果，保留 Invocation/Totals 并以 `TerminationError` 结束。

### Warning ephemeral context

1. 普通成功路径中，Provider 能看到 warning，`generationResult.context` 与 `state.contextHistory` 都不包含 warning。
2. transient/rate-limit retry 的每次物理请求都看到同一 warning，逻辑 Action 完成后只消费一次。
3. proactive L1/L2 与 reactive L1/L2 的输入、摘要范围和返回 Durable Context 都不包含 warning；压缩后的 Provider retry 仍看到 warning。
4. Ephemeral 合并使用复制切片，不修改 Durable、RunRequest History 或调用方 Config。
5. warning 不进入 NewMessages、EventListener、SSE、MySQL 或日志正文。

### 与 Governor 的优先级

1. Action 同时耗尽 Cost/Token 且包含循环 Tool Calls：预算终止，Detector 未调用。
2. 下一 turn 同时达到 MaxTurns 且可能发生循环：MaxTurns 终止，不调用模型或 Detector。
3. context canceled/deadline 覆盖 loop termination。
4. Loop error 映射到 `TerminationLoopDetected`，Totals 与全部已完成 Invocations 保持一致。
5. Detector 不改变 Limits、Totals、Invocation Sequence 或 Parent BatchBudget。

### Compaction

1. proactive L2 成功后 arm；L1 prune 不 arm。
2. reactive L2 成功后 arm。
3. L2 任一失败路径不 arm。
4. arm 后三个完全相同的 Call + Outcome 在第三个完成结果后终止。
5. 三个结果中参数或结果任一变化，不终止并自动 disarm。
6. excluded outcome 不消费三次观察窗口。
7. 窗口未结束时再次成功 Compaction，旧窗口被替换。
8. Compaction 不清空普通 16 条历史、warning 或 critical 次数。
9. post-compaction 的 A/B/A 三个不同完整 Outcome 不触发专属 guard；后续仍由保留的普通 Ping-pong/global 状态处理。

### 子代理与并发

1. 父/子调用相同工具不会共享次数。
2. 两个并发子代理调用相同工具不会互相触发 warning/critical。
3. 子代理 loop termination 只结束子 Run，父 Agent 收到完整 subagent tool error。
4. 子代理已完成 Invocation 继续扣减父预算并进入父账本。
5. 共享 Loop 的并发根 Run 在 race test 下互不污染。

## 实施切片

实现计划应按以下可独立验证的顺序拆分，但本文不直接执行：

1. `loopdetect` types/config/fingerprint 与单元测试；
2. 普通历史、warning、stable outcome、ping-pong、global circuit 状态机；
3. batch admission、一次 recovery 与 typed error；
4. post-compaction guard；
5. Loop 消息提交时机、durable/ephemeral 生成链、结果对齐失败与 synthetic results；
6. Governor termination/error code 集成；
7. Config/fx/root+subagent Loop 装配；开始实现前用当前工作树重新核对 fx provider 的真实落点，配置已迁移时修改实际装配入口，不强行沿用过期文件路径；
8. SSE/Notifier/observability 安全映射；
9. 全量单元、集成、race 和回归测试。

## 验收标准

设计实现完成必须同时满足：

1. `MaxTurns`、`MaxCostUSD`、`MaxTotalTokens` 的目录、配置、行为和现有测试保持在 `governor`，没有复制状态。
2. `Loop` 只创建一个 request-local Detector，并在固定检查点调用三个方法。
3. 任一被阻止批次都没有 Tool 副作用；Recover 消息组完整；Terminate 不留下孤立 Assistant。
4. 相同参数但结果变化永不触发 stable/global critical。
5. 第一次普通 critical 恢复、第二次终止；post-compaction 三次完全相同结果直接终止。
6. 主/子/并发 Run 的 Detector 状态完全隔离，父子资源预算传播不受影响。
7. warning 只进入一次逻辑 Action 的 Provider 请求，不进入 `generationResult.context`、`state.contextHistory`、Compaction summary、NewMessages、EventListener、SSE、数据库或日志正文。
8. 最终 `RunResult.Termination.Reason == loop_detected`，Totals 与 Invocation 明细一致。
9. Tool Result 对齐失败闭合消息协议、保留 Invocation/Totals，并以 `error` 而非 `loop_detected` 终止。
10. `go test ./...`、相关 race test、`git diff --check` 全部通过。

## 后续演进触发条件

只有出现真实生产样本时再考虑：

- 对 polling/exec/message 增加工具专属 result canonicalizer；
- 忽略 timestamp、cursor、random ID 等参数抖动字段；
- 针对只读工具与副作用工具使用不同阈值；
- 把阈值开放为高级配置；
- 增加跨 turn 的纯文本重复检测；
- 在管理端展示匿名化 intervention 统计。

这些扩展必须继续遵守三个不变量：Detector 不拥有预算、阻断发生在副作用前、原始参数与结果不进入循环检测账本。
