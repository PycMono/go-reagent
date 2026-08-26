package loopdetect

import (
	"slices"

	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/toolexec"
)

// 第一版固定阈值：通过包内测试演进；对外开放需要先积累误报、漏报和平均
// 止损数据。
const (
	// historySize 是普通行为历史的窗口上限，Detector 内存与 Run 长度无关。
	historySize = 16
	// warningThreshold 是同一 Call signature 触发提醒的 projected count。
	warningThreshold = 3
	// criticalThreshold 是同一 Call signature 构成无进展证据的已确认相同
	// Outcome 数；阈值定义为“已确认结果数”，不是模型请求序号。
	criticalThreshold = 5
)

// record 是一条工具调用行为记录。内部历史按模型返回的 Tool Calls 原始顺序
// 记录，不按并发工具实际完成顺序记录，保证 serial/parallel/mixed Scheduler
// 模式下相同输入得到相同判定。
type record struct {
	toolName         string
	callSignature    signature
	outcomeSignature signature
	outcomeRecorded  bool
	// loopVeto 表示该记录来自被 recover 阻止的批次：不伪装成真实工具
	// 结果，不生成普通 Outcome signature，也不会被解释为“取得进展”。
	loopVeto bool
}

// stableState 是一个 Call signature 的最近稳定结果与连续相同次数。
type stableState struct {
	outcome signature
	count   int
}

// Detector 是请求级行为检测器。不是并发安全的：由 Loop 单线程控制流按
// Admit → 工具执行 → Record 的顺序驱动。
type Detector struct {
	disabled bool
	excluded map[string]struct{}

	history []record
	stable  map[signature]stableState
	// warningsSent 是已发送提醒的 Call signature 集合；结果变化或签名离开
	// 窗口后允许未来重新提醒。
	warningsSent map[signature]struct{}
	// criticalInterventions 是 Run 级 critical 次数，不随窗口淘汰重置：
	// 第一次 critical 给一次恢复机会，第二次终止。
	criticalInterventions int
	// pending 是已准入但尚未记账的批次在 history 中的下标，按调用原始顺序。
	pending []int
	// pendingDropped 是本批准入时因超出窗口而立即被淘汰的前缀长度；
	// 这些记录在窗口外，Record 时跳过回填。
	pendingDropped int
}

func newDetector(config Config) *Detector {
	detector := &Detector{disabled: config.Disabled}
	if len(config.ExcludedTools) > 0 {
		detector.excluded = make(map[string]struct{}, len(config.ExcludedTools))
		for _, name := range config.ExcludedTools {
			detector.excluded[name] = struct{}{}
		}
	}
	if !detector.disabled {
		// Disabled 时不分配增长型状态。
		detector.stable = make(map[signature]stableState)
		detector.warningsSent = make(map[signature]struct{})
	}
	return detector
}

func (d *Detector) isExcluded(toolName string) bool {
	_, ok := d.excluded[toolName]
	return ok
}

// insert 把一条记录写入窗口。顺序固定为先淘汰清理、再插入：先丢弃最旧
// 记录并删除不再被引用的 signature 状态，最后才插入；反序会让新记录立即
// 重新引用同一 signature，导致稳定计数跨窗口泄漏累计。
func (d *Detector) insert(rec record) {
	for len(d.history) >= historySize {
		d.history = d.history[1:]
	}
	d.cleanUnreferenced()
	d.history = append(d.history, rec)
}

// cleanUnreferenced 删除不再被窗口中任何记录引用的 signature 的 stable
// 计数与 warning 抑制状态。Run 级 criticalInterventions 不在此清理。
func (d *Detector) cleanUnreferenced() {
	referenced := make(map[signature]struct{}, len(d.history))
	for _, rec := range d.history {
		referenced[rec.callSignature] = struct{}{}
	}
	for sig := range d.stable {
		if _, ok := referenced[sig]; !ok {
			delete(d.stable, sig)
		}
	}
	for sig := range d.warningsSent {
		if _, ok := referenced[sig]; !ok {
			delete(d.warningsSent, sig)
		}
	}
}

// intervention 是一次候选干预；order 是投影顺序，用于同级取第一个。
type intervention struct {
	pattern  Pattern
	count    int
	toolName string
	sig      signature
	order    int
	critical bool
}

// sortedToolNames 去重并按字典序排序，保证 Intervention 的日志与测试稳定。
func sortedToolNames(names ...string) []string {
	result := slices.Clone(names)
	slices.Sort(result)
	return slices.Compact(result)
}

// admit 实现整批原子准入：在窗口副本上按原始顺序逐个投影（批内相同
// signature 互相计入），取最严重干预；只有 allow/warn 才把投影原子提交到
// 真实历史；recover 把本批记录为 veto 证据；terminate 不修改历史。
func (d *Detector) admit(calls ai.ToolCalls) Admission {
	if d.disabled {
		return Admission{Decision: DecisionAllow}
	}

	projected := slices.Clone(d.history)
	var best *intervention
	pending := make([]record, 0, len(calls))

	consider := func(cand intervention) {
		if best == nil || (cand.critical && !best.critical) {
			candidate := cand
			best = &candidate
		}
	}

	for index, call := range calls {
		if d.isExcluded(call.Name) {
			continue
		}
		sig := callSignature(call)

		// 规则 2（stable-outcome critical）：阈值是已确认结果数；当前调用
		// 尚未执行，projected calls 不提供 Outcome 证据。
		if state, ok := d.stable[sig]; ok && state.count >= criticalThreshold {
			consider(intervention{
				pattern:  PatternStableOutcome,
				count:    state.count,
				toolName: call.Name,
				sig:      sig,
				order:    index,
				critical: true,
			})
		}

		// 规则 1（repeated-call warning）：projected count 包含窗口记录与
		// 本批前序相同 signature。
		count := 1
		for _, rec := range projected {
			if rec.callSignature == sig {
				count++
			}
		}
		if count >= warningThreshold {
			if _, sent := d.warningsSent[sig]; !sent {
				consider(intervention{
					pattern:  PatternRepeatedCall,
					count:    count,
					toolName: call.Name,
					sig:      sig,
					order:    index,
				})
			}
		}

		rec := record{toolName: call.Name, callSignature: sig}
		pending = append(pending, rec)
		if len(projected) >= historySize {
			projected = projected[1:]
		}
		projected = append(projected, rec)
	}

	switch {
	case best == nil:
		d.commitPending(pending)
		return Admission{Decision: DecisionAllow}
	case !best.critical:
		// 提醒在准入时即刻标记为已发送，相同 warning key 不重复提醒。
		d.warningsSent[best.sig] = struct{}{}
		d.commitPending(pending)
		return Admission{Decision: DecisionWarn, Intervention: &Intervention{
			Level:     LevelWarning,
			Pattern:   best.pattern,
			Count:     best.count,
			ToolNames: sortedToolNames(best.toolName),
		}}
	default:
		// “第二次”是 Run 级计数，不要求 Pattern、工具名或 Call signature
		// 与第一次相同。
		d.criticalInterventions++
		evidence := &Intervention{
			Pattern:   best.pattern,
			Count:     best.count,
			ToolNames: sortedToolNames(best.toolName),
		}
		if d.criticalInterventions == 1 {
			evidence.Level = LevelRecovery
			// recover：本批以 veto 证据写入历史，Loop 不得再调用 Record；
			// 任何未记账的旧 pending 随本次准入作废。
			d.pending = d.pending[:0]
			d.pendingDropped = 0
			for _, rec := range pending {
				rec.loopVeto = true
				d.insert(rec)
			}
			return Admission{Decision: DecisionRecover, Intervention: evidence}
		}
		evidence.Level = LevelTermination
		return Admission{Decision: DecisionTerminate, Intervention: evidence}
	}
}

// commitPending 把 allow/warn 批次的投影原子提交到真实历史，并记录 pending
// 下标供 RecordToolBatchOutcome 按原始顺序回填。批次超过窗口时，最旧的前缀
// 在准入时即被淘汰，记下 dropped 长度，Record 跳过它们的回填。
func (d *Detector) commitPending(pending []record) {
	d.pending = d.pending[:0]
	d.pendingDropped = 0
	for _, rec := range pending {
		d.insert(rec)
	}
	surviving := min(len(pending), len(d.history))
	d.pendingDropped = len(pending) - surviving
	start := len(d.history) - surviving
	for i := range surviving {
		d.pending = append(d.pending, start+i)
	}
}

// record 实现事实记账：对齐校验失败时不修改任何状态（不变量由 Loop 保证，
// 此处仅为防御性兜底）。结果变化代表进展：重置 stable streak 为 1 并清除
// 该 signature 的 warning 抑制状态；其他 signature 的状态不受影响。
func (d *Detector) record(calls ai.ToolCalls, results []toolexec.Result) {
	if d.disabled {
		return
	}
	if len(calls) != len(results) {
		return
	}
	for index := range calls {
		if calls[index].ID != results[index].ToolCallID || calls[index].Name != results[index].ToolName {
			return
		}
	}

	pendingIndex := 0
	skipped := 0
	for index, call := range calls {
		if d.isExcluded(call.Name) {
			continue
		}
		if skipped < d.pendingDropped {
			// 准入时即被淘汰出窗口的前缀记录，无法也无须回填。
			skipped++
			continue
		}
		if pendingIndex >= len(d.pending) {
			// 没有对应的准入记录（例如 recover 后被错误调用）；防御性忽略。
			return
		}
		rec := &d.history[d.pending[pendingIndex]]
		pendingIndex++
		outcome := outcomeSignature(rec.callSignature, results[index])
		rec.outcomeSignature = outcome
		rec.outcomeRecorded = true

		state, ok := d.stable[rec.callSignature]
		if ok && state.outcome == outcome {
			state.count++
			d.stable[rec.callSignature] = state
			continue
		}
		if ok {
			// 结果变化：既有 warning 抑制解除，未来允许重新提醒。
			delete(d.warningsSent, rec.callSignature)
		}
		d.stable[rec.callSignature] = stableState{outcome: outcome, count: 1}
	}
	d.pending = d.pending[:0]
	d.pendingDropped = 0
}
