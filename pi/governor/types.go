// Package governor 是 Run 治理域：一次运行的资源上限（Limits）、预算
// 累计与准入（Governor）、终止分类（Termination）、模型调用计量
// （Invocation），以及父子运行间经 ctx 传递这些原语的管道件。
package governor

import (
	"fmt"
	"math"

	"github.com/PycMono/go-reagent/pi/ai"
	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
)

// Limits 保存一次运行的确定性资源上限。每个维度的零值只表示该维度不限制。
type Limits struct {
	// MaxTurns 是外层 Agent turn 上限。0 表示不限制。
	MaxTurns int `json:"max_turns,omitempty" yaml:"max_turns" toml:"max_turns"`
	// MaxCostUSD 是所有已完成且可计量模型调用的累计美元成本上限。0 表示不限制。
	MaxCostUSD float64 `json:"max_cost_usd,omitempty" yaml:"max_cost_usd" toml:"max_cost_usd"`
	// MaxTotalTokens 是所有已完成且可计量模型调用的
	// InputTokens + OutputTokens 累计上限。0 表示不限制。
	MaxTotalTokens int64 `json:"max_total_tokens,omitempty" yaml:"max_total_tokens" toml:"max_total_tokens"`
}

// Validate 校验额度值的固有契约：不允许负数、NaN 或无穷；零值只表示不限制。
func (limits Limits) Validate() error {
	if limits.MaxTurns < 0 {
		return fmt.Errorf("%w: max turns must not be negative", pierrors.ErrRequestInvalid)
	}
	if limits.MaxTotalTokens < 0 {
		return fmt.Errorf("%w: max total tokens must not be negative", pierrors.ErrRequestInvalid)
	}
	if limits.MaxCostUSD < 0 || math.IsNaN(limits.MaxCostUSD) || math.IsInf(limits.MaxCostUSD, 0) {
		return fmt.Errorf("%w: max cost usd must be finite and non-negative", pierrors.ErrRequestInvalid)
	}
	return nil
}

// InvocationPhase 表示 Agent 调用模型的阶段。
type InvocationPhase string

const (
	// PhaseThinking 表示内部思考阶段的模型调用。
	PhaseThinking InvocationPhase = "thinking"
	// PhaseAction 表示生成回复或工具调用的模型调用。
	PhaseAction InvocationPhase = "action"
	// PhaseCompaction 表示上下文摘要阶段的模型调用。
	PhaseCompaction InvocationPhase = "compaction"
	// PhaseSubagent 表示子代理运行内的模型调用（入账父账本；
	// 指标层保留子运行真实 Phase；归属经 Trace 的 reagent.subagent.name 表达）。
	PhaseSubagent InvocationPhase = "subagent"
)

// InvocationOutcome 是可信 Invocation 的契约验收结果。
type InvocationOutcome string

const (
	// OutcomeAccepted 表示契约校验通过。
	OutcomeAccepted InvocationOutcome = "accepted"
	// OutcomeContractInvalid 表示已取得可信 Usage 但 Thinking/Action/
	// Compaction 契约校验失败；调用已计费，必须入账。
	OutcomeContractInvalid InvocationOutcome = "contract_invalid"
)

// Invocation 记录一次已完成且已计量的模型调用。
type Invocation struct {
	// Sequence 是本次运行内从 1 开始的可信调用顺序。
	Sequence uint32 `json:"sequence"`
	// Phase 是本次模型调用所处的阶段。
	Phase InvocationPhase `json:"phase"`
	// Usage 是本次模型调用的令牌、成本和耗时信息。
	Usage ai.Usage `json:"usage"`
	// Outcome 是契约验收结果；新 Invocation 必须显式写入，不依赖数据库默认值。
	Outcome InvocationOutcome `json:"outcome"`
	// ProviderRequestIndex 是本次调用对应的物理 Provider 请求序号，
	// 与 Invocation Sequence 分离；用于在 Trace 中定位唯一 Provider Span。
	ProviderRequestIndex uint32 `json:"provider_request_index,omitempty"`
	// FinishReason 是模型结束当前响应的统一原因。
	FinishReason string `json:"finish_reason,omitempty"`
}

// Totals 汇总一次运行内所有已记录模型调用的跨调用累计。
type Totals struct {
	// Turns 是已开始的 Agent turn 数。
	Turns int `json:"turns"`
	// Invocations 是已记录的模型调用数，恒等于 Invocations 明细长度。
	Invocations uint32 `json:"invocations"`
	// InputTokens 是累计输入令牌数。
	InputTokens int64 `json:"input_tokens"`
	// OutputTokens 是累计输出令牌数。
	OutputTokens int64 `json:"output_tokens"`
	// TotalTokens 恒等于 InputTokens + OutputTokens。
	TotalTokens int64 `json:"total_tokens"`
	// CostUSD 是累计美元成本。
	CostUSD float64 `json:"cost_usd"`
}

// TerminationReason 表示一次运行结束的确定性原因。
type TerminationReason string

const (
	// TerminationCompleted 表示得到无需工具的最终 Assistant 响应。
	TerminationCompleted TerminationReason = "completed"
	// TerminationError 表示因请求、契约或内部错误结束。
	TerminationError TerminationReason = "error"
	// TerminationCanceled 表示调用方取消了运行。
	TerminationCanceled TerminationReason = "canceled"
	// TerminationDeadline 表示运行超过调用方 deadline。
	TerminationDeadline TerminationReason = "deadline_exceeded"
	// TerminationMaxTurns 表示达到 MaxTurns。
	TerminationMaxTurns TerminationReason = "max_turns"
	// TerminationMaxCost 表示达到 MaxCostUSD。
	TerminationMaxCost TerminationReason = "max_cost"
	// TerminationMaxTotalTokens 表示达到 MaxTotalTokens。
	TerminationMaxTotalTokens TerminationReason = "max_total_tokens"
	// TerminationLoopDetected 表示行为循环检测熔断（第二阶段）。
	TerminationLoopDetected TerminationReason = "loop_detected"
)

// LimitKind 表示触发预算终止的额度维度。
type LimitKind string

const (
	// LimitTurns 表示 Agent turn 额度。
	LimitTurns LimitKind = "turns"
	// LimitCostUSD 表示累计美元成本额度。
	LimitCostUSD LimitKind = "cost_usd"
	// LimitTotalTokens 表示累计 Token 额度。
	LimitTotalTokens LimitKind = "total_tokens"
)

// Termination 是每次运行返回时的结构化终止结果。
type Termination struct {
	// Reason 是终止原因，每个返回路径都必须有值。
	Reason TerminationReason `json:"reason"`
	// Limit 是触发预算终止的额度维度，仅预算终止时有值。
	Limit LimitKind `json:"limit,omitempty"`
	// Totals 是终止时刻的累计值。
	Totals Totals `json:"totals"`
}
