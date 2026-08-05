package mysql

import (
	"math"
	"testing"

	"github.com/PycMono/go-reagent/agent"
	"github.com/PycMono/go-reagent/ai"
)

func TestEncodeInvocationProducesFixedScaleLedgerRow(t *testing.T) {
	runID := "run-1"
	row, err := encodeInvocation(validInvocation(), 7, 3, &runID)
	if err != nil {
		t.Fatal(err)
	}
	if row.ConversationPK != 7 || row.TurnVersion != 3 || row.Sequence != 2 ||
		row.RunID == nil || *row.RunID != "run-1" || row.Phase != "action" ||
		row.PlatformID != "zhipu" || row.Model != "glm-4.5-air" ||
		row.InputTokens != 120 || row.OutputTokens != 30 || row.LatencyMS != 245 ||
		row.InputPriceUSDPerMillionTokens != "0.150000000000" ||
		row.OutputPriceUSDPerMillionTokens != "0.600000000000" ||
		row.CostUSD != "0.000036000000" {
		t.Fatalf("row = %#v", row)
	}
	runID = "mutated"
	if row.RunID == nil || *row.RunID != "run-1" {
		t.Fatalf("RunID aliases caller: %#v", row.RunID)
	}
}

func TestEncodeInvocationRejectsInvalidMetrics(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*agent.ModelInvocation)
	}{
		{name: "zero sequence", mutate: func(v *agent.ModelInvocation) { v.Sequence = 0 }},
		{name: "unknown phase", mutate: func(v *agent.ModelInvocation) { v.Phase = "other" }},
		{name: "blank platform", mutate: func(v *agent.ModelInvocation) { v.Usage.PlatformID = " " }},
		{name: "blank model", mutate: func(v *agent.ModelInvocation) { v.Usage.Model = " " }},
		{name: "negative input tokens", mutate: func(v *agent.ModelInvocation) { v.Usage.InputTokens = -1 }},
		{name: "negative output tokens", mutate: func(v *agent.ModelInvocation) { v.Usage.OutputTokens = -1 }},
		{name: "negative latency", mutate: func(v *agent.ModelInvocation) { v.Usage.LatencyMS = -1 }},
		{name: "negative input price", mutate: func(v *agent.ModelInvocation) { v.Usage.InputPriceUSDPerMillionTokens = -1 }},
		{name: "NaN output price", mutate: func(v *agent.ModelInvocation) { v.Usage.OutputPriceUSDPerMillionTokens = math.NaN() }},
		{name: "infinite cost", mutate: func(v *agent.ModelInvocation) { v.Usage.CostUSD = math.Inf(1) }},
		{name: "negative cost", mutate: func(v *agent.ModelInvocation) { v.Usage.CostUSD = -1 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			invocation := validInvocation()
			tt.mutate(&invocation)
			if _, err := encodeInvocation(invocation, 7, 3, nil); err == nil {
				t.Fatal("encodeInvocation() error = nil")
			}
		})
	}
}

func validInvocation() agent.ModelInvocation {
	return agent.ModelInvocation{
		Sequence: 2,
		Phase:    agent.ModelInvocationPhaseAction,
		Usage: ai.Usage{
			InputTokens:                    120,
			OutputTokens:                   30,
			InputPriceUSDPerMillionTokens:  0.15,
			OutputPriceUSDPerMillionTokens: 0.60,
			CostUSD:                        0.000036,
			LatencyMS:                      245,
			PlatformID:                     "zhipu",
			Model:                          "glm-4.5-air",
		},
	}
}
