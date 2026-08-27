package conversation

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
)

type ContentType string

const (
	ContentTypeText  ContentType = "text"
	ContentTypeImage ContentType = "image"
)

// ContentBlock 是消息内容块；联合类型取值与 pi/ai.ContentBlock 对齐：
// text 块只携带 Text，image 块只携带 Image。
type ContentBlock struct {
	Type  ContentType   `json:"type"`
	Text  string        `json:"text,omitempty"`
	Image *ImageContent `json:"image,omitempty"`
}

// ImageContent 表示一个 URL 图像内容。
type ImageContent struct {
	// URL 是图像的可访问地址；调用方必须保证推理服务商可访问且生命周期足够长。
	URL string `json:"url"`
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
