package vo

import (
	"encoding/json"
	"time"
)

type ConversationVO struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	MessageTotal int64     `json:"message_total"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
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
