package harness

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/PycMono/go-reagent/pi/ai"
)

// prunedMarkerPrefix 标识已被本 pruner 裁剪的占位文本，用于幂等扫描。
const prunedMarkerPrefix = "[工具结果已裁剪]"

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
// 只裁剪属于完整工具组的结果：孤立 Tool 消息与未闭合组既不裁剪，
// 也不占用保护名额。占位文本必须严格小于原内容；已裁剪结果不会被
// 二次裁剪。输入切片与消息不被原地修改；返回值始终是拷贝。
func PruneToolResults(messages []ai.Message, opts PruneOptions) ([]ai.Message, PruneStats) {
	stats := PruneStats{BytesBefore: contentBytes(messages)}
	if !opts.Enable {
		copied := clonePruneMessages(messages)
		stats.BytesAfter = stats.BytesBefore
		return copied, stats
	}

	groups := scanToolGroups(messages)
	protected := protectedToolResultIndexes(groups, opts.ProtectRecentToolGroups)
	prunable := make(map[int]bool)
	for _, group := range groups {
		if !group.complete {
			continue
		}
		for _, index := range group.results {
			prunable[index] = true
		}
	}

	result := clonePruneMessages(messages)
	for index, message := range messages {
		if message.Role != ai.RoleTool || !prunable[index] || protected[index] {
			continue
		}
		if _, ok := opts.PrunableTools[message.ToolName]; !ok {
			continue
		}
		if message.IsError && opts.KeepErrors {
			continue
		}
		size := contentBytesOf(message.Content)
		if size <= opts.MaxToolResultBytes || isPrunedMarker(message.Content) {
			continue
		}
		placeholder := []ai.ContentBlock{ai.TextBlock(fmt.Sprintf(
			"%s tool=%s，原 %d 字节。如仍需要，请重新执行对应只读工具。",
			prunedMarkerPrefix, message.ToolName, size,
		))}
		// 占位必须严格小于原内容，否则放弃裁剪（小阈值下保证不增长）。
		if contentBytesOf(placeholder) >= size {
			continue
		}
		pruned := message
		pruned.Content = placeholder
		result[index] = pruned
		stats.PrunedMessages++
	}

	stats.BytesAfter = contentBytes(result)
	return result, stats
}

// toolGroup 是一条带 ToolCalls 的 assistant 消息及其紧随的 RoleTool 结果。
// complete 仅在结果 ToolCallID 集合与调用 ID 集合完整匹配（无缺漏、无多余、
// 无重复）时成立；未闭合或 ID 不匹配的组不算完整组。
type toolGroup struct {
	start    int
	results  []int
	complete bool
}

func scanToolGroups(messages []ai.Message) []toolGroup {
	groups := make([]toolGroup, 0)
	for index, message := range messages {
		if message.Role != ai.RoleAssistant || len(message.ToolCalls) == 0 {
			continue
		}
		group := toolGroup{start: index}
		want := make(map[string]struct{}, len(message.ToolCalls))
		for _, call := range message.ToolCalls {
			want[call.ID] = struct{}{}
		}
		seen := make(map[string]struct{}, len(message.ToolCalls))
		valid := true
		for cursor := index + 1; cursor < len(messages) && messages[cursor].Role == ai.RoleTool; cursor++ {
			id := messages[cursor].ToolCallID
			if _, ok := want[id]; !ok {
				valid = false
			} else if _, dup := seen[id]; dup {
				valid = false
			}
			seen[id] = struct{}{}
			group.results = append(group.results, cursor)
		}
		group.complete = valid && len(seen) == len(want)
		groups = append(groups, group)
	}
	return groups
}

// protectedToolResultIndexes 标记最近 groups 个完整工具组的 RoleTool 下标；
// 未闭合组不占用保护名额。
func protectedToolResultIndexes(groups []toolGroup, count int) map[int]bool {
	protected := make(map[int]bool)
	if count <= 0 {
		return protected
	}
	complete := make([]toolGroup, 0, len(groups))
	for _, group := range groups {
		if group.complete {
			complete = append(complete, group)
		}
	}
	if len(complete) > count {
		complete = complete[len(complete)-count:]
	}
	for _, group := range complete {
		for _, index := range group.results {
			protected[index] = true
		}
	}
	return protected
}

func isPrunedMarker(content []ai.ContentBlock) bool {
	if len(content) != 1 {
		return false
	}
	return strings.HasPrefix(content[0].Text, prunedMarkerPrefix)
}

// clonePruneMessages 拷贝消息切片及 Content/ToolCalls（含每个调用的
// Arguments）底层数组，使调用方对返回值的后续修改不会写回输入。
func clonePruneMessages(messages []ai.Message) []ai.Message {
	cloned := make([]ai.Message, len(messages))
	for index, message := range messages {
		cloned[index] = message
		if message.Content != nil {
			cloned[index].Content = append([]ai.ContentBlock(nil), message.Content...)
		}
		if message.ToolCalls != nil {
			calls := make([]ai.ToolCall, len(message.ToolCalls))
			for callIndex, call := range message.ToolCalls {
				calls[callIndex] = call
				if call.Arguments != nil {
					calls[callIndex].Arguments = append([]byte(nil), call.Arguments...)
				}
			}
			cloned[index].ToolCalls = calls
		}
	}
	return cloned
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
