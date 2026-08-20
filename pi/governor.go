package pi

import (
	"context"
	"errors"
	"fmt"
	"math"

	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
)

// runLimitError 标记一次运行达到了哪个维度的预算上限。
type runLimitError struct {
	kind RunLimitKind
}

func (err *runLimitError) Error() string {
	return fmt.Sprintf("%v: %s", pierrors.ErrRunLimitExceeded, err.kind)
}

func (err *runLimitError) Unwrap() error { return pierrors.ErrRunLimitExceeded }

// runGovernor 是一次 Run 的请求级预算累计与准入状态。它不属于共享的
// Agent、Loop、Provider 或 CostTracker，每次 Agent.Run 创建一个新实例。
type runGovernor struct {
	limits RunLimits
	totals RunTotals
	// costCompensation 是 Kahan 补偿项，降低多次成本累加的浮点误差。
	costCompensation float64
}

func newRunGovernor(limits RunLimits) *runGovernor {
	return &runGovernor{limits: limits}
}

// beforeTurn 在下一 turn 的 Thinking 之前检查 MaxTurns。
func (g *runGovernor) beforeTurn() error {
	if g.limits.MaxTurns > 0 && g.totals.Turns >= g.limits.MaxTurns {
		return pierrors.Wrap(pierrors.ErrorCodeRunLimitExceeded, "run budget", &runLimitError{kind: RunLimitTurns})
	}
	return nil
}

// startTurn 只在确定将进入该 turn 时递增。
func (g *runGovernor) startTurn() {
	g.totals.Turns++
}

func (g *runGovernor) getTurns() int {
	return g.totals.Turns
}

// observe 接收已经通过 validateMeteredUsage 的 Invocation，先累加，再判断
// cost/token 是否达到上限。每个 Invocation 只能 observe 一次。
func (g *runGovernor) observe(invocation ModelInvocation) error {
	input, ok := checkedAddInt64(g.totals.InputTokens, invocation.Usage.InputTokens)
	if !ok {
		return runTotalsOverflow("input tokens")
	}

	output, ok := checkedAddInt64(g.totals.OutputTokens, invocation.Usage.OutputTokens)
	if !ok {
		return runTotalsOverflow("output tokens")
	}

	g.totals.InputTokens = input
	g.totals.OutputTokens = output
	g.totals.TotalTokens = input + output
	if g.totals.Invocations == math.MaxUint32 {
		return runTotalsOverflow("invocations")
	}
	g.totals.Invocations++

	// Kahan 补偿求和。
	y := invocation.Usage.CostUSD - g.costCompensation
	t := g.totals.CostUSD + y
	g.costCompensation = (t - g.totals.CostUSD) - y
	g.totals.CostUSD = t
	if math.IsNaN(g.totals.CostUSD) || math.IsInf(g.totals.CostUSD, 0) {
		return runTotalsOverflow("cost")
	}

	// 判断是否达到上限
	if g.limits.MaxCostUSD > 0 && g.totals.CostUSD >= g.limits.MaxCostUSD {
		return pierrors.Wrap(pierrors.ErrorCodeRunLimitExceeded, "run budget", &runLimitError{kind: RunLimitCostUSD})
	}
	if g.limits.MaxTotalTokens > 0 && g.totals.TotalTokens >= g.limits.MaxTotalTokens {
		return pierrors.Wrap(pierrors.ErrorCodeRunLimitExceeded, "run budget", &runLimitError{kind: RunLimitTotalTokens})
	}

	return nil
}

// termination 把一次运行的结束 error 映射为结构化终止结果。
func (g *runGovernor) termination(err error) RunTermination {
	return terminationFromError(err, g.totals)
}

func checkedAddInt64(a, b int64) (int64, bool) {
	if b > 0 && a > math.MaxInt64-b {
		return 0, false
	}
	return a + b, true
}

func runTotalsOverflow(field string) error {
	return pierrors.Wrap(
		pierrors.ErrorCodeInternal,
		"run budget",
		fmt.Errorf("run totals %s exceeded the supported range", field),
	)
}

// terminationFromError 不依赖 Governor 把结束 error 映射为终止原因，
// 同时供 Loop 前的早期返回路径使用。
func terminationFromError(err error, totals RunTotals) RunTermination {
	termination := RunTermination{Totals: totals}
	if err == nil {
		termination.Reason = RunTerminationCompleted
		return termination
	}
	var limitErr *runLimitError
	switch {
	case errors.Is(err, context.Canceled):
		termination.Reason = RunTerminationCanceled
	case errors.Is(err, context.DeadlineExceeded):
		termination.Reason = RunTerminationDeadline
	case errors.As(err, &limitErr):
		termination.Limit = limitErr.kind
		switch limitErr.kind {
		case RunLimitTurns:
			termination.Reason = RunTerminationMaxTurns
		case RunLimitCostUSD:
			termination.Reason = RunTerminationMaxCost
		case RunLimitTotalTokens:
			termination.Reason = RunTerminationMaxTotalTokens
		default:
			termination.Reason = RunTerminationError
		}
	default:
		termination.Reason = RunTerminationError
	}
	return termination
}
