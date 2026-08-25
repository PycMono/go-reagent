package pi

import (
	"cmp"
	"context"
	"slices"
	"strings"

	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/toolexec"
)

type AgentEventType string

const (
	AgentEventToolStart     AgentEventType = "tool_start"
	AgentEventToolUpdate    AgentEventType = "tool_update"
	AgentEventToolEnd       AgentEventType = "tool_end"
	AgentEventMessageStart  AgentEventType = "message_start"
	AgentEventMessageUpdate AgentEventType = "message_update"
	AgentEventMessageEnd    AgentEventType = "message_end"
)

type AgentEvent struct {
	Type    AgentEventType   `json:"type"`
	Tool    *toolexec.Event  `json:"tool,omitempty"`
	Delta   *ai.ContentBlock `json:"delta,omitempty"`
	Message *ai.Message      `json:"message,omitempty"`
}

func NewAgentToolEvent(event toolexec.Event) AgentEvent {
	var eventType AgentEventType
	switch event.Phase {
	case toolexec.EventStart:
		eventType = AgentEventToolStart
	case toolexec.EventUpdate:
		eventType = AgentEventToolUpdate
	case toolexec.EventEnd:
		eventType = AgentEventToolEnd
	}

	return AgentEvent{Type: eventType, Tool: &event}
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
