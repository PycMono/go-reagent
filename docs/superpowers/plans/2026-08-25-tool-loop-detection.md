# Tool Loop Detection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为每次 `pi.Runner.Run` 增加请求级工具行为循环检测，在确定性预算耗尽前提醒、阻止并最终终止重复且无进展的 Tool Loop。

**Architecture:** 保留 `pi/governor` 作为预算、账本和最终 Termination 的唯一所有者；新增 `pi/loopdetect` 维护有界行为历史并返回 `Admission`/`Intervention`；`pi.Loop` 在 Scheduler 副作用边界前后显式编排 Detector。Warning 通过 Action-only 的 durable/ephemeral 生成输入注入，不进入持久历史或 Compaction。

**Tech Stack:** Go 1.24、标准库 `crypto/sha256`/`encoding/json`/`errors`、Uber Fx、现有 `pi/ai`/`pi/toolexec`/`pi/governor`、go-context-sdk tracing、go-observability-sdk metrics。

**Spec:** `docs/superpowers/specs/2026-08-25-tool-loop-detection-design.md`

## Global Constraints

- `MaxTurns`、`MaxCostUSD`、`MaxTotalTokens`、Invocation、Totals、父子预算传播继续只存在于 `pi/governor`。
- 固定阈值：history `16`、warning `3`、critical `5`、global circuit `8`、post-compaction window `3`；第一版不开放阈值配置。
- `loopdetect` 可以 import `ai`、`toolexec`，不得 import `governor`；`governor` 只为 typed error 映射 import `loopdetect`。
- Tool batch admission 是原子的；Recover/Terminate 路径在 Scheduler 启动前阻止整批，已经启动的批次不做循环型中途取消。
- 相同调用但 Outcome 变化表示进展；易变 `Content.Text` 造成的漏报由 warning 和 Governor 绝对预算兜底。
- Warning 只进入下一次 Action 的物理 Provider 请求；不得进入 Thinking/Compaction、`generationResult.context`、`state.contextHistory`、NewMessages、EventListener、SSE、数据库、Compaction summary 或日志正文。
- Ephemeral 文本不单独加入本地 Token pressure 估算；Provider 返回的真实 `InputTokens` 仍照常进入 Invocation/Governor 记账。
- 父 Run、每个子 Run、并发根 Run 分别创建独立 Detector；Compaction 不清空 Detector。
- 原始 Arguments、Content、canonical JSON、Call/Outcome hash 不得写入日志、指标、Span、数据库或错误正文。
- 每个任务只暂存自身文件；工作树中的 `cmd/server/app.go`、`infrastructure/*`、`docs/cli.md` 等既有用户改动必须保留并逐行合并，禁止覆盖或回退。

## File Map

**Create:**

- `pi/loopdetect/config.go`：SDK Config 快照和防御性 ExcludedTools 归一化。
- `pi/loopdetect/types.go`：Decision、Admission、Intervention、Pattern、Level 和内部 record。
- `pi/loopdetect/fingerprint.go`：Call/Outcome signature。
- `pi/loopdetect/detector.go`：普通 16 条历史、warning、stable、ping-pong、global 和 critical recovery 状态机。
- `pi/loopdetect/post_compaction.go`：3-outcome post-compaction guard。
- `pi/loopdetect/errors.go`：`ErrLoopDetected` 与 typed `Error`。
- `pi/loopdetect/fingerprint_test.go`、`detector_test.go`、`post_compaction_test.go`：包级行为测试。
- `pi/loopdetect_run_test.go`：Loop admission、消息协议、ephemeral 和并发集成测试。

**Modify:**

- `pi/loop.go`：Loop Config、request-local Detector、Admission 编排、synthetic results、outcome reconciliation。
- `pi/recovery.go`：`generationInput{Durable, Ephemeral}` 以及 Action-only Provider 合并。
- `pi/compaction.go`：只压缩 Durable，并在成功 L2 commit 后 arm Detector。
- `pi/register.go`、`pi/register_test.go`：根/子 Loop 注入同一不可变 Config。
- `pi/errors/errors.go`、`errors_test.go`：稳定 `run_loop_detected` error code。
- `pi/governor/governor.go`、`governor_test.go`：typed loop error → `TerminationLoopDetected`。
- `config/config.go`、`config/load.go`、`config/validate.go`、`config/config_test.go`、`config.example.json`：bundled service 配置、校验和 fx provider。
- `cmd/server/app.go`：向 Fx 图提供 `loopdetect.Config`；实现时保留文件中的既有用户修改。
- `pi/harness/observability/semantics.go`、`metrics.go`、`record.go`、相关测试：低基数 intervention Span/Metric。
- `application/service/chat/run_manager.go`、`run_manager_test.go`：安全 `loop_detected` 失败映射。
- `pi/notify_test.go`：loop termination 告警回归。

---

### Task 1: Config、类型与稳定指纹

**Files:**
- Create: `pi/loopdetect/config.go`
- Create: `pi/loopdetect/types.go`
- Create: `pi/loopdetect/fingerprint.go`
- Create: `pi/loopdetect/fingerprint_test.go`

**Interfaces:**
- Produces: `Config`、`Decision`、`Admission`、`Intervention`、`Pattern`、`Level`、`New(Config) *Detector` 的构造基础。
- Produces internally: `callSignature(ai.ToolCall) (string, error)`、`outcomeSignature(string, toolexec.Result) string`。
- Consumes: `ai.ToolCall`、`toolexec.Result`、`pierrors.ErrorCode`。

- [ ] **Step 1: 写 Config 防御性归一化失败测试**

在 `fingerprint_test.go` 先固定 Config 不修改调用方输入：

```go
func TestNewNormalizesExcludedToolsWithoutMutatingConfig(t *testing.T) {
    cfg := Config{Enabled: true, ExcludedTools: []string{" read ", "", "read", "exec"}}
    detector := New(cfg)
    cfg.ExcludedTools[0] = "changed"

    if !detector.excluded("read") || !detector.excluded("exec") || detector.excluded("changed") {
        t.Fatalf("excluded snapshot = %#v", detector.excludedTools)
    }
}
```

- [ ] **Step 2: 写 canonical call signature 失败测试**

覆盖 key/空白稳定、ID 忽略、数组顺序、数字文本和大整数：

```go
func TestCallSignatureCanonicalJSON(t *testing.T) {
    same := []ai.ToolCall{
        {ID: "a", Name: "read", Arguments: json.RawMessage(`{"b":2,"a":9007199254740993}`)},
        {ID: "b", Name: "read", Arguments: json.RawMessage(` { "a" : 9007199254740993, "b" : 2 } `)},
    }
    first, err := callSignature(same[0])
    if err != nil { t.Fatal(err) }
    second, err := callSignature(same[1])
    if err != nil { t.Fatal(err) }
    if first != second { t.Fatalf("signatures differ: %q != %q", first, second) }

    changed, _ := callSignature(ai.ToolCall{Name: "read", Arguments: json.RawMessage(`{"a":9007199254740993,"b":2.0}`)})
    if changed == first { t.Fatal("number text change must change signature") }
}
```

- [ ] **Step 3: 写 Outcome signature 纳入/排除字段失败测试**

```go
func TestOutcomeSignatureIncludesStableContentAndExcludesDetails(t *testing.T) {
    base := toolexec.Result{
        ToolCallID: "one", ToolName: "read", IsError: true,
        ErrorCode: pierrors.ErrorCodeToolRuntime,
        Content: []ai.ContentBlock{ai.TextBlock("failed at 10:00")},
        Details: map[string]any{"pid": 1},
    }
    got := outcomeSignature("call-hash", base)
    base.ToolCallID = "two"
    base.Details = map[string]any{"pid": 2}
    if same := outcomeSignature("call-hash", base); same != got {
        t.Fatal("ToolCallID/Details must not affect outcome signature")
    }
    base.Content[0].Text = "failed at 10:01"
    if changed := outcomeSignature("call-hash", base); changed == got {
        t.Fatal("volatile Content.Text intentionally changes signature")
    }
}
```

- [ ] **Step 4: 运行测试确认因缺少包实现而失败**

Run: `go test ./pi/loopdetect -run 'Test(NewNormalizes|CallSignature|OutcomeSignature)' -count=1`

Expected: FAIL，提示 `Config`、`New` 或 signature 函数未定义。

- [ ] **Step 5: 实现公共类型和 Config 快照**

在 `types.go` 定义设计稿中的枚举和结构：

```go
type Decision string
const (
    DecisionAllow Decision = "allow"
    DecisionWarn Decision = "warn"
    DecisionRecover Decision = "recover"
    DecisionTerminate Decision = "terminate"
)

type Level string
const (
    LevelWarning Level = "warning"
    LevelRecovery Level = "recovery"
    LevelTermination Level = "termination"
)

type Pattern string
const (
    PatternRepeatedCall Pattern = "repeated_call"
    PatternStableOutcome Pattern = "stable_outcome"
    PatternPingPong Pattern = "ping_pong"
    PatternGlobalCircuit Pattern = "global_circuit_breaker"
    PatternPostCompaction Pattern = "post_compaction_repeat"
)

type Intervention struct {
    Level Level
    Pattern Pattern
    Count int
    ToolNames []string
}

type Admission struct {
    Decision Decision
    Intervention *Intervention
}
```

在 `config.go` 固定常量，`New` 复制、trim、去空、去重 ExcludedTools；disabled Detector 不分配历史。

- [ ] **Step 6: 实现 canonical JSON 和 SHA-256**

`fingerprint.go` 使用 `json.Decoder.UseNumber()`，再读一次 token 验证 EOF。递归输出 object 时先 `sort.Strings(keys)`；number 直接输出 `json.Number.String()`，string 用 `json.Marshal`。Call hash 和 Outcome hash 都用 NUL 分隔，Outcome 严格按 Content block 顺序写入。

- [ ] **Step 7: 运行包测试并检查依赖方向**

Run: `go test ./pi/loopdetect -count=1`

Expected: PASS。

Run: `go list -deps ./pi/loopdetect | rg '/pi/governor$'`

Expected: 无输出，退出码 1；`loopdetect` 不依赖 `governor`。

- [ ] **Step 8: 提交 Task 1**

```bash
git add pi/loopdetect/config.go pi/loopdetect/types.go pi/loopdetect/fingerprint.go pi/loopdetect/fingerprint_test.go
git commit -m "feat: add tool loop fingerprints"
```

---

### Task 2: 普通工具循环状态机与批次准入

**Files:**
- Create: `pi/loopdetect/detector.go`
- Create: `pi/loopdetect/detector_test.go`
- Modify: `pi/loopdetect/types.go`

**Interfaces:**
- Consumes: Task 1 signatures and public types。
- Produces: `(*Detector).AdmitToolBatch(ai.ToolCalls) Admission`。
- Produces: `(*Detector).RecordToolBatchOutcome(ai.ToolCalls, []toolexec.Result) *Intervention` 的普通历史部分。
- Internal invariant: history 最大 16；结果按原始 calls 下标记录，不按完成顺序。

- [ ] **Step 1: 写 repeated-call warning 与 changing-result 测试**

先用相同 Outcome 固定同一 warning key 只提醒一次；再用单独 Detector 固定持续变化的 Outcome 永不 Recover/Terminate（结果变化允许未来再次 Warn，因此该分支不要求 Warn 只出现一次）：

```go
func TestRepeatedCallWarnsOnceForStableOutcome(t *testing.T) {
    d := New(Config{Enabled: true})
    call := ai.ToolCall{ID: "c", Name: "read", Arguments: json.RawMessage(`{"path":"a"}`)}
    for i := 1; i <= 5; i++ {
        call.ID = fmt.Sprintf("c%d", i)
        admission := d.AdmitToolBatch(ai.ToolCalls{call})
        if i == 3 && admission.Decision != DecisionWarn { t.Fatalf("third = %v", admission) }
        if i != 3 && admission.Decision != DecisionAllow { t.Fatalf("call %d = %v", i, admission) }
        d.RecordToolBatchOutcome(ai.ToolCalls{call}, []toolexec.Result{{
            ToolCallID: call.ID, ToolName: call.Name,
            Content: []ai.ContentBlock{ai.TextBlock("same")},
        }})
    }
}

func TestChangingResultsNeverBecomeCritical(t *testing.T) {
    d := New(Config{Enabled: true})
    for i := 1; i <= 10; i++ {
        call := ai.ToolCall{ID: fmt.Sprintf("c%d", i), Name: "read", Arguments: json.RawMessage(`{"path":"a"}`)}
        admission := d.AdmitToolBatch(ai.ToolCalls{call})
        if admission.Decision == DecisionRecover || admission.Decision == DecisionTerminate {
            t.Fatalf("changing result call %d = %v", i, admission)
        }
        d.RecordToolBatchOutcome(ai.ToolCalls{call}, []toolexec.Result{{
            ToolCallID: call.ID, ToolName: call.Name,
            Content: []ai.ContentBlock{ai.TextBlock(fmt.Sprintf("version-%d", i))},
        }})
    }
}
```

- [ ] **Step 2: 写 stable critical 的 Recover → Terminate 测试**

先记录 5 个相同 Outcome；第 6 次应 Recover 且不执行 outcome record；再次请求相同 call 应 Terminate。断言 ToolNames 去重排序、Pattern/Count 稳定。

- [ ] **Step 3: 写 Ping-pong 形式化边界测试**

分别固定：A/B 各一个完成结果不得 critical；同一批无历史 Outcome 的 A/B/A/B/A 不得 critical；A/B 各两个相同完成结果后 projected 交替长度 5 可以 critical；结果变化或插入 C 打断模式。

- [ ] **Step 4: 写 global circuit、excluded 和窗口淘汰测试**

构造 8 条跨 signature 的重复 Call+Outcome 证据触发 `PatternGlobalCircuit`；构造第 17 条淘汰某 signature，断言其 stable/warning/ping-pong 状态被清除但 `criticalInterventions` 不变；excluded call 不消费历史，混合批次中未排除 critical 仍阻止整批。

- [ ] **Step 5: 运行测试确认失败**

Run: `go test ./pi/loopdetect -run 'Test(Repeated|Stable|PingPong|Global|Excluded|History)' -count=1`

Expected: FAIL，缺少 Detector admission/history 实现。

- [ ] **Step 6: 实现有界 history 与原子 projected admission**

内部 record 只保存：

```go
type record struct {
    toolName string
    callSignature string
    outcomeSignature string
    outcomeRecorded bool
    loopVeto bool
}
```

`AdmitToolBatch` 先在 scratch history 上按 calls 原序评估整批，再取最严重 Decision：`Terminate > Recover > Warn > Allow`。Allow/Warn 才把 pending records commit 到真实 history；Recover 只为触发同一 action key 的 calls 记录 `loopVeto`，增加一次 Run 级 critical；Terminate 不启动、不提交 pending call。

- [ ] **Step 7: 实现 Outcome 记账和清理**

`RecordToolBatchOutcome` 先完整校验长度/ID/name；不匹配直接 nil 且不修改状态。正常路径查找对应 pending record、写 Outcome signature、更新 stable streak；变化重置为 1。超过 16 条时淘汰，并在 signature 完全离窗后清除全部 signature-local 状态，保留 Run 级 critical 和 post-compaction 状态。

- [ ] **Step 8: 运行 Detector 测试**

Run: `go test ./pi/loopdetect -count=1`

Expected: PASS。

- [ ] **Step 9: 提交 Task 2**

```bash
git add pi/loopdetect/types.go pi/loopdetect/detector.go pi/loopdetect/detector_test.go
git commit -m "feat: detect no-progress tool loops"
```

---

### Task 3: Post-compaction 三结果 Guard

**Files:**
- Create: `pi/loopdetect/post_compaction.go`
- Create: `pi/loopdetect/post_compaction_test.go`
- Modify: `pi/loopdetect/detector.go`

**Interfaces:**
- Produces: `(*Detector).ArmPostCompaction()`。
- Extends: `RecordToolBatchOutcome` 在普通历史记账后按原序观察未排除 outcome，并可能返回 `LevelTermination/PatternPostCompaction`。

- [ ] **Step 1: 写 arm/disarm 与三次相同结果测试**

```go
func recordPostOutcome(t *testing.T, d *Detector, id string, text string) *Intervention {
    t.Helper()
    call := ai.ToolCall{ID: id, Name: "read", Arguments: json.RawMessage(`{"path":"a"}`)}
    if admission := d.AdmitToolBatch(ai.ToolCalls{call}); admission.Decision == DecisionRecover || admission.Decision == DecisionTerminate {
        t.Fatalf("unexpected regular admission: %#v", admission)
    }
    return d.RecordToolBatchOutcome(ai.ToolCalls{call}, []toolexec.Result{{
        ToolCallID: id, ToolName: "read", Content: []ai.ContentBlock{ai.TextBlock(text)},
    }})
}

func TestPostCompactionGuardTerminatesOnThirdIdenticalOutcome(t *testing.T) {
    d := New(Config{Enabled: true})
    d.ArmPostCompaction()
    for i := 1; i <= 3; i++ {
        intervention := recordPostOutcome(t, d, fmt.Sprintf("c%d", i), "same")
        if i < 3 && intervention != nil { t.Fatalf("outcome %d = %#v", i, intervention) }
        if i == 3 && (intervention == nil || intervention.Level != LevelTermination || intervention.Pattern != PatternPostCompaction) {
            t.Fatalf("third = %#v", intervention)
        }
    }
}
```

- [ ] **Step 2: 写差异、excluded、re-arm 和 A/B 漏报边界测试**

三个结果中任一 Call/Outcome 不同则第三次自动 disarm 且不终止；excluded 不消费 remaining；mid-window re-arm 重置为 3；A/B/A 不触发专属 guard。

- [ ] **Step 3: 运行测试确认失败**

Run: `go test ./pi/loopdetect -run PostCompaction -count=1`

Expected: FAIL，`ArmPostCompaction` 或观察状态未实现。

- [ ] **Step 4: 实现三观察窗口**

Guard 只保存 `remaining=3` 和本窗口完整 Outcome signatures。每个未排除 outcome 消费一次；第三次时只有三个 signature 完全相同才返回 Termination Intervention，随后无条件 disarm。`ArmPostCompaction` 替换任何旧窗口，不保存压缩前 baseline。

- [ ] **Step 5: 运行完整包测试**

Run: `go test ./pi/loopdetect -count=1`

Expected: PASS。

- [ ] **Step 6: 提交 Task 3**

```bash
git add pi/loopdetect/detector.go pi/loopdetect/post_compaction.go pi/loopdetect/post_compaction_test.go
git commit -m "feat: guard post-compaction tool loops"
```

---

### Task 4: Typed loop error、稳定错误码与 Governor Termination

**Files:**
- Create: `pi/loopdetect/errors.go`
- Modify: `pi/errors/errors.go`
- Modify: `pi/errors/errors_test.go`
- Modify: `pi/governor/governor.go`
- Modify: `pi/governor/governor_test.go`

**Interfaces:**
- Produces: `loopdetect.ErrLoopDetected`、`*loopdetect.Error`。
- Produces: `pierrors.ErrorCodeRunLoopDetected = "run_loop_detected"`。
- Produces: `governor.TerminationFromError(*loopdetect.Error, totals).Reason == TerminationLoopDetected`。

- [ ] **Step 1: 写稳定错误码和 typed error 测试**

在 errors 测试的 stable map 加入：

```go
ErrorCodeRunLoopDetected: "run_loop_detected",
```

在 loopdetect 测试断言 `errors.Is(&Error{...}, ErrLoopDetected)`，`Error()` 不含 hash/参数/结果。

- [ ] **Step 2: 写 Governor 映射和优先级测试**

```go
func TestTerminationFromLoopError(t *testing.T) {
    loopErr := pierrors.Wrap(pierrors.ErrorCodeRunLoopDetected, "tool loop", &loopdetect.Error{
        Pattern: loopdetect.PatternStableOutcome, Count: 5, ToolNames: []string{"read"},
    })
    totals := Totals{Turns: 4, Invocations: 4}
    got := TerminationFromError(loopErr, totals)
    if got.Reason != TerminationLoopDetected || got.Totals != totals { t.Fatalf("termination = %#v", got) }
    if canceled := TerminationFromError(errors.Join(context.Canceled, loopErr), totals); canceled.Reason != TerminationCanceled {
        t.Fatalf("priority = %#v", canceled)
    }
}
```

- [ ] **Step 3: 运行测试确认失败**

Run: `go test ./pi/loopdetect ./pi/errors ./pi/governor -count=1`

Expected: FAIL，缺少 error code/type/mapping。

- [ ] **Step 4: 实现 errors 和单向 import**

`loopdetect.Error` 复制并排序 ToolNames，`Error()` 只格式化 Pattern/Count/ToolNames，`Unwrap()` 返回 sentinel。`governor.TerminationFromError` 在 canceled/deadline 后、limitError 前后均可加入 `errors.As` loop 分支，但取消/deadline 必须保持最高优先级；loop termination 的 `Limit` 为空。

- [ ] **Step 5: 验证测试和依赖图**

Run: `go test ./pi/loopdetect ./pi/errors ./pi/governor -count=1`

Expected: PASS。

Run: `go list -deps ./pi/loopdetect | rg '/pi/governor$'`

Expected: 无输出。

- [ ] **Step 6: 提交 Task 4**

```bash
git add pi/loopdetect/errors.go pi/errors/errors.go pi/errors/errors_test.go pi/governor/governor.go pi/governor/governor_test.go
git commit -m "feat: classify tool loop termination"
```

---

### Task 5: Action-only Durable/Ephemeral 生成链与 Compaction Arm

**Files:**
- Modify: `pi/recovery.go`
- Modify: `pi/compaction.go`
- Modify: `pi/compaction_test.go`
- Modify: `pi/tracing_run_test.go`

**Interfaces:**
- Produces internally: `generationInput{Durable, Ephemeral []ai.Message}`。
- Changes: `generateWithSpan`、`generate`、`generateWithRetry`、`recoverOverflow` 接受 generationInput；`generationResult.context` 只返回 Durable。
- Changes: `compactionRuntime.onCommitted func()`；成功 proactive/reactive L2 commit 后调用一次。

- [ ] **Step 1: 写普通成功与 phase 限定失败测试**

扩展 scripted/fake provider 记录每次 messages。Action 测试断言 Provider 看见 `loop-reminder-test`，result.context 不含；Compaction/非 Action phase 传入同一 Ephemeral 时 Provider 不应看见：

```go
input := generationInput{
    Durable: messages,
    Ephemeral: []ai.Message{{Role: ai.RoleSystem, Content: []ai.ContentBlock{ai.TextBlock("loop-reminder-test")}}},
}
```

`providerMessages(phase, input)` 必须仅在 `phase == GenerationPhaseAction` 合并 Ephemeral。当前项目没有 Thinking 调用入口，但用非 Action 单元测试锁定未来 Thinking 也不得收到提醒。

- [ ] **Step 2: 写 retry 和 reactive L1/L2 失败测试**

脚本先返回未发布的 transient/rate-limit 或 context overflow，再成功。断言每次 Action Provider 请求都看见提醒；Compaction summary 请求不看见；`generationResult.context`、L1/L2 输出和摘要输入都不含提醒。

- [ ] **Step 3: 写 proactive/reactive commit callback 测试**

为 `compactionRuntime.onCommitted` 设置计数器：proactive L2 成功一次计数 1；reactive L2 成功一次计数 1；L1、L2 失败、无缩减、预算/Usage/契约失败均为 0；re-arm 行为由 Task 3 包测试覆盖。

- [ ] **Step 4: 运行测试确认失败**

Run: `go test ./pi -run 'Test.*(Ephemeral|CompactionCommitted)' -count=1`

Expected: FAIL，generationInput/onCommitted 尚不存在。

- [ ] **Step 5: 实现 generationInput 和 Action-only 合并**

```go
type generationInput struct {
    Durable []ai.Message
    Ephemeral []ai.Message
}

func providerMessages(phase observability.GenerationPhase, input generationInput) []ai.Message {
    messages := append([]ai.Message(nil), input.Durable...)
    if phase == observability.GenerationPhaseAction {
        messages = append(messages, input.Ephemeral...)
    }
    return messages
}
```

`generateWithRetry` 每个物理请求重新调用 helper；所有 pressure、Prune、BuildCompactionPlan 只使用 `input.Durable`；reactive retry 构造 `generationInput{Durable: compacted, Ephemeral: input.Ephemeral}`。Compaction summary 传空 Ephemeral。

- [ ] **Step 6: 实现 commit callback**

在 proactive `rt.state = outcome.state` 之后、return 前调用 nil-safe callback；reactive 在 footprint 变小且 `rt.state` commit 后调用。失败和 L1 路径不调用。

- [ ] **Step 7: 明确 Token 口径并运行相关测试**

不把 Ephemeral 单独加入 `TokenMeter` pressure；它是固定短文本。真实 Provider Usage 的 InputTokens 已由现有 CostTracker/Invocation/Governor 计量，不新增本地估价或重复记账。

Run: `go test ./pi -run 'Test.*(Generate|Compact|Ephemeral|Tracing)' -count=1`

Expected: PASS。

- [ ] **Step 8: 提交 Task 5**

```bash
git add pi/recovery.go pi/compaction.go pi/compaction_test.go pi/tracing_run_test.go
git commit -m "refactor: separate ephemeral generation context"
```

---

### Task 6: Loop 批次协议、Recover/Terminate 与 Outcome 对齐

**Files:**
- Modify: `pi/loop.go`
- Create: `pi/loopdetect_run_test.go`
- Modify: `pi/subagent_test.go`

**Interfaces:**
- Consumes: `loopdetect.New`、`AdmitToolBatch`、`RecordToolBatchOutcome`、`ArmPostCompaction`、Task 5 generationInput。
- Produces: `WithLoopDetection(loopdetect.Config) LoopOption`。
- Internal helpers: `loopReminder(*loopdetect.Intervention) ai.Message`、`syntheticLoopResults`、`validateToolBatchOutcome`、`appendToolResults`。

- [ ] **Step 1: 写 Warning 下一 Action 注入集成测试**

脚本执行三次相同 echo call（结果相同）后第四次 Action 返回最终文本。断言第三次 admission 仍执行工具；第四次 Provider 请求看见固定 reminder；前 3 次和 `RunResult.NewMessages`/最终 durable history 不含 reminder。提醒在当前 Loop 只有 Action phase 注入。

- [ ] **Step 2: 写 Recover 原子阻断与协议闭合测试**

准备 5 个稳定 Outcomes 后返回包含两个 Tool Calls 的 critical batch。用计数工具断言本批执行次数为 0；NewMessages 新增原始 Assistant + 两个按原序的 `IsError=true/ErrorCodeRunLoopDetected` Tool Messages；listener 收到两个 synthetic start/end；不调用 Outcome recorder 的效果由下一次相同 critical 直接 Terminate 证明。

- [ ] **Step 3: 写 Terminate 不提交 provisional Assistant 测试**

第一次 Recover 后模型再次请求 critical。断言 `errors.Is(err, loopdetect.ErrLoopDetected)`、Termination 为 `loop_detected`、Provider Invocation 已保留、当前 Assistant/Tool Results 不进入 NewMessages、没有 message_end、Scheduler 没有调用。

- [ ] **Step 4: 写预算/取消优先级和 post-outcome 终止测试**

同一个 Action 已耗尽 Cost/Token 且含循环 Tool Calls时，Detector 不改变预算终止；下一 turn MaxTurns 在模型前终止；post-compaction 第三个相同真实 Outcome 执行并提交 Assistant+Tool Results 后才返回 loop error。

- [ ] **Step 5: 写 Tool Result 对齐失败测试 seam**

不要破坏生产 Scheduler。提取并直接测试 `validateToolBatchOutcome`/reconciliation helper，输入少结果、错 ID、错 ToolName，断言为每个原始 call 合成 `ErrorCodeInternal` Tool Message，正文固定“执行状态未知，请勿自动重试”，错误 code 为 internal，Detector recorder 不被调用。测试注明真实 start/end 与持久化 synthetic history 不一致是 bug-only 已接受取舍。

- [ ] **Step 6: 运行 Loop 测试确认失败**

Run: `go test ./pi -run 'TestToolLoop|TestToolBatchOutcome' -count=1`

Expected: FAIL，Loop 尚未集成 Detector。

- [ ] **Step 7: 调整 Assistant commit 时机并实现 Admission switch**

Action Usage/预算/契约后：无工具正常提交；有工具先 Validate 和 Admit。Allow/Warn/Recover 才 commit Assistant + message_end；Terminate 不 commit。Warn 将 `loopReminder` 保存到 `runState.pendingReminder`，下一 Action 构造 generationInput 后立即消费；Recover 生成全批 synthetic results，追加完整协议组后 return `done=false`。

固定 helper 形态和安全文本：

```go
const loopWarningText = "检测到重复工具调用。请检查结果是否发生变化；如果没有进展，请停止当前重试路径，改用不同方案，或明确说明无法继续。"
const loopRecoveryText = "工具循环护栏阻止了本批次执行：检测到重复且无进展的调用。请停止当前重试路径，改用不同方案，或明确说明无法继续。"

func loopReminder(_ *loopdetect.Intervention) ai.Message {
    return ai.Message{Role: ai.RoleSystem, Content: []ai.ContentBlock{ai.TextBlock(loopWarningText)}}
}

func syntheticLoopResults(calls ai.ToolCalls) []toolexec.Result {
    results := make([]toolexec.Result, len(calls))
    for i, call := range calls {
        results[i] = toolexec.Result{
            ToolCallID: call.ID, ToolName: call.Name,
            Content: []ai.ContentBlock{ai.TextBlock(loopRecoveryText)},
            IsError: true, ErrorCode: pierrors.ErrorCodeRunLoopDetected,
        }
    }
    return results
}
```

Pattern/count/tool names 只用于结构化日志与 Span，不插入 warning/recovery 正文。

- [ ] **Step 8: 实现工具结算与 post-outcome**

Allow/Warn 正常 plan/schedule/merge；合并后先 validate alignment，正常则 append results 后 Record。post-compaction Intervention 转 `*loopdetect.Error` 并在已提交协议组后结束。对齐失败按设计合成所有 internal Tool Messages、保留 Invocation/Totals、返回 internal error；不重发真实 live events，明确接受 live 与刷新后持久视图在这个内部故障路径不同。

对齐 helper 固定返回语义：

```go
func validateToolBatchOutcome(calls ai.ToolCalls, results []toolexec.Result) error {
    if len(calls) != len(results) {
        return fmt.Errorf("tool batch outcome count: calls=%d results=%d", len(calls), len(results))
    }
    for i := range calls {
        if calls[i].ID != results[i].ToolCallID || calls[i].Name != results[i].ToolName {
            return fmt.Errorf("tool batch outcome %d does not match call", i)
        }
    }
    return nil
}
```

失败时 synthetic internal results 必须以原始 calls 为源，不能复用已错位 results。

- [ ] **Step 9: 为每个 Run 和 child Loop 隔离 Detector**

Loop 只保存不可变 Config，`run` 方法局部 `detector := loopdetect.New(l.loopDetection)`；设置 `compactionRt.onCommitted = detector.ArmPostCompaction`。父/子 Loop Config 相同但实例独立；补并发根 Run 与两个并发子代理互不污染测试。

- [ ] **Step 10: 运行 Loop/子代理/race 测试**

Run: `go test ./pi -run 'TestToolLoop|TestToolBatchOutcome|TestSubagent' -count=1`

Expected: PASS。

Run: `go test -race ./pi -run 'TestToolLoop.*Concurrent|TestSubagent.*Concurrent' -count=1`

Expected: PASS，无 data race。

- [ ] **Step 11: 提交 Task 6**

```bash
git add pi/loop.go pi/loopdetect_run_test.go pi/subagent_test.go
git commit -m "feat: enforce tool loop admissions"
```

---

### Task 7: Bundled Config 与 Fx 根/子 Loop 装配

**Files:**
- Modify: `config/config.go`
- Modify: `config/load.go`
- Modify: `config/validate.go`
- Modify: `config/config_test.go`
- Modify: `config.example.json`
- Modify: `pi/register.go`
- Modify: `pi/register_test.go`
- Modify: `cmd/server/app.go`

**Interfaces:**
- Produces: `AgentConfig.LoopDetection loopdetect.Config`。
- Produces: `config.NewLoopDetectionConfig(*Config) loopdetect.Config`。
- Changes: Fx `loopParams` 和 `subagentBinderParams` optional `loopdetect.Config`；根/子 Loop 都传 `WithLoopDetection`。

- [ ] **Step 1: 写 config Load 校验测试**

表驱动覆盖 disabled/empty 合法；enabled + 精确工具名合法；`""`、`" read "`、重复 `"read"` 启动失败，并断言错误路径含 `agent.loop_detection.excluded_tools`。

- [ ] **Step 2: 写 Fx 根/子配置注入测试**

`CoreRegister` supply `loopdetect.Config{Enabled:true, ExcludedTools:[]string{"read"}}` 后 Populate Loop，断言不可变 Config 快照；不 supply 时零值关闭。Subagent binder 测试断言 child Loop 继承 Config 值但运行时各自 New Detector。

- [ ] **Step 3: 运行测试确认失败**

Run: `go test ./config ./pi -run 'Test.*(LoopDetection|CoreRegister|SubagentBinder)' -count=1`

Expected: FAIL，配置字段/provider/optional injection 尚不存在。

- [ ] **Step 4: 实现业务配置和 provider**

在 `AgentConfig` 增加字段；`AgentConfig.normalizeAndValidate` 在 workspace 处理后调用独立 `validateLoopDetection()`：遍历时用原值和 `strings.TrimSpace` 比较，空白、带前后空格、重复均报错，不修改列表。`NewLoopDetectionConfig` 返回复制后的 Config；SDK 防御性 trim/dedupe 仍由 Task 1 New 执行。

- [ ] **Step 5: 更新真实 Fx 落点和示例配置**

当前真实组合根是 `cmd/server/app.go` 的 `fx.Provide`，增加 `config.NewLoopDetectionConfig`；保留该文件已有用户改动。`loopParams`/`subagentBinderParams` optional 注入同一类型并传 Option。`config.example.json` 显式：

```json
"loop_detection": {
  "enabled": true,
  "excluded_tools": []
}
```

- [ ] **Step 6: 运行配置和装配测试**

Run: `go test ./config ./pi -run 'Test.*(Config|LoopDetection|CoreRegister|Subagent)' -count=1`

Expected: PASS。

- [ ] **Step 7: 提交 Task 7**

先用 `git diff -- cmd/server/app.go` 确认没有覆盖既有改动，再只暂存本任务文件：

```bash
git add config/config.go config/load.go config/validate.go config/config_test.go config.example.json pi/register.go pi/register_test.go cmd/server/app.go
git commit -m "feat: configure tool loop detection"
```

---

### Task 8: 可观测性、Notifier 与 Web 安全映射

**Files:**
- Modify: `pi/harness/observability/semantics.go`
- Modify: `pi/harness/observability/semantics_test.go`
- Modify: `pi/harness/observability/metrics.go`
- Modify: `pi/harness/observability/metrics_test.go`
- Modify: `pi/harness/observability/record.go`
- Modify: `pi/loop.go`
- Modify: `pi/tracing_run_test.go`
- Modify: `pi/notify_test.go`
- Modify: `application/service/chat/run_manager.go`
- Modify: `application/service/chat/run_manager_test.go`

**Interfaces:**
- Produces: `RecordLoopDetectionIntervention(ctx, pattern, level string)`。
- Produces constants: `MetricLoopDetectionInterventions`、`AttrLoopDetectionPattern/Level/Count`、`LabelPattern/Level`。
- Changes: Chat `runErrorVO` 对 `TerminationLoopDetected` 返回安全文案和 `Reason=loop_detected`。

- [ ] **Step 1: 写 Metric 定义/记录失败测试**

在 Domain definitions/kind 测试加入 intervention counter；调用：

```go
RecordLoopDetectionIntervention(ctx, "stable_outcome", "recovery")
```

断言只含 `pattern`、`level` labels，不含 tool/hash/arguments/content；记录 API Kind 与 Definition 都为 Counter。

- [ ] **Step 2: 写 Turn Span 属性测试**

触发 Warn/Recover，断言当前 `reagent.turn` Span 含 pattern/level/count；不含 ToolNames、hash、参数、结果正文。日志正文无法直接从现有测试抓取时，不为此引入日志 capture 抽象；代码审查确认只传结构化安全字段。

- [ ] **Step 3: 写 Notifier 和 Chat 安全错误测试**

Notifier：loop termination 产生一次 `NotificationRunError`，Summary 只含 `loop_detected`。Chat：

```go
termination := governor.Termination{Reason: governor.TerminationLoopDetected}
got := runErrorVO(loopErr, termination)
```

断言 `Reason == "loop_detected"`、Message 为“检测到重复且无进展的操作，本轮已停止，请调整请求后重试”，不含工具名、count、参数、价格或 Token，Code 使用 Conflict 而非 Internal。

- [ ] **Step 4: 运行测试确认失败**

Run: `go test ./pi/harness/observability ./pi ./application/service/chat -run 'Test.*(LoopDetection|LoopTermination)' -count=1`

Expected: FAIL，缺少 metrics/attrs/chat mapping。

- [ ] **Step 5: 实现低基数观测**

Loop 每次非 nil Intervention 调用一个 helper：在当前 Turn Span 写 pattern/level/count，调用 Counter，只以 pattern/level 为 labels；日志允许 ToolNames 作为结构化字段但禁止正文/hash。Allow 无 Intervention 不记录。

- [ ] **Step 6: 实现 Chat 映射并验证 live/persistence 取舍**

`runErrorVO` 增加 Loop reason case。Tool Result 对齐失败仍走 generic internal；真实 Tool start/end 已流出而持久历史为 synthetic unknown 是 bug-only 兜底的已接受不一致，不新增前端 reconcile 协议。

- [ ] **Step 7: 运行相关测试**

Run: `go test ./pi/harness/observability ./pi ./application/service/chat -run 'Test.*(Loop|Metric|Notifier|RunBudget)' -count=1`

Expected: PASS。

- [ ] **Step 8: 提交 Task 8**

```bash
git add pi/harness/observability/semantics.go pi/harness/observability/semantics_test.go pi/harness/observability/metrics.go pi/harness/observability/metrics_test.go pi/harness/observability/record.go pi/loop.go pi/tracing_run_test.go pi/notify_test.go application/service/chat/run_manager.go application/service/chat/run_manager_test.go
git commit -m "feat: observe tool loop interventions"
```

---

### Task 9: 全量回归、Race 与规格验收

**Files:**
- Modify only if verification exposes a feature-scoped defect in files already listed above.
- Do not modify unrelated user-owned files.

**Interfaces:**
- Consumes all prior tasks。
- Produces a verified implementation matching the approved spec。

- [ ] **Step 1: 运行 loopdetect 与核心 Pi 测试**

Run: `go test ./pi/loopdetect ./pi/... -count=1`

Expected: PASS，0 failures。

- [ ] **Step 2: 运行配置和应用测试**

Run: `go test ./config ./application/service/chat ./conversation/... -count=1`

Expected: PASS，0 failures。

- [ ] **Step 3: 运行全仓测试**

Run: `GOCACHE=/tmp/go-reagent-go-cache go test ./... -count=1`

Expected: PASS，0 failures。

- [ ] **Step 4: 运行高风险 Race 测试**

Run: `GOCACHE=/tmp/go-reagent-go-cache go test -race ./pi/... ./application/service/chat -count=1`

Expected: PASS，无 data race。

- [ ] **Step 5: 逐项核对关键不变量**

Run:

```bash
rg -n 'MaxTurns|MaxCostUSD|MaxTotalTokens' pi/loopdetect
rg -n 'governor' pi/loopdetect
rg -n 'loop-reminder|tool loop' pi/loop.go pi/recovery.go pi/compaction.go
git diff --check
```

Expected:

- 前两条无输出；Detector 不复制预算、不反向依赖 Governor。
- reminder 只在构造 Ephemeral/安全 synthetic result 的固定位置出现，不在持久化或 Compaction prompt 中散落。
- `git diff --check` 无输出。

- [ ] **Step 6: 检查工作树和提交范围**

Run: `git status --short`

Expected: 只剩任务开始前已经存在的用户改动；本功能文件均已提交。确认没有删除或覆盖 `infrastructure` 重构和 `docs/cli.md`。

- [ ] **Step 7: 处理验证期修复**

如果 Step 1-6 暴露缺陷，返回产生该缺陷的 Task，补一条先失败后通过的精确回归测试，并使用该 Task 已列出的显式 `git add` 文件清单提交；修复后重新执行 Task 9 全部命令。若没有新增改动，结束验证，禁止创建空 commit。
