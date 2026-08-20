package pi

import (
	"fmt"
	"math"
	"strings"

	"github.com/PycMono/go-reagent/pi/ai"
	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
)

// RunRequest 保存一次无状态运行所需的调用方输入。
type RunRequest struct {
	// History 是本轮运行开始前、面向业务的文本会话历史。
	History []Message `json:"history,omitempty"`
	// Input 是本轮用户输入消息。
	Input Message `json:"input"`
	// Context 是本轮额外注入的业务上下文。
	Context []ContextBlock `json:"context,omitempty"`
	// Limits 是本轮运行的确定性资源上限；全零表示不限制。
	Limits RunLimits `json:"limits,omitempty"`
}

// Validate 校验请求的固有契约。它不触碰 History/Input 转换、Context
// 构造或任何 Provider 调用，调用方必须在那些步骤之前执行。
func (request RunRequest) Validate() error {
	if err := request.Limits.Validate(); err != nil {
		return err
	}
	for index, block := range request.Context {
		if strings.TrimSpace(block.Name) == "" {
			return fmt.Errorf("%w: context block %d name must not be empty", pierrors.ErrRequestInvalid, index)
		}
		if strings.TrimSpace(block.Content) == "" {
			return fmt.Errorf("%w: context block %d content must not be empty", pierrors.ErrRequestInvalid, index)
		}
	}

	return nil
}

// RunLimits 保存一次运行的确定性资源上限。每个维度的零值只表示该维度不限制。
type RunLimits struct {
	// MaxTurns 是外层 Agent turn 上限。0 表示不限制。
	MaxTurns int `json:"max_turns,omitempty" yaml:"max_turns" toml:"max_turns"`
	// MaxCostUSD 是所有已完成且可计量模型调用的累计美元成本上限。0 表示不限制。
	MaxCostUSD float64 `json:"max_cost_usd,omitempty" yaml:"max_cost_usd" toml:"max_cost_usd"`
	// MaxTotalTokens 是所有已完成且可计量模型调用的
	// InputTokens + OutputTokens 累计上限。0 表示不限制。
	MaxTotalTokens int64 `json:"max_total_tokens,omitempty" yaml:"max_total_tokens" toml:"max_total_tokens"`
}

// Validate 校验额度值的固有契约：不允许负数、NaN 或无穷；零值只表示不限制。
func (limits RunLimits) Validate() error {
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

// ContextBlock 表示运行时注入到会话历史之前的一段业务上下文。
type ContextBlock struct {
	// Name 是上下文名称。
	Name string `json:"name"`
	// Content 是上下文内容。
	Content string `json:"content"`
	// Priority 决定上下文的排列顺序，数值越大越靠前。
	Priority int `json:"priority,omitempty"`
}

// ModelInvocationPhase 表示 Agent 调用模型的阶段。
type ModelInvocationPhase string

const (
	// ModelInvocationPhaseThinking 表示内部思考阶段的模型调用。
	ModelInvocationPhaseThinking ModelInvocationPhase = "thinking"
	// ModelInvocationPhaseAction 表示生成回复或工具调用的模型调用。
	ModelInvocationPhaseAction ModelInvocationPhase = "action"
	// ModelInvocationPhaseCompaction 表示上下文摘要阶段的模型调用。
	ModelInvocationPhaseCompaction ModelInvocationPhase = "compaction"
)

// ModelInvocation 记录一次已完成且已计量的模型调用。
type ModelInvocation struct {
	// Sequence 是本次运行内从 1 开始的模型调用顺序。
	Sequence uint32 `json:"sequence"`
	// Phase 是本次模型调用所处的阶段。
	Phase ModelInvocationPhase `json:"phase"`
	// Usage 是本次模型调用的令牌、成本和耗时信息。
	Usage ai.Usage `json:"usage"`
}

// RunTotals 汇总一次运行内所有已记录模型调用的跨调用累计。
type RunTotals struct {
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

// RunTerminationReason 表示一次运行结束的确定性原因。
type RunTerminationReason string

const (
	// RunTerminationCompleted 表示得到无需工具的最终 Assistant 响应。
	RunTerminationCompleted RunTerminationReason = "completed"
	// RunTerminationError 表示因请求、契约或内部错误结束。
	RunTerminationError RunTerminationReason = "error"
	// RunTerminationCanceled 表示调用方取消了运行。
	RunTerminationCanceled RunTerminationReason = "canceled"
	// RunTerminationDeadline 表示运行超过调用方 deadline。
	RunTerminationDeadline RunTerminationReason = "deadline_exceeded"
	// RunTerminationMaxTurns 表示达到 MaxTurns。
	RunTerminationMaxTurns RunTerminationReason = "max_turns"
	// RunTerminationMaxCost 表示达到 MaxCostUSD。
	RunTerminationMaxCost RunTerminationReason = "max_cost"
	// RunTerminationMaxTotalTokens 表示达到 MaxTotalTokens。
	RunTerminationMaxTotalTokens RunTerminationReason = "max_total_tokens"
	// RunTerminationLoopDetected 表示行为循环检测熔断（第二阶段）。
	RunTerminationLoopDetected RunTerminationReason = "loop_detected"
)

// RunLimitKind 表示触发预算终止的额度维度。
type RunLimitKind string

const (
	// RunLimitTurns 表示 Agent turn 额度。
	RunLimitTurns RunLimitKind = "turns"
	// RunLimitCostUSD 表示累计美元成本额度。
	RunLimitCostUSD RunLimitKind = "cost_usd"
	// RunLimitTotalTokens 表示累计 Token 额度。
	RunLimitTotalTokens RunLimitKind = "total_tokens"
)

// RunTermination 是每次运行返回时的结构化终止结果。
type RunTermination struct {
	// Reason 是终止原因，每个返回路径都必须有值。
	Reason RunTerminationReason `json:"reason"`
	// Limit 是触发预算终止的额度维度，仅预算终止时有值。
	Limit RunLimitKind `json:"limit,omitempty"`
	// Totals 是终止时刻的累计值。
	Totals RunTotals `json:"totals"`
}

// RunResult 保存一次运行中新产生的消息、模型调用记录和终止结果。
type RunResult struct {
	// NewMessages 是本次运行新增的 Assistant 和 Tool 消息。
	NewMessages []ai.Message `json:"new_messages,omitempty"`
	// Invocations 是本次运行按顺序完成的模型调用记录。
	Invocations []ModelInvocation `json:"invocations,omitempty"`
	// Termination 是本次运行的结构化终止结果。
	Termination RunTermination `json:"termination"`
}
