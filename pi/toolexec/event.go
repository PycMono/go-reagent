package toolexec

import (
	"context"

	"github.com/PycMono/go-reagent/pi/ai"
	pierrors "github.com/PycMono/go-reagent/pi/errors"
)

// EventObserver 接收 Tool 执行的生命周期事件。
type EventObserver func(context.Context, Event)

type EventPhase string

const (
	EventStart  EventPhase = "start"
	EventUpdate EventPhase = "update"
	EventEnd    EventPhase = "end"
)

// Event 是 Tool 执行的生命周期事件（对应 pi.dev 的
// tool_execution_start/update/end）。
type Event struct {
	Phase  EventPhase     `json:"phase"`
	Call   ai.ToolCall    `json:"call"`
	Update *ai.ToolUpdate `json:"update,omitempty"`

	Content   ai.ContentBlocks   `json:"content,omitempty"`
	Details   any                `json:"details,omitempty"`
	IsError   bool               `json:"is_error,omitempty"`
	ErrorCode pierrors.ErrorCode `json:"error_code,omitempty"`
}

func NewStartEvent(call ai.ToolCall) Event {
	return Event{Phase: EventStart, Call: call}
}

func NewUpdateEvent(call ai.ToolCall, update ai.ToolUpdate) Event {
	return Event{Phase: EventUpdate, Call: call, Update: &update}
}

func NewEndEvent(
	call ai.ToolCall,
	output ai.ToolOutput,
	isError bool,
	errorCode pierrors.ErrorCode,
) Event {
	return Event{
		Phase:     EventEnd,
		Call:      call,
		Content:   output.Content,
		Details:   output.Details,
		IsError:   isError,
		ErrorCode: errorCode,
	}
}

// ResultsMatchCalls 校验合并后的结束事件与原始调用的长度、ID、工具名
// 一一对齐。
func ResultsMatchCalls(calls ai.ToolCalls, events []Event) bool {
	if len(calls) != len(events) {
		return false
	}
	for index := range calls {
		if events[index].Phase != EventEnd ||
			calls[index].ID != events[index].Call.ID || calls[index].Name != events[index].Call.Name {
			return false
		}
	}
	return true
}

// NewRejectedEvent 构造一条确定性合成的 IsError 工具结束事件。
func NewRejectedEvent(call ai.ToolCall, code pierrors.ErrorCode, text string) Event {
	return NewEndEvent(call, ai.ToolOutput{
		Content: []ai.ContentBlock{ai.TextBlock(text)},
	}, true, code)
}

// ResultMessage 将工具结束事件转换为模型消息，复制内容以隔离后续修改。
func (event Event) ResultMessage() ai.Message {
	return ai.Message{
		Role:       ai.RoleTool,
		Content:    event.Content.Clone(),
		ToolCallID: event.Call.ID,
		ToolName:   event.Call.Name,
		IsError:    event.IsError,
	}
}
