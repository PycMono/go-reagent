package observability

import (
	"testing"
)

// TestEnumValuesMatchDesign 固定设计第 4、8 章的枚举拼写，防止实现期漂移。
func TestEnumValuesMatchDesign(t *testing.T) {
	cases := map[string][]string{
		"generation_phase":   {string(GenerationPhaseThinking), string(GenerationPhaseAction), string(GenerationPhaseCompaction), string(GenerationPhaseUnknown)},
		"generation_outcome": {string(GenerationOutcomeSucceeded), string(GenerationOutcomeFailed), string(GenerationOutcomeCanceled), string(GenerationOutcomeDeadlineExceeded)},
		"compaction_reason":  {string(CompactionReasonOverflow), string(CompactionReasonThreshold)},
		"execution_mode":     {string(ExecutionModeSerial), string(ExecutionModeParallel), string(ExecutionModeMixed)},
		"transport":          {string(TransportHTTPSSE)},
	}
	want := map[string][]string{
		"generation_phase":   {"thinking", "action", "compaction", "unknown"},
		"generation_outcome": {"succeeded", "failed", "canceled", "deadline_exceeded"},
		"compaction_reason":  {"overflow", "threshold"},
		"execution_mode":     {"serial", "parallel", "mixed"},
		"transport":          {"http_sse"},
	}
	for name, got := range cases {
		expected := want[name]
		if len(got) != len(expected) {
			t.Fatalf("%s: 值数量 %d != %d", name, len(got), len(expected))
		}
		for index := range expected {
			if got[index] != expected[index] {
				t.Fatalf("%s[%d] = %q，期望 %q", name, index, got[index], expected[index])
			}
		}
	}
}
