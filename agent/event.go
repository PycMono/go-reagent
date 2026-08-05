package agent

import "github.com/PycMono/go-reagent/ai"

type ToolOutput struct {
	Content []ai.ContentBlock `json:"content"`
	Details any               `json:"details,omitempty"`
}

type ToolUpdate struct {
	Content []ai.ContentBlock `json:"content"`
	Details any               `json:"details,omitempty"`
}

type ToolResult struct {
	ToolCallID string            `json:"tool_call_id"`
	ToolName   string            `json:"tool_name"`
	Content    []ai.ContentBlock `json:"content"`
	Details    any               `json:"details,omitempty"`
	IsError    bool              `json:"is_error"`
}

type AgentEventType string

const (
	AgentEventThinking   AgentEventType = "thinking"
	AgentEventToolStart  AgentEventType = "tool_start"
	AgentEventToolUpdate AgentEventType = "tool_update"
	AgentEventToolEnd    AgentEventType = "tool_end"
	AgentEventMessage    AgentEventType = "message"
)

type ToolEventPhase string

const (
	ToolEventStart  ToolEventPhase = "start"
	ToolEventUpdate ToolEventPhase = "update"
	ToolEventEnd    ToolEventPhase = "end"
)

type ToolEvent struct {
	Phase  ToolEventPhase `json:"phase"`
	Call   ai.ToolCall    `json:"call"`
	Update *ToolUpdate    `json:"update,omitempty"`
	Result *ToolResult    `json:"result,omitempty"`
}

type AgentEvent struct {
	Type    AgentEventType `json:"type"`
	Tool    *ToolEvent     `json:"tool,omitempty"`
	Message *ai.Message    `json:"message,omitempty"`
}

func NewToolStart(call ai.ToolCall) ToolEvent {
	return ToolEvent{Phase: ToolEventStart, Call: call}
}

func NewToolUpdate(call ai.ToolCall, update ToolUpdate) ToolEvent {
	return ToolEvent{Phase: ToolEventUpdate, Call: call, Update: &update}
}

func NewToolEnd(call ai.ToolCall, result ToolResult) ToolEvent {
	return ToolEvent{Phase: ToolEventEnd, Call: call, Result: &result}
}

func NewAgentToolEvent(event ToolEvent) AgentEvent {
	var eventType AgentEventType
	switch event.Phase {
	case ToolEventStart:
		eventType = AgentEventToolStart
	case ToolEventUpdate:
		eventType = AgentEventToolUpdate
	case ToolEventEnd:
		eventType = AgentEventToolEnd
	}
	return AgentEvent{Type: eventType, Tool: &event}
}

func NewToolStartEvent(call ai.ToolCall) AgentEvent {
	return NewAgentToolEvent(NewToolStart(call))
}

func NewToolUpdateEvent(call ai.ToolCall, update ToolUpdate) AgentEvent {
	return NewAgentToolEvent(NewToolUpdate(call, update))
}

func NewToolEndEvent(call ai.ToolCall, result ToolResult) AgentEvent {
	return NewAgentToolEvent(NewToolEnd(call, result))
}

func NewThinkingEvent() AgentEvent {
	return AgentEvent{Type: AgentEventThinking}
}

func NewMessageEvent(message ai.Message) AgentEvent {
	return AgentEvent{Type: AgentEventMessage, Message: &message}
}
