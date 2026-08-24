package pi

import (
	"context"
	"errors"
	"math"
	"sync"
	"sync/atomic"

	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
)

// run_primitives.go 集中管理经 ctx 在父子运行间传递的 Run 级原语：
// 物理请求序号器、父预算账户、子调用账本记录器与子代理深度。

type runPrimitiveKey string

const (
	keySequencer    runPrimitiveKey = "sequencer"
	keyBatchBudget  runPrimitiveKey = "batch_budget"
	keyRecorder     runPrimitiveKey = "invocation_recorder"
	keySubagentDeep runPrimitiveKey = "subagent_depth"
)

// requestSequencer 是 Run 级物理请求序号分配器，并发安全。
// 根 Loop.run 创建并经 ctx 传递；子运行复用，父子共享单调序号空间，
// 保证 ProviderRequestIndex 在整个 Run（含子代理）内唯一。
type requestSequencer struct {
	value atomic.Uint32
}

func newRequestSequencer() *requestSequencer {
	return &requestSequencer{}
}

// next 返回从 1 开始的唯一序号；uint32 耗尽时返回内部错误。
// CAS 循环保证计数器永不写入 0，因此不存在"回绕后重新从 1 分配"的窗口，
// 维持非零唯一契约（实际不可达，防御）。
func (s *requestSequencer) next() (uint32, error) {
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

// sequencerFromCtx 返回 ctx 中的序号器；没有时创建并返回新序号器与携带它的
// ctx——根运行为自己创建，子运行复用父 Run 的。
func sequencerFromCtx(ctx context.Context) (*requestSequencer, context.Context) {
	if sequencer, ok := ctx.Value(keySequencer).(*requestSequencer); ok && sequencer != nil {
		return sequencer, ctx
	}
	sequencer := newRequestSequencer()
	return sequencer, context.WithValue(ctx, keySequencer, sequencer)
}

// withBatchBudget 把本批父预算账户挂到 ctx 供子代理工具读取。
func withBatchBudget(ctx context.Context, account *batchBudget) context.Context {
	return context.WithValue(ctx, keyBatchBudget, account)
}

func batchBudgetFromCtx(ctx context.Context) *batchBudget {
	account, _ := ctx.Value(keyBatchBudget).(*batchBudget)
	return account
}

// subagentRunReport 是子运行结束后上报父运行结算阶段的记录。
// 不含 Totals：父账本按 Invocation 粒度入账，父 Turns 不合并子 turns。
type subagentRunReport struct {
	Agent       string
	Invocations []ModelInvocation
}

// invocationRecorder 收集工具执行期间（可能并发）完成的子运行调用记录，
// 由 Loop 在工具批次结束后单线程排空入账。
type invocationRecorder struct {
	mu      sync.Mutex
	reports []subagentRunReport
}

func (r *invocationRecorder) record(report subagentRunReport) {
	if len(report.Invocations) == 0 {
		return
	}
	r.mu.Lock()
	r.reports = append(r.reports, report)
	r.mu.Unlock()
}

func (r *invocationRecorder) drain() []subagentRunReport {
	r.mu.Lock()
	defer r.mu.Unlock()
	reports := r.reports
	r.reports = nil
	return reports
}

// withInvocationRecorder 把本批账本记录器挂到 ctx 供子代理工具读取。
func withInvocationRecorder(ctx context.Context, recorder *invocationRecorder) context.Context {
	return context.WithValue(ctx, keyRecorder, recorder)
}

// recorderFromCtx 返回 ctx 中的账本记录器；没有时返回 nil（子代理工具
// 在 Loop 外直接调用时退化为不上报，调用方需判空）。
func recorderFromCtx(ctx context.Context) *invocationRecorder {
	recorder, _ := ctx.Value(keyRecorder).(*invocationRecorder)
	return recorder
}

// maxSubagentDepth 是子代理嵌套深度上限：1 表示子代理不能再拉起子代理。
const maxSubagentDepth = 1

func withSubagentDepth(ctx context.Context, depth int) context.Context {
	return context.WithValue(ctx, keySubagentDeep, depth)
}

func subagentDepth(ctx context.Context) int {
	depth, _ := ctx.Value(keySubagentDeep).(int)
	return depth
}
