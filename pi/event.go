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

func NewToolStartEvent(call ai.ToolCall) AgentEvent {
	return NewAgentToolEvent(NewToolStart(call))
}

func NewToolUpdateEvent(call ai.ToolCall, update ai.ToolUpdate) AgentEvent {
	return NewAgentToolEvent(NewToolUpdate(call, update))
}

func NewToolEndEvent(call ai.ToolCall, result ToolResult) AgentEvent {
	return NewAgentToolEvent(NewToolEnd(call, result))
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

// Reporter receives user-facing Agent lifecycle events.
type Reporter interface {
	Report(context.Context, AgentEvent)
}

// ReporterRegistration describes one deterministic Reporter subscriber.
type ReporterRegistration struct {
	Name     string
	Order    int
	Reporter Reporter
}

type multiReporter struct {
	registrations []ReporterRegistration
}

// NewMultiReporter broadcasts events in Order then Name order.
func NewMultiReporter(registrations []ReporterRegistration) Reporter {
	filtered := append([]ReporterRegistration(nil), registrations...)
	slices.SortFunc(filtered, func(a, b ReporterRegistration) int {
		if order := cmp.Compare(a.Order, b.Order); order != 0 {
			return order
		}
		return cmp.Compare(a.Name, b.Name)
	})
	return &multiReporter{registrations: filtered}
}

func (r *multiReporter) Report(ctx context.Context, event AgentEvent) {
	for _, registration := range r.registrations {
		if strings.TrimSpace(registration.Name) == "" || registration.Reporter == nil {
			continue
		}
		reportSafely(ctx, registration.Reporter, event)
	}
}

func reportSafely(ctx context.Context, reporter Reporter, event AgentEvent) {
	defer func() {
		_ = recover()
	}()
	reporter.Report(ctx, event)
}
