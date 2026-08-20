package harness

import (
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/pi/ai"
)

func pruneTestOptions() PruneOptions {
	return PruneOptions{
		Enable:                  true,
		ProtectRecentToolGroups: 1,
		MaxToolResultBytes:      128,
		KeepErrors:              true,
		PrunableTools:           map[string]struct{}{"read": {}},
	}
}

func pruneToolGroup(callID string, toolName string, content string) []ai.Message {
	return []ai.Message{
		{
			Role: ai.RoleAssistant,
			ToolCalls: []ai.ToolCall{
				{ID: callID, Name: toolName, Arguments: []byte(`{"path":"a"}`)},
			},
		},
		{
			Role:       ai.RoleTool,
			ToolCallID: callID,
			ToolName:   toolName,
			Content:    []ai.ContentBlock{ai.TextBlock(content)},
		},
	}
}

func pruneBigContent() string {
	return strings.Repeat("x", 4096)
}

func TestPruneToolResultsDisabledReturnsCopy(t *testing.T) {
	messages := pruneToolGroup("c1", "read", pruneBigContent())
	opts := pruneTestOptions()
	opts.Enable = false

	got, stats := PruneToolResults(messages, opts)
	if stats.PrunedMessages != 0 || stats.BytesAfter != stats.BytesBefore {
		t.Fatalf("stats = %+v, want no pruning", stats)
	}
	if got[1].Content[0].Text != messages[1].Content[0].Text {
		t.Fatal("disabled prune modified content")
	}
	got[1].Content[0].Text = "mutated"
	if messages[1].Content[0].Text == "mutated" {
		t.Fatal("returned messages share content with input")
	}
}

func TestPruneToolResultsPrunesWhitelistedReadOnly(t *testing.T) {
	messages := append(
		pruneToolGroup("c1", "read", pruneBigContent()),
		pruneToolGroup("c2", "read", pruneBigContent())...,
	)

	got, stats := PruneToolResults(messages, pruneTestOptions())
	if stats.PrunedMessages != 1 {
		t.Fatalf("PrunedMessages = %d, want 1 (recent group protected)", stats.PrunedMessages)
	}
	if !isPrunedMarker(got[1].Content) {
		t.Fatalf("old read result not pruned: %q", got[1].Content[0].Text)
	}
	if isPrunedMarker(got[3].Content) {
		t.Fatal("recent group result must be protected")
	}
	if stats.BytesAfter >= stats.BytesBefore {
		t.Fatalf("BytesAfter %d must be strictly smaller than BytesBefore %d", stats.BytesAfter, stats.BytesBefore)
	}
}

func TestPruneToolResultsKeepsNonWhitelistedTools(t *testing.T) {
	messages := pruneToolGroup("c1", "exec", pruneBigContent())
	opts := pruneTestOptions()
	opts.ProtectRecentToolGroups = 0

	got, stats := PruneToolResults(messages, opts)
	if stats.PrunedMessages != 0 || isPrunedMarker(got[1].Content) {
		t.Fatal("exec result must never be pruned")
	}
}

func TestPruneToolResultsKeepErrors(t *testing.T) {
	build := func() []ai.Message {
		messages := pruneToolGroup("c1", "read", pruneBigContent())
		messages[1].IsError = true
		return messages
	}
	opts := pruneTestOptions()
	opts.ProtectRecentToolGroups = 0

	got, _ := PruneToolResults(build(), opts)
	if isPrunedMarker(got[1].Content) {
		t.Fatal("error result must be kept when KeepErrors is true")
	}

	opts.KeepErrors = false
	got, stats := PruneToolResults(build(), opts)
	if stats.PrunedMessages != 1 || !isPrunedMarker(got[1].Content) {
		t.Fatal("error result should be pruned when KeepErrors is false")
	}
	if !got[1].IsError {
		t.Fatal("IsError must be preserved")
	}
}

func TestPruneToolResultsPreservesMessageFields(t *testing.T) {
	messages := pruneToolGroup("c1", "read", pruneBigContent())
	opts := pruneTestOptions()
	opts.ProtectRecentToolGroups = 0

	got, _ := PruneToolResults(messages, opts)
	pruned := got[1]
	if pruned.Role != ai.RoleTool || pruned.ToolCallID != "c1" || pruned.ToolName != "read" || pruned.IsError {
		t.Fatalf("message fields changed: %+v", pruned)
	}
}

func TestPruneToolResultsSkipsOrphanToolMessage(t *testing.T) {
	// 孤立 Tool 消息：与工具组之间隔着其他消息，不属于任何连续结果段。
	orphan := ai.Message{
		Role:       ai.RoleTool,
		ToolCallID: "missing",
		ToolName:   "read",
		Content:    []ai.ContentBlock{ai.TextBlock(pruneBigContent())},
	}
	messages := append(
		pruneToolGroup("c1", "read", pruneBigContent()),
		ai.Message{Role: ai.RoleUser, Content: []ai.ContentBlock{ai.TextBlock("继续")}},
		orphan,
	)
	opts := pruneTestOptions()
	opts.ProtectRecentToolGroups = 0

	got, stats := PruneToolResults(messages, opts)
	if stats.PrunedMessages != 1 {
		t.Fatalf("PrunedMessages = %d, want 1 (only the complete group)", stats.PrunedMessages)
	}
	if isPrunedMarker(got[3].Content) {
		t.Fatal("orphan tool result must not be pruned")
	}
}

func TestPruneToolResultsSkipsUnclosedGroup(t *testing.T) {
	// assistant 请求两个调用，只返回一个结果：未闭合组。
	unclosed := []ai.Message{
		{
			Role: ai.RoleAssistant,
			ToolCalls: []ai.ToolCall{
				{ID: "c2", Name: "read", Arguments: []byte(`{}`)},
				{ID: "c3", Name: "read", Arguments: []byte(`{}`)},
			},
		},
		{
			Role:       ai.RoleTool,
			ToolCallID: "c2",
			ToolName:   "read",
			Content:    []ai.ContentBlock{ai.TextBlock(pruneBigContent())},
		},
	}
	messages := append(pruneToolGroup("c1", "read", pruneBigContent()), unclosed...)
	opts := pruneTestOptions() // 保护最近 1 个完整组

	got, stats := PruneToolResults(messages, opts)
	if stats.PrunedMessages != 0 {
		t.Fatalf("PrunedMessages = %d, want 0: unclosed group results must not be pruned, "+
			"and the only complete group is protected", stats.PrunedMessages)
	}
	if isPrunedMarker(got[3].Content) {
		t.Fatal("unclosed group result must not be pruned")
	}
}

func TestPruneToolResultsUnclosedGroupDoesNotConsumeProtectionSlot(t *testing.T) {
	// 完整组 c1 + 尾部未闭合组。未闭合组不得占用保护名额，
	// 因此 c1（唯一的完整组）被保护，未闭合组结果仍不裁剪。
	mismatched := []ai.Message{
		{
			Role: ai.RoleAssistant,
			ToolCalls: []ai.ToolCall{
				{ID: "c2", Name: "read", Arguments: []byte(`{}`)},
			},
		},
		{
			Role:       ai.RoleTool,
			ToolCallID: "other-id",
			ToolName:   "read",
			Content:    []ai.ContentBlock{ai.TextBlock(pruneBigContent())},
		},
	}
	messages := append(pruneToolGroup("c1", "read", pruneBigContent()), mismatched...)

	got, stats := PruneToolResults(messages, pruneTestOptions())
	if stats.PrunedMessages != 0 {
		t.Fatalf("PrunedMessages = %d, want 0", stats.PrunedMessages)
	}
	if isPrunedMarker(got[1].Content) {
		t.Fatal("the only complete group should be protected")
	}
	if isPrunedMarker(got[3].Content) {
		t.Fatal("mismatched result must not be pruned")
	}
}

func TestPruneToolResultsPlaceholderNeverGrows(t *testing.T) {
	// 阈值小于占位文本长度：原内容刚刚超过阈值但小于占位，必须放弃裁剪。
	small := strings.Repeat("x", 64)
	messages := pruneToolGroup("c1", "read", small)
	opts := pruneTestOptions()
	opts.ProtectRecentToolGroups = 0
	opts.MaxToolResultBytes = 8

	got, stats := PruneToolResults(messages, opts)
	if stats.PrunedMessages != 0 {
		t.Fatalf("PrunedMessages = %d, want 0 when placeholder would not shrink", stats.PrunedMessages)
	}
	if stats.BytesAfter != stats.BytesBefore {
		t.Fatal("bytes must not grow")
	}
	if got[1].Content[0].Text != small {
		t.Fatal("content must be untouched")
	}
}

func TestPruneToolResultsIdempotent(t *testing.T) {
	messages := pruneToolGroup("c1", "read", pruneBigContent())
	opts := pruneTestOptions()
	opts.ProtectRecentToolGroups = 0

	once, stats1 := PruneToolResults(messages, opts)
	if stats1.PrunedMessages != 1 {
		t.Fatalf("first pass PrunedMessages = %d, want 1", stats1.PrunedMessages)
	}
	twice, stats2 := PruneToolResults(once, opts)
	if stats2.PrunedMessages != 0 {
		t.Fatalf("second pass PrunedMessages = %d, want 0", stats2.PrunedMessages)
	}
	if twice[1].Content[0].Text != once[1].Content[0].Text {
		t.Fatal("second pass rewrote the placeholder")
	}
}

func TestPruneToolResultsDoesNotMutateInput(t *testing.T) {
	messages := pruneToolGroup("c1", "read", pruneBigContent())
	opts := pruneTestOptions()
	opts.ProtectRecentToolGroups = 0

	_, _ = PruneToolResults(messages, opts)
	if isPrunedMarker(messages[1].Content) {
		t.Fatal("input message was modified in place")
	}
}
