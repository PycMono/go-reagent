package loopdetect

import (
	"fmt"
	"testing"

	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/toolexec"
)

// runTurn 模拟一次完整的准入-执行-记账生命周期。
func runTurn(d *Detector, calls ai.ToolCalls, results []toolexec.Result) Admission {
	admission := d.AdmitToolBatch(calls)
	if admission.Decision == DecisionAllow || admission.Decision == DecisionWarn {
		d.RecordToolBatchOutcome(calls, results)
	}
	return admission
}

func sameResults(calls ai.ToolCalls, text string) []toolexec.Result {
	results := make([]toolexec.Result, len(calls))
	for i, call := range calls {
		results[i] = makeResult(call, text)
	}
	return results
}

// Config 2：SDK 直接调用 New 时对 ExcludedTools 做 trim、去空、去重，且不
// 修改调用方传入的 Config。
func TestNewDefensiveNormalization(t *testing.T) {
	config := Config{ExcludedTools: []string{" poll ", "", "poll", "x"}}
	detector := New(config)
	if len(config.ExcludedTools) != 4 {
		t.Fatal("caller config must not be mutated")
	}
	if len(detector.excluded) != 2 {
		t.Fatalf("expected 2 normalized exclusions, got %d", len(detector.excluded))
	}
	if !detector.isExcluded("poll") || !detector.isExcluded("x") {
		t.Fatal("trimmed names must be excluded")
	}
	if detector.isExcluded(" poll ") {
		t.Fatal("untrimmed name must not be excluded")
	}
}

// Config 3 / Admission 1：零值默认启用；Disabled 时恒 Allow 且所有方法为
// 无增长型状态的 no-op。
func TestDisabledNoop(t *testing.T) {
	d := New(Config{})
	if d.stable == nil || d.warningsSent == nil {
		t.Fatal("zero value config must be enabled")
	}

	disabled := New(Config{Disabled: true})
	if disabled.stable != nil || disabled.warningsSent != nil || disabled.history != nil {
		t.Fatal("disabled detector must not allocate growing state")
	}
	calls := ai.ToolCalls{makeCall("1", "t", `{"a":1}`)}
	for range 10 {
		if got := disabled.AdmitToolBatch(calls).Decision; got != DecisionAllow {
			t.Fatalf("disabled must always allow, got %s", got)
		}
	}
	disabled.RecordToolBatchOutcome(calls, sameResults(calls, "x"))
	disabled.RecordToolBatchOutcome(calls, nil) // 对齐失败同样无效果
	if len(disabled.history) != 0 {
		t.Fatal("disabled detector must stay stateless")
	}
}

// Admission 2/3：第 3 次相同调用只 Warn 不阻止；相同 warning key 只提醒一次
// （结果保持稳定时抑制不会被解除）。
func TestRepeatedCallWarningOnce(t *testing.T) {
	d := New(Config{})
	call := makeCall("1", "t", `{"a":1}`)
	for i, want := range []Decision{DecisionAllow, DecisionAllow, DecisionWarn, DecisionAllow, DecisionAllow} {
		call.ID = fmt.Sprint(i)
		admission := runTurn(d, ai.ToolCalls{call}, sameResults(ai.ToolCalls{call}, "same"))
		if admission.Decision != want {
			t.Fatalf("turn %d: want %s, got %s", i, want, admission.Decision)
		}
		if admission.Decision == DecisionWarn {
			if admission.Intervention == nil ||
				admission.Intervention.Level != LevelWarning ||
				admission.Intervention.Pattern != PatternRepeatedCall ||
				admission.Intervention.Count != 3 {
				t.Fatalf("bad warn intervention: %+v", admission.Intervention)
			}
		}
		if admission.Decision == DecisionAllow && admission.Intervention != nil {
			t.Fatal("allow must carry nil intervention")
		}
	}
}

// Admission 3：结果变化后未来允许重新提醒。
func TestWarningRearmAfterOutcomeChange(t *testing.T) {
	d := New(Config{})
	call := makeCall("1", "t", `{"a":1}`)
	// 3 次相同调用（结果相同）触发 warn。
	for i := range 3 {
		call.ID = fmt.Sprint(i)
		runTurn(d, ai.ToolCalls{call}, sameResults(ai.ToolCalls{call}, "same"))
	}
	// 第 4 次：结果变化 → 清除抑制。
	call.ID = "4"
	if got := runTurn(d, ai.ToolCalls{call}, sameResults(ai.ToolCalls{call}, "changed")).Decision; got != DecisionAllow {
		t.Fatalf("suppressed warn expected allow, got %s", got)
	}
	// 第 5 次：warning key 已解除抑制，允许重新提醒。
	call.ID = "5"
	if got := runTurn(d, ai.ToolCalls{call}, sameResults(ai.ToolCalls{call}, "changed")).Decision; got != DecisionWarn {
		t.Fatalf("expected re-armed warn, got %s", got)
	}
}

// Admission 4：相同调用但结果持续变化，不触发 stable critical。
func TestChangingOutcomesNeverCritical(t *testing.T) {
	d := New(Config{})
	call := makeCall("1", "poll", `{"id":1}`)
	for i := range 12 {
		call.ID = fmt.Sprint(i)
		admission := runTurn(d, ai.ToolCalls{call}, sameResults(ai.ToolCalls{call}, fmt.Sprintf("state-%d", i)))
		if admission.Decision == DecisionRecover || admission.Decision == DecisionTerminate {
			t.Fatalf("turn %d: changing outcomes must not be critical", i)
		}
	}
}

// Admission 5：确认 5 个相同 outcomes 后再次调用，第一次返回 Recover。
func TestStableOutcomeRecover(t *testing.T) {
	d := New(Config{})
	call := makeCall("1", "poll", `{"id":1}`)
	for i := range 5 {
		call.ID = fmt.Sprint(i)
		runTurn(d, ai.ToolCalls{call}, sameResults(ai.ToolCalls{call}, "pending"))
	}
	call.ID = "6"
	admission := d.AdmitToolBatch(ai.ToolCalls{call})
	if admission.Decision != DecisionRecover {
		t.Fatalf("expected recover, got %s", admission.Decision)
	}
	if admission.Intervention.Level != LevelRecovery ||
		admission.Intervention.Pattern != PatternStableOutcome ||
		admission.Intervention.Count != 5 {
		t.Fatalf("bad recover intervention: %+v", admission.Intervention)
	}
}

// Admission 6/7：Recover 批次由 Admit 记录 veto（不调用 Record）；之后再次
// 任意 critical 返回 Terminate。
func TestSecondCriticalTerminates(t *testing.T) {
	d := New(Config{})
	call := makeCall("1", "poll", `{"id":1}`)
	for i := range 5 {
		call.ID = fmt.Sprint(i)
		runTurn(d, ai.ToolCalls{call}, sameResults(ai.ToolCalls{call}, "pending"))
	}
	call.ID = "6"
	if got := d.AdmitToolBatch(ai.ToolCalls{call}).Decision; got != DecisionRecover {
		t.Fatalf("expected recover, got %s", got)
	}
	// veto 不产生 outcome；模型恢复后再次请求同一签名 → 第二次 critical。
	call.ID = "7"
	admission := d.AdmitToolBatch(ai.ToolCalls{call})
	if admission.Decision != DecisionTerminate {
		t.Fatalf("expected terminate, got %s", admission.Decision)
	}
	if admission.Intervention.Level != LevelTermination {
		t.Fatalf("bad terminate intervention: %+v", admission.Intervention)
	}
}

// Admission 8：A/B 交替循环无专属规则；A 侧确认 5 个相同结果后由
// stable-outcome critical 拦截。
func TestAlternatingLoopCaughtByStableOutcome(t *testing.T) {
	d := New(Config{})
	a := makeCall("a", "tool_a", `{"x":1}`)
	b := makeCall("b", "tool_b", `{"y":2}`)
	for i := range 5 {
		a.ID = fmt.Sprintf("a-%d", i)
		b.ID = fmt.Sprintf("b-%d", i)
		runTurn(d, ai.ToolCalls{a, b}, sameResults(ai.ToolCalls{a, b}, "stable"))
	}
	a.ID = "a-5"
	if got := d.AdmitToolBatch(ai.ToolCalls{a}).Decision; got != DecisionRecover {
		t.Fatalf("alternating loop must escalate via stable-outcome, got %s", got)
	}
}

// Admission 9：多签名漂移循环（每个签名窗口内最多出现 2 次）不触发任何
// 行为级 critical，由预算兜底——固定这一有意漏报边界。
func TestDriftLoopEscapes(t *testing.T) {
	d := New(Config{})
	for cycle := range 3 {
		for i := range 10 {
			call := makeCall(fmt.Sprintf("%d-%d", cycle, i), fmt.Sprintf("tool_%d", i), `{"q":1}`)
			admission := runTurn(d, ai.ToolCalls{call}, sameResults(ai.ToolCalls{call}, "same"))
			if admission.Decision != DecisionAllow {
				t.Fatalf("cycle %d tool %d: drift loop must not trigger interventions, got %s",
					cycle, i, admission.Decision)
			}
		}
	}
}

// Admission 10：第 17 条插入后最旧记录淘汰；signature 完全离窗时清除
// stable 与 warning 状态，但不重置 Run 级 critical 次数。
func TestEvictionClearsSignatureStateButNotCriticalCount(t *testing.T) {
	d := New(Config{})
	loop := makeCall("1", "loop_tool", `{"a":1}`)

	// 先制造一次 critical（recover）。
	for i := range 5 {
		loop.ID = fmt.Sprintf("loop-%d", i)
		runTurn(d, ai.ToolCalls{loop}, sameResults(ai.ToolCalls{loop}, "same"))
	}
	loop.ID = "loop-6"
	if got := d.AdmitToolBatch(ai.ToolCalls{loop}).Decision; got != DecisionRecover {
		t.Fatalf("expected recover, got %s", got)
	}

	// 用 17 个不同调用把 loop_tool 的签名完全挤出窗口。
	for i := range 17 {
		other := makeCall(fmt.Sprintf("o-%d", i), fmt.Sprintf("other_%d", i), `{}`)
		runTurn(d, ai.ToolCalls{other}, sameResults(ai.ToolCalls{other}, "x"))
	}
	loopSig := callSignature(loop)
	if _, ok := d.stable[loopSig]; ok {
		t.Fatal("evicted signature stable state must be cleaned")
	}
	if _, ok := d.warningsSent[loopSig]; ok {
		t.Fatal("evicted signature warning state must be cleaned")
	}

	// loop_tool 重新出现：stable count 从零开始，不立即 critical。
	loop.ID = "loop-7"
	if got := runTurn(d, ai.ToolCalls{loop}, sameResults(ai.ToolCalls{loop}, "same")).Decision; got != DecisionAllow {
		t.Fatalf("evicted signature must restart counting, got %s", got)
	}
	// 但 Run 级 critical 次数保留：重新累计 5 个相同结果后应直接终止。
	for i := range 4 {
		loop.ID = fmt.Sprintf("loop-%d", 8+i)
		runTurn(d, ai.ToolCalls{loop}, sameResults(ai.ToolCalls{loop}, "same"))
	}
	loop.ID = "loop-12"
	if got := d.AdmitToolBatch(ai.ToolCalls{loop}).Decision; got != DecisionTerminate {
		t.Fatalf("Run-level critical count must survive eviction, got %s", got)
	}
}

// Admission 11：间隔恰在窗口边界的低频循环（A + 15 个不同调用 + A）不累计
// stable count——插入新记录前必须先完成淘汰与清理。
func TestBoundarySlowLoopDoesNotAccumulate(t *testing.T) {
	d := New(Config{})
	a := makeCall("a", "a_tool", `{}`)
	for cycle := range 6 {
		a.ID = fmt.Sprintf("a-%d", cycle)
		if got := runTurn(d, ai.ToolCalls{a}, sameResults(ai.ToolCalls{a}, "same")).Decision; got != DecisionAllow {
			t.Fatalf("cycle %d: boundary slow loop must not accumulate, got %s", cycle, got)
		}
		for i := range 15 {
			other := makeCall(fmt.Sprintf("%d-%d", cycle, i), fmt.Sprintf("o%d_%d", cycle, i), `{}`)
			runTurn(d, ai.ToolCalls{other}, sameResults(ai.ToolCalls{other}, "x"))
		}
	}
}

// Admission 12：excluded tool 不计数；混合批次中的未排除工具仍可阻止整批。
func TestExcludedToolsAndMixedBatch(t *testing.T) {
	d := New(Config{ExcludedTools: []string{"poll"}})
	excluded := makeCall("1", "poll", `{"id":1}`)
	for i := range 8 {
		excluded.ID = fmt.Sprint(i)
		if got := runTurn(d, ai.ToolCalls{excluded}, sameResults(ai.ToolCalls{excluded}, "same")).Decision; got != DecisionAllow {
			t.Fatalf("excluded tool must never trigger interventions, got %s", got)
		}
	}
	if len(d.history) != 0 {
		t.Fatal("excluded calls must not enter history")
	}

	// 未排除工具累计 5 个稳定结果后，混合批次整批被 recover。
	normal := makeCall("n", "read", `{"p":1}`)
	for i := range 5 {
		normal.ID = fmt.Sprintf("n-%d", i)
		runTurn(d, ai.ToolCalls{normal}, sameResults(ai.ToolCalls{normal}, "same"))
	}
	normal.ID = "n-5"
	excluded.ID = "e-final"
	admission := d.AdmitToolBatch(ai.ToolCalls{excluded, normal})
	if admission.Decision != DecisionRecover {
		t.Fatalf("mixed batch must be blocked atomically, got %s", admission.Decision)
	}
}

// Admission 13：Tool Names 去重、排序稳定。
func TestInterventionToolNamesSorted(t *testing.T) {
	intervention := &Intervention{ToolNames: []string{"b", "a", "b"}}
	err := NewError(intervention)
	if len(err.ToolNames) != 2 || err.ToolNames[0] != "a" || err.ToolNames[1] != "b" {
		t.Fatalf("tool names must be deduped and sorted, got %v", err.ToolNames)
	}
}

// Admission 14：同一 Action 批次内 3 个相同调用按原始顺序投影，第 3 个触发
// warn；Count 为触发值。
func TestSameBatchProjection(t *testing.T) {
	d := New(Config{})
	calls := ai.ToolCalls{
		makeCall("1", "t", `{"a":1}`),
		makeCall("2", "t", `{"a":1}`),
		makeCall("3", "t", `{"a":1}`),
	}
	admission := d.AdmitToolBatch(calls)
	if admission.Decision != DecisionWarn {
		t.Fatalf("3 identical calls in one batch must warn, got %s", admission.Decision)
	}
	if admission.Intervention.Count != 3 {
		t.Fatalf("count must be the projected trigger value 3, got %d", admission.Intervention.Count)
	}
	// warn 批次完整提交：后续 Record 正常回填 3 条。
	d.RecordToolBatchOutcome(calls, sameResults(calls, "x"))
	if len(d.history) != 3 {
		t.Fatalf("warn batch must be committed, got %d records", len(d.history))
	}
	for _, rec := range d.history {
		if !rec.outcomeRecorded {
			t.Fatal("all records must have outcomes recorded")
		}
	}
}

// 契约：Record 对齐前置条件（长度、ID、工具名）被违反时不修改状态。
func TestRecordAlignmentViolationsAreNoop(t *testing.T) {
	d := New(Config{})
	calls := ai.ToolCalls{makeCall("1", "t", `{"a":1}`)}
	d.AdmitToolBatch(calls)

	d.RecordToolBatchOutcome(calls, nil)
	mismatched := sameResults(calls, "x")
	mismatched[0].ToolCallID = "other"
	d.RecordToolBatchOutcome(calls, mismatched)
	mismatched = sameResults(calls, "x")
	mismatched[0].ToolName = "other"
	d.RecordToolBatchOutcome(calls, mismatched)

	if len(d.stable) != 0 || d.history[0].outcomeRecorded {
		t.Fatal("misaligned record calls must not modify state")
	}
}

// 契约：批次超过窗口时，最旧前缀在准入时淘汰，Record 正常回填存活记录。
func TestOversizedBatchCommit(t *testing.T) {
	d := New(Config{})
	calls := make(ai.ToolCalls, 0, historySize+4)
	for i := range historySize + 4 {
		calls = append(calls, makeCall(fmt.Sprint(i), fmt.Sprintf("t%d", i), `{}`))
	}
	if got := d.AdmitToolBatch(calls).Decision; got != DecisionAllow {
		t.Fatalf("expected allow, got %s", got)
	}
	if len(d.history) != historySize {
		t.Fatalf("history must be capped, got %d", len(d.history))
	}
	d.RecordToolBatchOutcome(calls, sameResults(calls, "x"))
	if len(d.stable) != historySize {
		t.Fatalf("only surviving records get outcomes, got %d", len(d.stable))
	}
}
