package pi

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/PycMono/go-reagent/pi/ai"
	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
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
	governor := newRunGovernor(RunLimits{MaxTotalTokens: 100})
	if err := governor.observe(ModelInvocation{Sequence: 1, Usage: governorUsage(10, 10)}); err != nil {
		t.Fatal(err)
	}
	if governor.totals.Invocations != 1 || governor.totals.TotalTokens != 20 {
		t.Fatalf("totals = %#v", governor.totals)
	}
	if err := governor.observe(ModelInvocation{Sequence: 2, Usage: governorUsage(10, 10)}); err != nil {
		t.Fatal(err)
	}
	if governor.totals.Invocations != 2 || governor.totals.TotalTokens != 40 {
		t.Fatalf("totals = %#v, want accumulated once per call", governor.totals)
	}
}

func TestGovernorRejectsTokenOverflow(t *testing.T) {
	governor := newRunGovernor(RunLimits{})
	usage := ai.Usage{
		PlatformID: "test", Model: "model",
		InputTokens: math.MaxInt64 - 1,
	}
	if err := governor.observe(ModelInvocation{Sequence: 1, Usage: usage}); err != nil {
		t.Fatal(err)
	}
	err := governor.observe(ModelInvocation{Sequence: 2, Usage: usage})
	if pierrors.ErrorCodeOf(err) != pierrors.ErrorCodeInternal {
		t.Fatalf("observe() error = %v, want internal overflow error", err)
	}
}

func TestGovernorCompensatedCostSummation(t *testing.T) {
	governor := newRunGovernor(RunLimits{})
	usage := ai.Usage{
		PlatformID: "test", Model: "model",
		InputTokens:                   1000,
		InputPriceUSDPerMillionTokens: 1,
		CostUSD:                       0.001,
	}
	for range 1000 {
		if err := governor.observe(ModelInvocation{Usage: usage}); err != nil {
			t.Fatal(err)
		}
	}
	if math.Abs(governor.totals.CostUSD-1.0) > 1e-9 {
		t.Fatalf("CostUSD = %v, want 1.0", governor.totals.CostUSD)
	}
}

func TestTerminationFromErrorPriority(t *testing.T) {
	limitErr := pierrors.Wrap(pierrors.ErrorCodeRunLimitExceeded, "run budget", &runLimitError{kind: RunLimitCostUSD})
	joined := errors.Join(context.Canceled, limitErr)
	if got := terminationFromError(joined, RunTotals{}); got.Reason != RunTerminationCanceled {
		t.Fatalf("reason = %q, want canceled priority", got.Reason)
	}
	if got := terminationFromError(limitErr, RunTotals{}); got.Reason != RunTerminationMaxCost ||
		got.Limit != RunLimitCostUSD {
		t.Fatalf("termination = %#v, want max_cost", got)
	}
	if got := terminationFromError(errors.New("boom"), RunTotals{}); got.Reason != RunTerminationError {
		t.Fatalf("reason = %q, want error", got.Reason)
	}
	if got := terminationFromError(nil, RunTotals{}); got.Reason != RunTerminationCompleted {
		t.Fatalf("reason = %q, want completed", got.Reason)
	}
}
