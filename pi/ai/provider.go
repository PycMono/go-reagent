package ai

import "context"

// Provider 定义根据消息上下文和可用工具生成模型响应的能力。
type Provider interface {
	// Stream 根据消息上下文和可用工具创建一次模型响应流。
	Stream(context.Context, []Message, []ToolDefinition) Stream
}

// Stream 表示一次按顺序消费的模型响应流。
type Stream interface {
	Next() bool
	Current() StreamEvent
	Result() (*Message, error)
	Close() error
}

// StreamEventType 表示模型响应流中的统一事件类型。
type StreamEventType string

const (
	StreamEventStart         StreamEventType = "start"
	StreamEventTextDelta     StreamEventType = "text_delta"
	StreamEventToolCallDelta StreamEventType = "tool_call_delta"
	StreamEventDone          StreamEventType = "done"
	StreamEventError         StreamEventType = "error"
)

// ToolCallDelta 表示工具调用在模型流中的一个增量片段。
type ToolCallDelta struct {
	Index          int
	IDDelta        string
	NameDelta      string
	ArgumentsDelta string
}

// StreamEvent 是与具体模型 SDK 无关的模型响应事件。
type StreamEvent struct {
	Type          StreamEventType
	TextDelta     string
	ToolCallDelta *ToolCallDelta
}
