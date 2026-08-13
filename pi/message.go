package pi

import (
	"fmt"
	"strings"

	"github.com/PycMono/go-reagent/pi/ai"
	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
)

// Message 表示调用方传入的一条业务消息。
type Message struct {
	// ContentType 表示消息内容类型，目前仅支持 text。
	ContentType string `json:"content_type"`
	// CreateTime 是调用方提供的可读创建时间。
	CreateTime string `json:"create_time,omitempty"`
	// CreateTS 是调用方提供的创建时间戳。
	CreateTS string `json:"create_ts,omitempty"`
	// FileURL 是调用方提供的文件地址；文本消息不会将其发送给模型。
	FileURL string `json:"file_url,omitempty"`
	// TalkerName 是消息发送方的展示名称；不会将其发送给模型。
	TalkerName string `json:"talker_name,omitempty"`
	// Content 是消息正文。
	Content string `json:"content"`
	// ID 是调用方提供的消息标识；不会将其发送给模型。
	ID string `json:"id,omitempty"`
	// SenderType 表示消息由 AI 或客户发送。
	SenderType string `json:"sender_type"`
}

// Message2AI 校验业务消息并转换为模型内部消息。
func (message Message) Message2AI() (ai.Message, error) {
	if message.ContentType != "text" {
		return ai.Message{}, fmt.Errorf(
			"%w: message content type must be %q, got %q",
			pierrors.ErrRequestInvalid,
			"text",
			message.ContentType,
		)
	}
	if strings.TrimSpace(message.Content) == "" {
		return ai.Message{}, fmt.Errorf("%w: message content must not be empty", pierrors.ErrRequestInvalid)
	}

	var role ai.Role
	switch message.SenderType {
	case "customer":
		role = ai.RoleUser
	case "ai":
		role = ai.RoleAssistant
	default:
		return ai.Message{}, fmt.Errorf(
			"%w: unsupported message sender type %q",
			pierrors.ErrRequestInvalid,
			message.SenderType,
		)
	}
	return ai.Message{Role: role, Content: []ai.ContentBlock{ai.TextBlock(message.Content)}}, nil
}
