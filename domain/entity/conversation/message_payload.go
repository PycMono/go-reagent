package conversation

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
)

type ContentType string

const ContentTypeText ContentType = "text"

type ContentBlock struct {
	Type ContentType `json:"type"`
	Text string      `json:"text"`
}

type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// MessagePayload is the JSON content stored in agent_messages.payload.
// Role and execution metadata live in Message columns and are not duplicated here.
type MessagePayload struct {
	Content    []ContentBlock `json:"content,omitempty"`
	ToolCalls  []ToolCall     `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	ToolName   string         `json:"tool_name,omitempty"`
	IsError    bool           `json:"is_error,omitempty"`
}

func (payload MessagePayload) Value() (driver.Value, error) {
	value, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode message payload: %w", err)
	}
	return value, nil
}

func (payload *MessagePayload) Scan(source any) error {
	if payload == nil {
		return errors.New("message payload receiver is nil")
	}
	var value []byte
	switch source := source.(type) {
	case []byte:
		value = source
	case string:
		value = []byte(source)
	default:
		return fmt.Errorf("unsupported message payload type %T", source)
	}
	if err := json.Unmarshal(value, payload); err != nil {
		return fmt.Errorf("decode message payload: %w", err)
	}
	return nil
}
