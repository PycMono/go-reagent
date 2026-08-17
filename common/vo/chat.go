package vo

import (
	"encoding/json"
	"time"
)

type ConversationVO struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	ProfileCode  string    `json:"profile_code"`
	MessageTotal int64     `json:"message_total"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type AgentProfileStarterVO struct {
	Title  string `json:"title"`
	Prompt string `json:"prompt"`
}

type AgentProfileVO struct {
	Code        string                  `json:"code"`
	Name        string                  `json:"name"`
	Description string                  `json:"description"`
	Icon        string                  `json:"icon"`
	Selectable  bool                    `json:"selectable"`
	Welcome     string                  `json:"welcome"`
	Starters    []AgentProfileStarterVO `json:"starters"`
}

type AgentProfileCatalogVO struct {
	Items          []*AgentProfileVO `json:"items"`
	DefaultProfile string            `json:"default_profile"`
}

type ConversationPageVO struct {
	Items      []*ConversationVO `json:"items"`
	NextCursor string            `json:"next_cursor,omitempty"`
}

type ContentBlockVO struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type ToolCallVO struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type MessageVO struct {
	ID          string           `json:"id"`
	TurnVersion uint64           `json:"turn_version"`
	Ordinal     uint32           `json:"ordinal"`
	RunID       string           `json:"run_id,omitempty"`
	Role        string           `json:"role"`
	Content     []ContentBlockVO `json:"content,omitempty"`
	ToolCalls   []ToolCallVO     `json:"tool_calls,omitempty"`
	ToolCallID  string           `json:"tool_call_id,omitempty"`
	ToolName    string           `json:"tool_name,omitempty"`
	IsError     bool             `json:"is_error,omitempty"`
	CreatedAt   time.Time        `json:"created_at"`
}

type MessagePageVO struct {
	Items      []*MessageVO `json:"items"`
	NextCursor string       `json:"next_cursor,omitempty"`
}

type RunEventType string

const (
	RunEventRunStarted       RunEventType = "run.started"
	RunEventAgentThinking    RunEventType = "agent.thinking"
	RunEventToolStarted      RunEventType = "tool.started"
	RunEventToolUpdated      RunEventType = "tool.updated"
	RunEventToolCompleted    RunEventType = "tool.completed"
	RunEventMessageStarted   RunEventType = "message.started"
	RunEventMessageDelta     RunEventType = "message.delta"
	RunEventMessageCompleted RunEventType = "message.completed"
	RunEventRunFailed        RunEventType = "run.failed"
	RunEventRunCompleted     RunEventType = "run.completed"
)

type ToolEventVO struct {
	ID        string           `json:"id"`
	Name      string           `json:"name"`
	Arguments json.RawMessage  `json:"arguments,omitempty"`
	Content   []ContentBlockVO `json:"content,omitempty"`
	Details   any              `json:"details,omitempty"`
	IsError   bool             `json:"is_error,omitempty"`
	ErrorCode string           `json:"error_code,omitempty"`
}

type RunMessageVO struct {
	Role       string           `json:"role"`
	Content    []ContentBlockVO `json:"content,omitempty"`
	ToolCalls  []ToolCallVO     `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	ToolName   string           `json:"tool_name,omitempty"`
	IsError    bool             `json:"is_error,omitempty"`
}

type RunErrorVO struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type RunEventVO struct {
	Type    RunEventType    `json:"-"`
	RunID   string          `json:"run_id,omitempty"`
	Tool    *ToolEventVO    `json:"tool,omitempty"`
	Delta   *ContentBlockVO `json:"delta,omitempty"`
	Message *RunMessageVO   `json:"message,omitempty"`
	Error   *RunErrorVO     `json:"error,omitempty"`
}
