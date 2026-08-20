package harness

import (
	"encoding/json"
	"fmt"

	"github.com/PycMono/go-reagent/pi/ai"
)

// PruneOptions 控制单次 Run 内只读工具结果裁剪（L1）的行为。
type PruneOptions struct {
	// Enable 为 false 时仅返回内容相同的拷贝，不裁剪任何消息。
	Enable bool
	// ProtectRecentToolGroups 保护最近若干个完整工具组不被裁剪。
	ProtectRecentToolGroups int
	// MaxToolResultBytes 是单条工具结果 Content 的 JSON 序列化字节阈值，超过才裁剪。
	MaxToolResultBytes int
	// KeepErrors 为 true 时不裁剪错误结果。
	KeepErrors bool
	// PrunableTools 是允许裁剪的只读工具白名单；未列入的工具永不裁剪。
	PrunableTools map[string]struct{}
}

// PruneStats 记录一次裁剪的规模。
type PruneStats struct {
	PrunedMessages int
	BytesBefore    int
	BytesAfter     int
}

// PruneToolResults 将超过阈值的旧只读工具结果替换为裁剪占位文本。
// 输入切片与消息不被原地修改；返回值始终是拷贝。
func PruneToolResults(messages []ai.Message, opts PruneOptions) ([]ai.Message, PruneStats) {
	stats := PruneStats{BytesBefore: contentBytes(messages)}
	if !opts.Enable {
		copied := append([]ai.Message(nil), messages...)
		stats.BytesAfter = stats.BytesBefore
		return copied, stats
	}

	protected := protectedToolResultIndexes(messages, opts.ProtectRecentToolGroups)
	result := append([]ai.Message(nil), messages...)
	for index, message := range messages {
		if message.Role != ai.RoleTool || protected[index] {
			continue
		}
		if _, ok := opts.PrunableTools[message.ToolName]; !ok {
			continue
		}
		if message.IsError && opts.KeepErrors {
			continue
		}
		size := contentBytesOf(message.Content)
		if size <= opts.MaxToolResultBytes {
			continue
		}
		pruned := message
		pruned.Content = []ai.ContentBlock{ai.TextBlock(fmt.Sprintf(
			"[工具结果已裁剪] tool=%s，原 %d 字节。如仍需要，请重新执行对应只读工具。",
			message.ToolName, size,
		))}
		result[index] = pruned
		stats.PrunedMessages++
	}
	stats.BytesAfter = contentBytes(result)
	return result, stats
}

// protectedToolResultIndexes 标记属于最近 groups 个完整工具组的 RoleTool 下标。
// 完整工具组 = 带 ToolCalls 的 assistant 消息 + 紧随其后的全部 RoleTool 结果。
func protectedToolResultIndexes(messages []ai.Message, groups int) map[int]bool {
	protected := make(map[int]bool)
	if groups <= 0 {
		return protected
	}
	groupStarts := make([]int, 0)
	for index, message := range messages {
		if message.Role == ai.RoleAssistant && len(message.ToolCalls) > 0 {
			groupStarts = append(groupStarts, index)
		}
	}
	if len(groupStarts) > groups {
		groupStarts = groupStarts[len(groupStarts)-groups:]
	}
	for _, start := range groupStarts {
		for index := start + 1; index < len(messages) && messages[index].Role == ai.RoleTool; index++ {
			protected[index] = true
		}
	}
	return protected
}

func contentBytes(messages []ai.Message) int {
	total := 0
	for _, message := range messages {
		total += contentBytesOf(message.Content)
	}
	return total
}

func contentBytesOf(content []ai.ContentBlock) int {
	encoded, err := json.Marshal(content)
	if err != nil {
		return 0
	}
	return len(encoded)
}
