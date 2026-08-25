package governor

import (
	"context"
	"errors"
	"math"
	"sync"
	"sync/atomic"

	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
)

// primitives.go 集中管理经 ctx 在父子运行间传递的 Run 级原语：
// 物理请求序号器、父预算账户、子调用账本记录器与子代理深度。

type primitiveKey string

const (
	keySequencer    primitiveKey = "sequencer"
	keyBatchBudget  primitiveKey = "batch_budget"
	keyRecorder     primitiveKey = "invocation_recorder"
	keySubagentDeep primitiveKey = "subagent_depth"
)

// Sequencer 是 Run 级物理请求序号分配器，并发安全。
// 根 Loop.run 创建并经 ctx 传递；子运行复用，父子共享单调序号空间，
// 保证 ProviderRequestIndex 在整个 Run（含子代理）内唯一。
type Sequencer struct {
	value atomic.Uint32
}

func NewSequencer() *Sequencer {
	return &Sequencer{}
}

// Next 返回从 1 开始的唯一序号；uint32 耗尽时返回内部错误。
// CAS 循环保证计数器永不写入 0，因此不存在"回绕后重新从 1 分配"的窗口，
// 维持非零唯一契约（实际不可达，防御）。
func (s *Sequencer) Next() (uint32, error) {
	for {
		current := s.value.Load()
		if current == math.MaxUint32 {
			return 0, pierrors.Wrap(pierrors.ErrorCodeInternal, "request sequencer",
				errors.New("provider request index overflow"))
		}
		if s.value.CompareAndSwap(current, current+1) {
			return current + 1, nil
		}
	}
}

// SequencerFromCtx 返回 ctx 中的序号器；没有时创建并返回新序号器与携带它的
// ctx——根运行为自己创建，子运行复用父 Run 的。
func SequencerFromCtx(ctx context.Context) (*Sequencer, context.Context) {
	if sequencer, ok := ctx.Value(keySequencer).(*Sequencer); ok && sequencer != nil {
		return sequencer, ctx
	}
	sequencer := NewSequencer()
	return sequencer, context.WithValue(ctx, keySequencer, sequencer)
}

// WithBatchBudget 把本批父预算账户挂到 ctx 供子代理工具读取。
func WithBatchBudget(ctx context.Context, account *BatchBudget) context.Context {
	return context.WithValue(ctx, keyBatchBudget, account)
}

func BatchBudgetFromCtx(ctx context.Context) *BatchBudget {
	account, _ := ctx.Value(keyBatchBudget).(*BatchBudget)
	return account
}

// RunReport 是子运行结束后上报父运行结算阶段的记录。
// 不含 Totals：父账本按 Invocation 粒度入账，父 Turns 不合并子 turns。
type RunReport struct {
	Agent       string
	Invocations []Invocation
}

// InvocationRecorder 收集工具执行期间（可能并发）完成的子运行调用记录，
// 由 Loop 在工具批次结束后单线程排空入账。
type InvocationRecorder struct {
	mu      sync.Mutex
	reports []RunReport
}

func NewInvocationRecorder() *InvocationRecorder {
	return &InvocationRecorder{}
}

func (r *InvocationRecorder) Record(report RunReport) {
	if len(report.Invocations) == 0 {
		return
	}
	r.mu.Lock()
	r.reports = append(r.reports, report)
	r.mu.Unlock()
}

func (r *InvocationRecorder) Drain() []RunReport {
	r.mu.Lock()
	defer r.mu.Unlock()
	reports := r.reports
	r.reports = nil
	return reports
}

// WithInvocationRecorder 把本批账本记录器挂到 ctx 供子代理工具读取。
func WithInvocationRecorder(ctx context.Context, recorder *InvocationRecorder) context.Context {
	return context.WithValue(ctx, keyRecorder, recorder)
}

// RecorderFromCtx 返回 ctx 中的账本记录器；没有时返回 nil（子代理工具
// 在 Loop 外直接调用时退化为不上报，调用方需判空）。
func RecorderFromCtx(ctx context.Context) *InvocationRecorder {
	recorder, _ := ctx.Value(keyRecorder).(*InvocationRecorder)
	return recorder
}

// MaxSubagentDepth 是子代理嵌套深度上限：1 表示子代理不能再拉起子代理。
const MaxSubagentDepth = 1

func WithSubagentDepth(ctx context.Context, depth int) context.Context {
	return context.WithValue(ctx, keySubagentDeep, depth)
}

func SubagentDepth(ctx context.Context) int {
	depth, _ := ctx.Value(keySubagentDeep).(int)
	return depth
}
