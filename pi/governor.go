package pi

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"

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

// errParentBudgetExhausted 是父预算触顶取消 batchCtx 的专属 cause（§批次取消语义）：
// 用于把内部预算取消与用户取消/真实 deadline 区分开。
var errParentBudgetExhausted = errors.New("parent run budget exhausted")

// runGovernor 是一次 Run 的请求级预算累计与准入状态。它不属于共享的
// Agent、Loop、Provider 或 CostTracker，每次 Agent.Run 创建一个新实例。
//
// Governor 是并发安全的：子代理运行经 batchBudget.debit 在工具批次内并发
// 扣减父预算。parent 仅存在于子代理的 Governor：子运行 Invocation 完成时
// 除自身累加外，同步扣减父预算账户。
type runGovernor struct {
	mu       sync.Mutex
	limits   RunLimits
	totals   RunTotals
	// costCompensation 是 Kahan 补偿项，降低多次成本累加的浮点误差。
	costCompensation float64
	// exhausted 与 firstErr 记录本 Run 首次预算越界：触顶后在飞调用仍须
	// 累加 Totals（保持账本与 Totals 一致），但只保留首个错误。
	exhausted bool
	firstErr  error
	// parent 是子代理 Governor 关联的父预算账户；主运行为 nil。
	parent *batchBudget
}

func newRunGovernor(limits RunLimits) *runGovernor {
	return &runGovernor{limits: limits}
}

// batchBudget 是一个工具批次内的父预算账户，经 ctx 传给子代理运行。
// 取消回调不存 Governor：每批独立持有 cancelCause 与 once。
type batchBudget struct {
	governor *runGovernor
	cancel   context.CancelCauseFunc
	once     sync.Once
}

// debit 供子运行在每次子 Invocation 完成时立即扣减父预算：
//   - 无论是否已触顶都累加 Totals（触顶后在飞调用仍须入账，保持账本与
//     Totals 一致）；
//   - 首次触顶记录 firstErr，并在锁外以 errParentBudgetExhausted 取消
//     batchCtx（每批最多取消一次）；
//   - 返回首个预算错误（未越界返回 nil）。
func (b *batchBudget) debit(invocation ModelInvocation) error {
	if b == nil || b.governor == nil {
		return nil
	}
	g := b.governor
	g.mu.Lock()
	err := g.accumulateLocked(invocation)
	g.mu.Unlock()
	if err != nil {
		b.once.Do(func() {
			b.cancel(errParentBudgetExhausted)
		})
	}
	g.mu.Lock()
	firstErr := g.firstErr
	g.mu.Unlock()
	return firstErr
}

// exhausted 报告父预算是否已触顶（供子 Governor beforeTurn 检查）。
func (b *batchBudget) exhausted() bool {
	if b == nil || b.governor == nil {
		return false
	}
	b.governor.mu.Lock()
	defer b.governor.mu.Unlock()
	return b.governor.exhausted
}

// firstBudgetError 返回父预算首个越界错误（未触顶返回 nil）。
func (b *batchBudget) firstBudgetError() error {
	if b == nil || b.governor == nil {
		return nil
	}
	b.governor.mu.Lock()
	defer b.governor.mu.Unlock()
	return b.governor.firstErr
}

// accumulateLocked 累加 Invocation 并在首次越界时记录 firstErr/exhausted；
// 调用方必须持有 g.mu。返回首次记录的预算错误（未越界或已触顶返回 nil）。
func (g *runGovernor) accumulateLocked(invocation ModelInvocation) error {
	input, ok := checkedAddInt64(g.totals.InputTokens, invocation.Usage.InputTokens)
	if !ok {
		return g.setFirstErrLocked(runTotalsOverflow("input tokens"))
	}

	output, ok := checkedAddInt64(g.totals.OutputTokens, invocation.Usage.OutputTokens)
	if !ok {
		return g.setFirstErrLocked(runTotalsOverflow("output tokens"))
	}

	g.totals.InputTokens = input
	g.totals.OutputTokens = output
	g.totals.TotalTokens = input + output
	if g.totals.Invocations == math.MaxUint32 {
		return g.setFirstErrLocked(runTotalsOverflow("invocations"))
	}
	g.totals.Invocations++

	// Kahan 补偿求和。
	y := invocation.Usage.CostUSD - g.costCompensation
	t := g.totals.CostUSD + y
	g.costCompensation = (t - g.totals.CostUSD) - y
	g.totals.CostUSD = t
	if math.IsNaN(g.totals.CostUSD) || math.IsInf(g.totals.CostUSD, 0) {
		return g.setFirstErrLocked(runTotalsOverflow("cost"))
	}

	// 判断是否达到上限
	if g.limits.MaxCostUSD > 0 && g.totals.CostUSD >= g.limits.MaxCostUSD {
		return g.setFirstErrLocked(pierrors.Wrap(pierrors.ErrorCodeRunLimitExceeded, "run budget", &runLimitError{kind: RunLimitCostUSD}))
	}
	if g.limits.MaxTotalTokens > 0 && g.totals.TotalTokens >= g.limits.MaxTotalTokens {
		return g.setFirstErrLocked(pierrors.Wrap(pierrors.ErrorCodeRunLimitExceeded, "run budget", &runLimitError{kind: RunLimitTotalTokens}))
	}

	return nil
}

// setFirstErrLocked 只在首次越界时记录错误并置位 exhausted；返回的错误
// 仅在本次触顶时非 nil。调用方必须持有 g.mu。
func (g *runGovernor) setFirstErrLocked(err error) error {
	if g.exhausted {
		return nil
	}
	g.exhausted = true
	g.firstErr = err
	return err
}

// beforeTurn 在下一 turn 的 Thinking 之前检查 MaxTurns；子代理 Governor
// 额外检查父预算账户是否已触顶。
func (g *runGovernor) beforeTurn() error {
	g.mu.Lock()
	turnsErr := error(nil)
	if g.limits.MaxTurns > 0 && g.totals.Turns >= g.limits.MaxTurns {
		turnsErr = pierrors.Wrap(pierrors.ErrorCodeRunLimitExceeded, "run budget", &runLimitError{kind: RunLimitTurns})
	}
	g.mu.Unlock()
	if turnsErr != nil {
		return turnsErr
	}
	if g.parent != nil && g.parent.exhausted() {
		return g.parent.firstBudgetError()
	}
	return nil
}

// startTurn 只在确定将进入该 turn 时递增。
func (g *runGovernor) startTurn() {
	g.mu.Lock()
	g.totals.Turns++
	g.mu.Unlock()
}

func (g *runGovernor) getTurns() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.totals.Turns
}

// observe 接收已经通过 validateMeteredUsage 的 Invocation：先自身累加并检查
// 子 Limits，再（子代理场景）经父账户扣减父预算——父扣减永远执行，子触顶
// 不跳过。返回综合错误：子预算错误优先，其次父预算错误。
// 每个 Invocation 只能 observe 一次。
func (g *runGovernor) observe(invocation ModelInvocation) error {
	g.mu.Lock()
	childErr := g.accumulateLocked(invocation)
	g.mu.Unlock()
	var parentErr error
	if g.parent != nil {
		parentErr = g.parent.debit(invocation)
	}
	if childErr != nil {
		return childErr
	}
	return parentErr
}

// termination 把一次运行的结束 error 映射为结构化终止结果。
func (g *runGovernor) termination(err error) RunTermination {
	g.mu.Lock()
	totals := g.totals
	g.mu.Unlock()
	return terminationFromError(err, totals)
}

// firstBudgetError 返回本 Run 首个预算越界错误（未触顶返回 nil）。
func (g *runGovernor) firstBudgetError() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.firstErr
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
