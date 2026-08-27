package governor

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/PycMono/go-reagent/pi/ai"
	pierrors "github.com/PycMono/go-reagent/pi/errors"
	"github.com/PycMono/go-reagent/pi/loopdetect"
)

func governorUsage(input, output int64) ai.Usage {
	return ai.Usage{
		PlatformID:                     "test",
		Model:                          "model",
		InputTokens:                    input,
		OutputTokens:                   output,
		InputPriceUSDPerMillionTokens:  1,
		OutputPriceUSDPerMillionTokens: 1,
		CostUSD:                        float64(input+output) / 1_000_000,
	}
}

func TestGovernorCountsEachInvocationOnce(t *testing.T) {
	governor := New(Limits{MaxTotalTokens: 100})
	if err := governor.Observe(Invocation{Sequence: 1, Usage: governorUsage(10, 10)}); err != nil {
		t.Fatal(err)
	}
	if governor.totals.Invocations != 1 || governor.totals.TotalTokens != 20 {
		t.Fatalf("totals = %#v", governor.totals)
	}
	if err := governor.Observe(Invocation{Sequence: 2, Usage: governorUsage(10, 10)}); err != nil {
		t.Fatal(err)
	}
	if governor.totals.Invocations != 2 || governor.totals.TotalTokens != 40 {
		t.Fatalf("totals = %#v, want accumulated once per call", governor.totals)
	}
}

func TestGovernorRejectsTokenOverflow(t *testing.T) {
	// 预算必须足够大：本用例测的是累计溢出，不是预算触顶。
	governor := New(Limits{MaxTotalTokens: math.MaxInt64})
	usage := ai.Usage{
		PlatformID: "test", Model: "model",
		InputTokens: math.MaxInt64 - 1,
	}
	if err := governor.Observe(Invocation{Sequence: 1, Usage: usage}); err != nil {
		t.Fatal(err)
	}
	err := governor.Observe(Invocation{Sequence: 2, Usage: usage})
	if pierrors.ErrorCodeOf(err) != pierrors.ErrorCodeInternal {
		t.Fatalf("observe() error = %v, want internal overflow error", err)
	}
}

func TestGovernorCompensatedCostSummation(t *testing.T) {
	// 预算必须高于累计总额：1000 次 × 0.001 = $1.0，默认 $1 会触顶。
	governor := New(Limits{MaxCostUSD: 2})
	usage := ai.Usage{
		PlatformID: "test", Model: "model",
		InputTokens:                   1000,
		InputPriceUSDPerMillionTokens: 1,
		CostUSD:                       0.001,
	}
	for range 1000 {
		if err := governor.Observe(Invocation{Usage: usage}); err != nil {
			t.Fatal(err)
		}
	}
	if math.Abs(governor.totals.CostUSD-1.0) > 1e-9 {
		t.Fatalf("CostUSD = %v, want 1.0", governor.totals.CostUSD)
	}
}

func TestTerminationFromErrorPriority(t *testing.T) {
	limitErr := pierrors.Wrap(pierrors.ErrorCodeRunLimitExceeded, "run budget", &limitError{kind: LimitCostUSD})
	joined := errors.Join(context.Canceled, limitErr)
	if got := TerminationFromError(joined, Totals{}); got.Reason != TerminationCanceled {
		t.Fatalf("reason = %q, want canceled priority", got.Reason)
	}
	if got := TerminationFromError(limitErr, Totals{}); got.Reason != TerminationMaxCost ||
		got.Limit != LimitCostUSD {
		t.Fatalf("termination = %#v, want max_cost", got)
	}
	if got := TerminationFromError(errors.New("boom"), Totals{}); got.Reason != TerminationError {
		t.Fatalf("reason = %q, want error", got.Reason)
	}
	if got := TerminationFromError(nil, Totals{}); got.Reason != TerminationCompleted {
		t.Fatalf("reason = %q, want completed", got.Reason)
	}
}

func TestTerminationFromErrorLoopDetected(t *testing.T) {
	loopErr := pierrors.Wrap(pierrors.ErrorCodeRunLoopDetected, "tool loop detection",
		&loopdetect.Error{Pattern: loopdetect.PatternStableOutcome, Count: 5, ToolNames: []string{"search"}})
	if got := TerminationFromError(loopErr, Totals{}); got.Reason != TerminationLoopDetected {
		t.Fatalf("reason = %q, want loop_detected", got.Reason)
	}
	// 取消/deadline 的映射优先于 loop error。
	canceled := errors.Join(context.Canceled, loopErr)
	if got := TerminationFromError(canceled, Totals{}); got.Reason != TerminationCanceled {
		t.Fatalf("reason = %q, want canceled priority over loop error", got.Reason)
	}
}

func TestNewAppliesDefaultLimits(t *testing.T) {
	// 整体未配置：回填全部默认预算。
	if got := New(Limits{}).limits; got != DefaultLimits() {
		t.Fatalf("limits = %#v, want %#v", got, DefaultLimits())
	}
	// 逐字段默认：已配置字段保持原值，零值字段回填默认。
	partial := New(Limits{MaxCostUSD: 0.5}).limits
	want := Limits{MaxTurns: 20, MaxCostUSD: 0.5, MaxTotalTokens: 2_000_000}
	if partial != want {
		t.Fatalf("limits = %#v, want %#v", partial, want)
	}
	// 全部显式配置：不触碰。
	full := Limits{MaxTurns: 5, MaxCostUSD: 0.1, MaxTotalTokens: 1000}
	if got := New(full).limits; got != full {
		t.Fatalf("limits = %#v, want %#v", got, full)
	}
}
