package loopdetect

import (
	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/toolexec"
)

// Decision 是一次整批准入后 Loop 必须执行的明确决定。
type Decision string

const (
	// DecisionAllow 允许整批工具执行。
	DecisionAllow Decision = "allow"
	// DecisionWarn 允许整批工具执行，并携带一次只对模型可见的提醒证据。
	DecisionWarn Decision = "warn"
	// DecisionRecover 阻止整批工具执行，给予模型一次恢复 turn。
	DecisionRecover Decision = "recover"
	// DecisionTerminate 阻止整批工具执行并终止 Run。
	DecisionTerminate Decision = "terminate"
)

// Level 是一次干预的严重级别。
type Level string

const (
	LevelWarning     Level = "warning"
	LevelRecovery    Level = "recovery"
	LevelTermination Level = "termination"
)

// Pattern 是触发干预的行为模式。
type Pattern string

const (
	// PatternRepeatedCall 表示同一 Call signature 的重复调用达到提醒阈值。
	PatternRepeatedCall Pattern = "repeated_call"
	// PatternStableOutcome 表示同一 Call signature 连续确认多个完全相同的
	// Outcome signature，构成确定的无进展证据。
	PatternStableOutcome Pattern = "stable_outcome"
)

// Intervention 是产生 Warn/Recover/Terminate 决定的安全行为证据：只含模型
// 已经知道的工具名，不含参数、结果、路径、命令、URL 或消息正文。
type Intervention struct {
	Level   Level
	Pattern Pattern
	// Count 是触发该干预的计数值：规则 1 取 projected count，规则 2 取
	// 已确认 stable count。
	Count int
	// ToolNames 去重并按字典序排序，保证日志与测试稳定。
	ToolNames []string
}

// Admission 是一次整批准入的完整返回值。
type Admission struct {
	Decision Decision
	// Intervention 在 DecisionAllow 时恒为 nil。
	Intervention *Intervention
}

// New 创建一个请求级 Detector。Config 零值即默认启用策略；New 不返回配置
// 错误，只对 ExcludedTools 做 trim、去空、去重的防御性归一化，且不修改
// 调用方传入的 Config。
func New(config Config) *Detector {
	return newDetector(config.normalize())
}

// AdmitToolBatch 在副作用发生前对整个 Tool Calls 批次做一次纯内存、无副
// 作用的原子准入判定。Disabled 时恒返回 DecisionAllow。
func (d *Detector) AdmitToolBatch(calls ai.ToolCalls) Admission {
	return d.admit(calls)
}

// RecordToolBatchOutcome 在副作用完成后做事实记账，无返回值、不产生干预
// 决定；所有干预都在下一次 AdmitToolBatch 准入时发生。
//
// 要求 len(calls) == len(events)、events[i] 是结束事件且其中的 Call 与
// calls[i] 一致；该不变量由 Loop 在调用前保证，Detector 对不匹配输入不修改
// 状态、不产生任何效果（防御性兜底）。Disabled 时无任何副作用。
func (d *Detector) RecordToolBatchOutcome(calls ai.ToolCalls, events []toolexec.Event) {
	d.record(calls, events)
}
