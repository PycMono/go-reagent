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
