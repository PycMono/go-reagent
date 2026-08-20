package harness

import (
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/pi/ai"
)

func TestTokenMeterProjectionExcludesInternalFields(t *testing.T) {
	base := ai.Message{
		Role:    ai.RoleAssistant,
		Content: []ai.ContentBlock{ai.TextBlock("计划")},
	}
	withInternal := base
	withInternal.FinishReason = ai.FinishReasonStop
	withInternal.Usage = &ai.Usage{InputTokens: 1000, OutputTokens: 2000, CostUSD: 1.5}

	got := TokenMeter{}.Estimate(RequestFootprint{Messages: []ai.Message{withInternal}})
	want := TokenMeter{}.Estimate(RequestFootprint{Messages: []ai.Message{base}})
	if got != want {
		t.Fatalf("Estimate with internal fields = %d, want %d (Usage/FinishReason must not be metered)", got, want)
	}
}

func TestTokenMeterToolProjectionExcludesInternalFields(t *testing.T) {
	base := ai.ToolDefinition{
		Name:        "read",
		Description: "读取文件",
		InputSchema: map[string]any{"type": "object"},
	}
	withInternal := base
	withInternal.Label = "Read File"
	withInternal.ParallelSafe = true

	got := TokenMeter{}.Estimate(RequestFootprint{Tools: ai.ToolDefinitions{withInternal}})
	want := TokenMeter{}.Estimate(RequestFootprint{Tools: ai.ToolDefinitions{base}})
	if got != want {
		t.Fatalf("Estimate with internal tool fields = %d, want %d (Label/ParallelSafe must not be metered)", got, want)
	}
}

func TestTokenMeterCoversToolCallsAndResults(t *testing.T) {
	meter := TokenMeter{}
	bare := meter.Estimate(RequestFootprint{Messages: []ai.Message{
		{Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("ok")}},
	}})
	withCalls := meter.Estimate(RequestFootprint{Messages: []ai.Message{
		{
			Role:    ai.RoleAssistant,
			Content: []ai.ContentBlock{ai.TextBlock("ok")},
			ToolCalls: []ai.ToolCall{
				{ID: "c1", Name: "read", Arguments: []byte(`{"path":"a.txt"}`)},
			},
		},
	}})
	if withCalls <= bare {
		t.Fatalf("tool calls must add to the estimate: bare %d, with calls %d", bare, withCalls)
	}

	withResult := meter.Estimate(RequestFootprint{Messages: []ai.Message{
		{
			Role:       ai.RoleTool,
			ToolCallID: "c1",
			ToolName:   "read",
			IsError:    true,
			Content:    []ai.ContentBlock{ai.TextBlock(strings.Repeat("x", 1024))},
		},
	}})
	if withResult <= 0 {
		t.Fatal("tool result must be metered")
	}
}

func TestTokenMeterToolsDifferenceBetweenPhases(t *testing.T) {
	meter := TokenMeter{}
	messages := []ai.Message{
		{Role: ai.RoleUser, Content: []ai.ContentBlock{ai.TextBlock("你好")}},
	}
	thinking := meter.Estimate(RequestFootprint{Messages: messages})
	action := meter.Estimate(RequestFootprint{
		Messages: messages,
		Tools: ai.ToolDefinitions{
			{Name: "read", Description: strings.Repeat("d", 256), InputSchema: map[string]any{"type": "object"}},
		},
	})
	if action <= thinking {
		t.Fatalf("action footprint (%d) must exceed thinking footprint (%d)", action, thinking)
	}
}

func TestTokenMeterEmptyFootprint(t *testing.T) {
	if got := (TokenMeter{}).Estimate(RequestFootprint{}); got != 0 {
		t.Fatalf("Estimate of empty footprint = %d, want 0", got)
	}
}

func TestCompactionInternalDefaultsRelationships(t *testing.T) {
	if !(0 < DefaultPruneRatio && DefaultPruneRatio < DefaultThresholdRatio && DefaultThresholdRatio < 1) {
		t.Fatalf("ratios must satisfy 0 < prune < threshold < 1: %v, %v", DefaultPruneRatio, DefaultThresholdRatio)
	}
	if DefaultReserveOutputTokens < 0 || DefaultSafetyMarginTokens < 0 {
		t.Fatal("reserve and margin must be non-negative")
	}
	if DefaultRetainRecentUnits < 0 || DefaultProtectRecentGroups < 0 {
		t.Fatal("retention defaults must be non-negative")
	}
	if DefaultSummaryInputMaxBytes <= 0 || DefaultMaxToolResultBytes <= 0 {
		t.Fatal("byte budgets must be positive")
	}
}
