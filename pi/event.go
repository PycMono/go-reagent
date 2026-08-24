package pi

import (
	"cmp"
	"context"
	"slices"
	"strings"

	"github.com/PycMono/go-reagent/pi/ai"
	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
)

type ToolResult struct {
	ToolCallID string             `json:"tool_call_id"`
	ToolName   string             `json:"tool_name"`
	Content    []ai.ContentBlock  `json:"content"`
	Details    any                `json:"details,omitempty"`
	IsError    bool               `json:"is_error"`
	ErrorCode  pierrors.ErrorCode `json:"error_code,omitempty"`
}

type AgentEventType string

const (
	AgentEventThinking      AgentEventType = "thinking"
	AgentEventToolStart     AgentEventType = "tool_start"
	AgentEventToolUpdate    AgentEventType = "tool_update"
	AgentEventToolEnd       AgentEventType = "tool_end"
	AgentEventMessageStart  AgentEventType = "message_start"
	AgentEventMessageUpdate AgentEventType = "message_update"
	AgentEventMessageEnd    AgentEventType = "message_end"
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
	Update *ai.ToolUpdate `json:"update,omitempty"`
	Result *ToolResult    `json:"result,omitempty"`
}

type AgentEvent struct {
	Type    AgentEventType   `json:"type"`
	Tool    *ToolEvent       `json:"tool,omitempty"`
	Delta   *ai.ContentBlock `json:"delta,omitempty"`
	Message *ai.Message      `json:"message,omitempty"`
}

func NewToolStart(call ai.ToolCall) ToolEvent {
	return ToolEvent{Phase: ToolEventStart, Call: call}
}

func NewToolUpdate(call ai.ToolCall, update ai.ToolUpdate) ToolEvent {
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

func NewThinkingEvent() AgentEvent {
	return AgentEvent{Type: AgentEventThinking}
}

func NewMessageStartEvent() AgentEvent {
	return AgentEvent{Type: AgentEventMessageStart}
}

func NewMessageUpdateEvent(delta ai.ContentBlock) AgentEvent {
	return AgentEvent{Type: AgentEventMessageUpdate, Delta: &delta}
}

func NewMessageEndEvent(message ai.Message) AgentEvent {
	return AgentEvent{Type: AgentEventMessageEnd, Message: &message}
}

// EventListener receives user-facing Agent lifecycle events.
type EventListener interface {
	OnEvent(context.Context, AgentEvent)
}

// nopListener 丢弃全部事件，用于把可选 EventListener 归一化为非 nil。
type nopListener struct{}

func (nopListener) OnEvent(context.Context, AgentEvent) {}

// ListenerRegistration describes one deterministic EventListener subscriber.
type ListenerRegistration struct {
	Name     string
	Order    int
	Listener EventListener
}

type multiEventListener struct {
	registrations []ListenerRegistration
}

// NewMultiEventListener broadcasts events in Order then Name order.
func NewMultiEventListener(registrations []ListenerRegistration) EventListener {
	filtered := append([]ListenerRegistration(nil), registrations...)
	slices.SortFunc(filtered, func(a, b ListenerRegistration) int {
		if order := cmp.Compare(a.Order, b.Order); order != 0 {
			return order
		}
		return cmp.Compare(a.Name, b.Name)
	})
	return &multiEventListener{registrations: filtered}
}

func (r *multiEventListener) OnEvent(ctx context.Context, event AgentEvent) {
	for _, registration := range r.registrations {
		if strings.TrimSpace(registration.Name) == "" || registration.Listener == nil {
			continue
		}
		deliverSafely(ctx, registration.Listener, event)
	}
}

func deliverSafely(ctx context.Context, listener EventListener, event AgentEvent) {
	defer func() {
		_ = recover()
	}()
	listener.OnEvent(ctx, event)
}
