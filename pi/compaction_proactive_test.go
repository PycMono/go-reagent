package pi

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/harness"
	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
)

func compactTestMessages(groups int) []ai.Message {
	messages := []ai.Message{
		{Role: ai.RoleSystem, Content: []ai.ContentBlock{ai.TextBlock("system")}},
		{Role: ai.RoleUser, Content: []ai.ContentBlock{ai.TextBlock("目标")}},
	}
	for index := 0; index < groups; index++ {
		callID := fmt.Sprintf("c%d", index)
		messages = append(messages,
			ai.Message{
				Role: ai.RoleAssistant,
				ToolCalls: []ai.ToolCall{
					{ID: callID, Name: "read", Arguments: []byte(`{"path":"a"}`)},
				},
			},
			ai.Message{
				Role:       ai.RoleTool,
				ToolCallID: callID,
				ToolName:   "read",
				Content:    []ai.ContentBlock{ai.TextBlock(strings.Repeat("x", 4096))},
			},
		)
	}
	return messages
}

// fakeCompactionStream 立即返回结果的最小流实现。
type fakeCompactionStream struct {
	message *ai.Message
	err     error
}

func (s *fakeCompactionStream) Next() bool                   { return false }
func (s *fakeCompactionStream) Current() ai.StreamEvent      { return ai.StreamEvent{} }
func (s *fakeCompactionStream) Result() (*ai.Message, error) { return s.message, s.err }
func (s *fakeCompactionStream) Close() error                 { return nil }

type compactionStep struct {
	message *ai.Message
	err     error
}

// fakeCompactionProvider 按脚本依次响应，并记录每次请求快照。
type fakeCompactionProvider struct {
	steps    []compactionStep
	calls    int
	requests [][]ai.Message
}

func (p *fakeCompactionProvider) Stream(_ context.Context, messages []ai.Message, _ []ai.ToolDefinition) ai.Stream {
	p.requests = append(p.requests, messages)
	step := compactionStep{err: errors.New("unexpected provider call")}
	if p.calls < len(p.steps) {
		step = p.steps[p.calls]
	}
	p.calls++
	return &fakeCompactionStream{message: step.message, err: step.err}
}

func contextOverflowErr() error {
	return pierrors.Wrap(pierrors.ErrorCodeAIContextOverflow, "test", errors.New("too long"))
}

func requestsContain(messages []ai.Message, fragment string) bool {
	for _, message := range messages {
		text, _ := ai.TextContent(message.Content)
		if strings.Contains(text, fragment) {
			return true
		}
	}
	return false
}

func compactSummaryResponse(t *testing.T, text string) *ai.Message {
	t.Helper()
	return &ai.Message{
		Role:    ai.RoleAssistant,
		Content: []ai.ContentBlock{ai.TextBlock(text)},
		Usage: &ai.Usage{
			PlatformID: "test", Model: "fake",
			InputTokens: 1, OutputTokens: 1,
			InputPriceUSDPerMillionTokens: 1, OutputPriceUSDPerMillionTokens: 1,
			CostUSD: 2.0 / 1e6,
		},
	}
}

func compactTestLoop(cfg harness.CompactionConfig) *Loop {
	return NewLoopWithCompaction(nil, nil, false, cfg)
}

func mustMaybeCompact(
	t *testing.T,
	loop *Loop,
	messages []ai.Message,
	tools ai.ToolDefinitions,
	rt *compactionRuntime,
) []ai.Message {
	t.Helper()
	got, err := loop.maybeCompact(context.Background(), messages, tools, rt, nil)
	if err != nil {
		t.Fatalf("maybeCompact() error = %v", err)
	}
	return got
}

func TestMaybeCompactSkipsWhenWindowUnknown(t *testing.T) {
	loop := compactTestLoop(harness.CompactionConfig{ContextWindowTokens: 0, EnablePrune: true})
	messages := compactTestMessages(10)

	got := mustMaybeCompact(t, loop, messages, nil, newCompactionRuntime(loop.compaction, 1))
	if got[3].Content[0].Text != messages[3].Content[0].Text {
		t.Fatal("window = 0 must skip the proactive path")
	}
}

func TestMaybeCompactSkipsWhenPruneDisabled(t *testing.T) {
	provider := &fakeCompactionProvider{}
	// 压力位于 PruneRatio 与 ThresholdRatio 之间：L1 被开关禁用，L2 未到水位。
	loop := NewLoopWithCompaction(provider, nil, false,
		harness.CompactionConfig{ContextWindowTokens: 22000, EnablePrune: false})
	messages := compactTestMessages(10)

	got := mustMaybeCompact(t, loop, messages, nil, newCompactionRuntime(loop.compaction, 1))
	if got[3].Content[0].Text != messages[3].Content[0].Text {
		t.Fatal("EnablePrune = false must skip L1")
	}
	if provider.calls != 0 {
		t.Fatalf("summary calls = %d, want 0 below the L2 threshold", provider.calls)
	}
}

func TestMaybeCompactSkipsBelowPruneRatio(t *testing.T) {
	loop := compactTestLoop(harness.CompactionConfig{ContextWindowTokens: 1 << 30, EnablePrune: true})
	messages := compactTestMessages(2)

	got := mustMaybeCompact(t, loop, messages, nil, newCompactionRuntime(loop.compaction, 1))
	for index := range messages {
		if !reflect.DeepEqual(got[index], messages[index]) {
			t.Fatalf("message %d changed below the prune ratio", index)
		}
	}
}

func TestMaybeCompactPrunesOldGroupsAbovePruneRatio(t *testing.T) {
	provider := &fakeCompactionProvider{}
	loop := NewLoopWithCompaction(provider, nil, false,
		harness.CompactionConfig{ContextWindowTokens: 20000, EnablePrune: true})
	messages := compactTestMessages(10)
	original := messages[3].Content[0].Text

	got := mustMaybeCompact(t, loop, messages, nil, newCompactionRuntime(loop.compaction, 1))

	// 10 组中保护最近 3 组，前 7 组应被裁剪；L1 后已低于 L2 水位，不调摘要。
	prunedCount := 0
	for index := 2; index < len(got); index++ {
		if got[index].Role == ai.RoleTool && strings.HasPrefix(got[index].Content[0].Text, "[工具结果已裁剪]") {
			prunedCount++
		}
	}
	if prunedCount != 7 {
		t.Fatalf("pruned tool results = %d, want 7", prunedCount)
	}
	if provider.calls != 0 {
		t.Fatalf("summary calls = %d, want 0 when L1 already drops below the threshold", provider.calls)
	}
	if messages[3].Content[0].Text != original {
		t.Fatal("input history was modified in place")
	}
}

func TestMaybeCompactAccountsForActionTools(t *testing.T) {
	provider := &fakeCompactionProvider{}
	loop := NewLoopWithCompaction(provider, nil, false,
		harness.CompactionConfig{ContextWindowTokens: 20000, EnablePrune: true})
	// 无 tools 时低于水位，携带大 schema 的 tools 时超过水位。
	messages := compactTestMessages(5)
	rt := newCompactionRuntime(loop.compaction, 1)

	got := mustMaybeCompact(t, loop, messages, nil, rt)
	if !reflect.DeepEqual(got[3], messages[3]) {
		t.Fatal("footprint without tools must stay below the prune ratio")
	}

	bigSchema := strings.Repeat("s", 64*1024)
	tools := ai.ToolDefinitions{
		{Name: "read", Description: bigSchema, InputSchema: map[string]any{"type": "object"}},
	}
	got = mustMaybeCompact(t, loop, messages, tools, rt)
	if !strings.HasPrefix(got[3].Content[0].Text, "[工具结果已裁剪]") {
		t.Fatal("tools schema must count toward proactive pressure")
	}
}

func compactTextHistory(pairs int) []ai.Message {
	messages := []ai.Message{
		{Role: ai.RoleSystem, Content: []ai.ContentBlock{ai.TextBlock("system")}},
		{Role: ai.RoleUser, Content: []ai.ContentBlock{ai.TextBlock("本次输入")}},
	}
	for index := 0; index < pairs; index++ {
		messages = append(messages,
			ai.Message{Role: ai.RoleUser, Content: []ai.ContentBlock{ai.TextBlock(fmt.Sprintf("内部过渡 %d", index))}},
			ai.Message{Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock(strings.Repeat("答", 2048))}},
		)
	}
	return messages
}

func TestMaybeCompactProactiveL2ReplacesHistory(t *testing.T) {
	provider := &fakeCompactionProvider{steps: []compactionStep{
		{message: compactSummaryResponse(t, "主动摘要")},
	}}
	loop := NewLoopWithCompaction(provider, nil, false,
		harness.CompactionConfig{ContextWindowTokens: 16000, EnablePrune: true})
	messages := compactTextHistory(8)
	rt := newCompactionRuntime(loop.compaction, 1)

	observed := 0
	got, err := loop.maybeCompact(context.Background(), messages, nil, rt, func(ai.Usage) error {
		observed++
		return nil
	})
	if err != nil {
		t.Fatalf("maybeCompact() error = %v", err)
	}
	if provider.calls != 1 {
		t.Fatalf("summary calls = %d, want 1", provider.calls)
	}
	if observed != 1 {
		t.Fatalf("observed usages = %d, want 1 (usage must be recorded immediately)", observed)
	}
	if len(got) >= len(messages) {
		t.Fatalf("compacted history length %d must be smaller than %d", len(got), len(messages))
	}
	found := false
	for _, message := range got {
		text, _ := ai.TextContent(message.Content)
		if strings.Contains(text, `<compacted-summary untrusted="true">`) && strings.Contains(text, "主动摘要") {
			found = true
		}
	}
	if !found {
		t.Fatal("compacted history must contain the wrapped checkpoint")
	}
	if len(rt.state.CheckpointIndexes) != 1 {
		t.Fatalf("CheckpointIndexes = %v, want exactly one", rt.state.CheckpointIndexes)
	}
	// 本次输入在范围之前，下标不变且内容保留。
	if rt.state.CurrentInputIndex != 1 || got[1].Content[0].Text != "本次输入" {
		t.Fatalf("current input not preserved: index %d, message %q",
			rt.state.CurrentInputIndex, got[1].Content[0].Text)
	}
}

func TestMaybeCompactProactiveL2FailOpenKeepsHistory(t *testing.T) {
	provider := &fakeCompactionProvider{steps: []compactionStep{
		{err: errors.New("summary unavailable")},
	}}
	loop := NewLoopWithCompaction(provider, nil, false,
		harness.CompactionConfig{ContextWindowTokens: 16000, EnablePrune: true})
	messages := compactTextHistory(8)
	rt := newCompactionRuntime(loop.compaction, 1)

	got, err := loop.maybeCompact(context.Background(), messages, nil, rt, nil)
	if err != nil {
		t.Fatalf("maybeCompact() must fail open, got error = %v", err)
	}
	if provider.calls != 1 {
		t.Fatalf("summary calls = %d, want 1", provider.calls)
	}
	if len(got) != len(messages) {
		t.Fatalf("history must be preserved on summary failure: %d != %d", len(got), len(messages))
	}
	if len(rt.state.CheckpointIndexes) != 0 {
		t.Fatalf("CheckpointIndexes = %v, want none after failed summary", rt.state.CheckpointIndexes)
	}
}

func TestMaybeCompactProactiveL2ObserverErrorIsFatal(t *testing.T) {
	provider := &fakeCompactionProvider{steps: []compactionStep{
		{message: compactSummaryResponse(t, "主动摘要")},
	}}
	loop := NewLoopWithCompaction(provider, nil, false,
		harness.CompactionConfig{ContextWindowTokens: 16000, EnablePrune: true})
	messages := compactTextHistory(8)
	rt := newCompactionRuntime(loop.compaction, 1)

	observerErr := errors.New("budget exceeded")
	_, err := loop.maybeCompact(context.Background(), messages, nil, rt, func(ai.Usage) error {
		return observerErr
	})
	if !errors.Is(err, observerErr) {
		t.Fatalf("maybeCompact() error = %v, want observer error to terminate", err)
	}
}

func TestGenerateReactiveL1RespectsEnablePrune(t *testing.T) {
	// EnablePrune=false：overflow 后不做 L1，直接尝试 L2。
	provider := &fakeCompactionProvider{steps: []compactionStep{
		{err: contextOverflowErr()},
		{message: compactSummaryResponse(t, "反应式摘要")},
		{message: compactSummaryResponse(t, "done")},
	}}
	loop := NewLoopWithCompaction(provider, nil, false,
		harness.CompactionConfig{ContextWindowTokens: 0, EnablePrune: false})
	messages := compactTestMessages(6)
	rt := newCompactionRuntime(loop.compaction, 1)

	result, err := loop.generate(context.Background(), messages, nil, nil, nil, rt)
	if err != nil {
		t.Fatalf("generate() error = %v", err)
	}
	if provider.calls != 3 {
		t.Fatalf("provider calls = %d, want 3 (overflow, summary, retry)", provider.calls)
	}
	if requestsContain(provider.requests[2], "[工具结果已裁剪]") {
		t.Fatal("EnablePrune=false must not prune in the reactive path")
	}
	if !requestsContain(provider.requests[2], `<compacted-summary untrusted="true">`) {
		t.Fatal("retried request must contain the checkpoint")
	}
	if len(rt.state.CheckpointIndexes) != 1 {
		t.Fatalf("CheckpointIndexes = %v, want one after committed compaction", rt.state.CheckpointIndexes)
	}
	_ = result
}

func TestGenerateReactiveL1RetriesImmediatelyUnknownWindow(t *testing.T) {
	// 未知窗口：L1 有实际缩减即基于 L1 结果重试，不调摘要。
	provider := &fakeCompactionProvider{steps: []compactionStep{
		{err: contextOverflowErr()},
		{message: compactSummaryResponse(t, "done")},
	}}
	loop := NewLoopWithCompaction(provider, nil, false,
		harness.CompactionConfig{ContextWindowTokens: 0, EnablePrune: true})
	messages := compactTestMessages(6)
	rt := newCompactionRuntime(loop.compaction, 1)

	_, err := loop.generate(context.Background(), messages, nil, nil, nil, rt)
	if err != nil {
		t.Fatalf("generate() error = %v", err)
	}
	if provider.calls != 2 {
		t.Fatalf("provider calls = %d, want 2 (overflow, retry with L1 result)", provider.calls)
	}
	if !requestsContain(provider.requests[1], "[工具结果已裁剪]") {
		t.Fatal("retried request must use the pruned candidate")
	}
}

func TestGenerateReactiveKnownWindowRetriesBelowThreshold(t *testing.T) {
	// 已知窗口：L1 后回到 ThresholdRatio 以下，直接重试，不调摘要。
	provider := &fakeCompactionProvider{steps: []compactionStep{
		{err: contextOverflowErr()},
		{message: compactSummaryResponse(t, "done")},
	}}
	loop := NewLoopWithCompaction(provider, nil, false,
		harness.CompactionConfig{ContextWindowTokens: 20000, EnablePrune: true})
	messages := compactTestMessages(10)
	rt := newCompactionRuntime(loop.compaction, 1)

	_, err := loop.generate(context.Background(), messages, nil, nil, nil, rt)
	if err != nil {
		t.Fatalf("generate() error = %v", err)
	}
	if provider.calls != 2 {
		t.Fatalf("provider calls = %d, want 2 (overflow, retry with L1 result)", provider.calls)
	}
	if !requestsContain(provider.requests[1], "[工具结果已裁剪]") {
		t.Fatal("retried request must use the pruned candidate")
	}
}

func TestGenerateReactiveFallsBackToL1WhenL2HasNoRange(t *testing.T) {
	// 已知窗口：L1 后仍超阈值，但无安全范围（全部 unit 受保护）时，
	// 回退到 L1 结果重试。
	provider := &fakeCompactionProvider{steps: []compactionStep{
		{err: contextOverflowErr()},
		{message: compactSummaryResponse(t, "done")},
	}}
	loop := NewLoopWithCompaction(provider, nil, false,
		harness.CompactionConfig{ContextWindowTokens: 20000, EnablePrune: true})
	messages := compactTestMessages(5)
	rt := newCompactionRuntime(loop.compaction, 1)
	bigTools := []ai.ToolDefinition{
		{Name: "read", Description: strings.Repeat("s", 64*1024), InputSchema: map[string]any{"type": "object"}},
	}

	_, err := loop.generate(context.Background(), messages, bigTools, nil, nil, rt)
	if err != nil {
		t.Fatalf("generate() error = %v", err)
	}
	if provider.calls != 2 {
		t.Fatalf("provider calls = %d, want 2 (overflow, fallback retry)", provider.calls)
	}
	if !requestsContain(provider.requests[1], "[工具结果已裁剪]") {
		t.Fatal("fallback retry must use the L1 result")
	}
}

func TestGenerateReactiveRecordsUsageBeforeContentValidation(t *testing.T) {
	// 摘要 Usage 先记账再验正文：正文无效时 Usage 仍已记录，
	// 随后回退到 L1 结果重试。
	provider := &fakeCompactionProvider{steps: []compactionStep{
		{err: contextOverflowErr()},
		{message: compactSummaryResponse(t, "   ")}, // 正文无效
		{message: compactSummaryResponse(t, "done")},
	}}
	loop := NewLoopWithCompaction(provider, nil, false,
		harness.CompactionConfig{ContextWindowTokens: 18000, EnablePrune: true})

	messages := []ai.Message{
		{Role: ai.RoleSystem, Content: []ai.ContentBlock{ai.TextBlock("system")}},
		{Role: ai.RoleUser, Content: []ai.ContentBlock{ai.TextBlock("本次输入")}},
	}
	// 文本对提供 L2 候选范围，工具组提供 L1 进展。
	for index := 0; index < 6; index++ {
		messages = append(messages,
			ai.Message{Role: ai.RoleUser, Content: []ai.ContentBlock{ai.TextBlock(fmt.Sprintf("过渡 %d", index))}},
			ai.Message{Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock(strings.Repeat("答", 2048))}},
		)
	}
	messages = append(messages, compactTestMessages(5)[2:]...)
	rt := newCompactionRuntime(loop.compaction, 1)

	observed := 0
	_, err := loop.generate(context.Background(), messages, nil, nil, func(ai.Usage) error {
		observed++
		return nil
	}, rt)
	if err != nil {
		t.Fatalf("generate() error = %v", err)
	}
	if provider.calls != 3 {
		t.Fatalf("provider calls = %d, want 3 (overflow, invalid summary, fallback retry)", provider.calls)
	}
	if observed != 1 {
		t.Fatalf("observed usages = %d, want 1 (usage recorded before content validation)", observed)
	}
	if !requestsContain(provider.requests[2], "[工具结果已裁剪]") {
		t.Fatal("fallback retry must use the L1 result")
	}
	if len(rt.state.CheckpointIndexes) != 0 {
		t.Fatalf("CheckpointIndexes = %v, want none after invalid summary", rt.state.CheckpointIndexes)
	}
}

func TestGenerateReactiveObserverErrorIsFatal(t *testing.T) {
	// observer 返回的任何错误必须立即终止，不得回退重试。
	provider := &fakeCompactionProvider{steps: []compactionStep{
		{err: contextOverflowErr()},
		{message: compactSummaryResponse(t, "反应式摘要")},
		{message: compactSummaryResponse(t, "unreachable")},
	}}
	loop := NewLoopWithCompaction(provider, nil, false,
		harness.CompactionConfig{ContextWindowTokens: 0, EnablePrune: false})
	messages := compactTextHistory(8)
	rt := newCompactionRuntime(loop.compaction, 1)

	budgetErr := errors.New("budget exceeded")
	_, err := loop.generate(context.Background(), messages, nil, nil, func(ai.Usage) error {
		return budgetErr
	}, rt)
	if !errors.Is(err, budgetErr) {
		t.Fatalf("generate() error = %v, want observer error", err)
	}
	if provider.calls != 2 {
		t.Fatalf("provider calls = %d, want 2 (no retry after observer error)", provider.calls)
	}
}

func TestMaybeCompactDoesNotCommitStateWithoutTokenProgress(t *testing.T) {
	// checkpoint 仅按字节略小、token 估算持平时：消息与状态都不得提交。
	messages := compactTextHistory(8)
	rt := newCompactionRuntime(harness.CompactionConfig{}, 1)
	plan, err := harness.BuildCompactionPlan(messages, rt.state, harness.PlanOptions{
		RetainRecentUnits: harness.DefaultRetainRecentUnits,
	})
	if err != nil {
		t.Fatalf("BuildCompactionPlan() error = %v", err)
	}
	// 调整历史使总字节数不被 4 整除，构造估算持平所需的精确摘要长度。
	for harness.VisibleMessagesBytes(messages)%4 == 0 {
		messages[2].Content[0].Text += "x"
	}
	total := harness.VisibleMessagesBytes(messages)
	rangeBytes := harness.VisibleMessagesBytes(plan.SummaryMessages)
	targetCheckpoint := rangeBytes - total%4
	base := harness.VisibleMessagesBytes([]ai.Message{{
		Role:    ai.RoleUser,
		Content: []ai.ContentBlock{ai.TextBlock(harness.WrapCompactedSummary(""))},
	}})
	bodyLen := targetCheckpoint - base
	if bodyLen <= 0 {
		t.Fatalf("cannot construct tie summary: target %d, base %d", targetCheckpoint, base)
	}

	provider := &fakeCompactionProvider{steps: []compactionStep{
		{message: compactSummaryResponse(t, strings.Repeat("s", bodyLen))},
	}}
	pressure := int64(total/4) + harness.DefaultReserveOutputTokens + harness.DefaultSafetyMarginTokens
	window := pressure * 5 / 4
	loop := NewLoopWithCompaction(provider, nil, false,
		harness.CompactionConfig{ContextWindowTokens: window, EnablePrune: true})
	rt = newCompactionRuntime(loop.compaction, 1)

	got, err := loop.maybeCompact(context.Background(), messages, nil, rt, nil)
	if err != nil {
		t.Fatalf("maybeCompact() error = %v", err)
	}
	if provider.calls != 1 {
		t.Fatalf("summary calls = %d, want 1", provider.calls)
	}
	if len(got) != len(messages) {
		t.Fatalf("history must not be replaced on token-estimate tie: %d != %d", len(got), len(messages))
	}
	if len(rt.state.CheckpointIndexes) != 0 {
		t.Fatalf("CheckpointIndexes = %v, state must not commit without token progress", rt.state.CheckpointIndexes)
	}
}
