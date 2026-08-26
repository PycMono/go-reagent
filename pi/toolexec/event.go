package toolexec

import (
	"github.com/PycMono/go-reagent/pi/ai"
	pierrors "github.com/PycMono/go-reagent/pi/errors"
)

// Result 是一次 Tool 调用的归一化结果。
type Result struct {
	ToolCallID string             `json:"tool_call_id"`
	ToolName   string             `json:"tool_name"`
	Content    []ai.ContentBlock  `json:"content"`
	Details    any                `json:"details,omitempty"`
	IsError    bool               `json:"is_error"`
	ErrorCode  pierrors.ErrorCode `json:"error_code,omitempty"`
}

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
	Result *Result        `json:"result,omitempty"`
}

func NewStartEvent(call ai.ToolCall) Event {
	return Event{Phase: EventStart, Call: call}
}

func NewUpdateEvent(call ai.ToolCall, update ai.ToolUpdate) Event {
	return Event{Phase: EventUpdate, Call: call, Update: &update}
}

func NewEndEvent(call ai.ToolCall, result Result) Event {
	return Event{Phase: EventEnd, Call: call, Result: &result}
}
