package pi

import (
	"fmt"
	"strings"

	"github.com/PycMono/go-reagent/pi/ai"
	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
)

// HistoryContentType 表示历史消息的内容类型。
type HistoryContentType string

const (
	// HistoryContentTypeText 表示纯文本历史消息。
	HistoryContentTypeText HistoryContentType = "text"
)

// HistorySenderType 表示历史消息的发送方类型。
type HistorySenderType string

const (
	// HistorySenderTypeAI 表示消息由 AI 发送。
	HistorySenderTypeAI HistorySenderType = "ai"
	// HistorySenderTypeCustomer 表示消息由客户发送。
	HistorySenderTypeCustomer HistorySenderType = "customer"
)

// HistoryMessage 表示调用方传入的一条业务历史消息。
type HistoryMessage struct {
	// ContentType 表示消息内容类型，目前仅支持 text。
	ContentType HistoryContentType `json:"content_type"`
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
	SenderType HistorySenderType `json:"sender_type"`
}

func (message HistoryMessage) toAIMessage() (ai.Message, error) {
	if message.ContentType != HistoryContentTypeText {
		return ai.Message{}, fmt.Errorf(
			"%w: history content type must be %q, got %q",
			pierrors.ErrRequestInvalid,
			HistoryContentTypeText,
			message.ContentType,
		)
	}
	if strings.TrimSpace(message.Content) == "" {
		return ai.Message{}, fmt.Errorf("%w: history content must not be empty", pierrors.ErrRequestInvalid)
	}

	var role ai.Role
	switch message.SenderType {
	case HistorySenderTypeCustomer:
		role = ai.RoleUser
	case HistorySenderTypeAI:
		role = ai.RoleAssistant
	default:
		return ai.Message{}, fmt.Errorf(
			"%w: unsupported history sender type %q",
			pierrors.ErrRequestInvalid,
			message.SenderType,
		)
	}
	return ai.Message{Role: role, Content: []ai.ContentBlock{ai.TextBlock(message.Content)}}, nil
}

func historyMessagesToAI(messages []HistoryMessage) ([]ai.Message, error) {
	if messages == nil {
		return nil, nil
	}
	converted := make([]ai.Message, len(messages))
	for index, message := range messages {
		convertedMessage, err := message.toAIMessage()
		if err != nil {
			return nil, fmt.Errorf("history message %d: %w", index, err)
		}
		converted[index] = convertedMessage
	}
	return converted, nil
}
