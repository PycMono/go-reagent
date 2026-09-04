package pi

import (
	"fmt"
	"strings"

	"github.com/PycMono/go-reagent/pi/ai"
	pierrors "github.com/PycMono/go-reagent/pi/errors"
	"github.com/PycMono/go-reagent/pi/governor"
)

// MaxImagesPerMessage 是单条消息允许附加的图片数量上限。每张图按固定
// token 常量参与计量与压缩规划（4 张 ≈ 16KB 虚拟字节），超限会破坏压缩
// unit 边界；入口在 Message2AI 处 fail-fast。
const MaxImagesPerMessage = 4

// RunRequest 保存一次无状态运行所需的调用方输入。
type RunRequest struct {
	// History 是本轮运行开始前、面向业务的文本会话历史。
	History []Message `json:"history,omitempty"`
	// Input 是本轮用户输入消息。
	Input Message `json:"input"`
	// Context 是本轮额外注入的业务上下文。
	Context []ContextBlock `json:"context,omitempty"`
	// Limits 是本轮运行的确定性资源上限；未配置（零值）的字段使用
	// governor.DefaultLimits 对应字段的默认值。
	Limits governor.Limits `json:"limits,omitempty"`
}

// Validate 校验请求的固有契约。它不触碰 History/Input 转换、Context
// 构造或任何 Provider 调用，调用方必须在那些步骤之前执行。
func (request RunRequest) Validate() error {
	if err := request.Limits.Validate(); err != nil {
		return err
	}
	for index, block := range request.Context {
		if strings.TrimSpace(block.Name) == "" {
			return fmt.Errorf("%w: context block %d name must not be empty", pierrors.ErrRequestInvalid, index)
		}
		if strings.TrimSpace(block.Content) == "" {
			return fmt.Errorf("%w: context block %d content must not be empty", pierrors.ErrRequestInvalid, index)
		}
	}

	return nil
}

// ContextBlock 表示运行时注入到会话历史之前的一段业务上下文。
type ContextBlock struct {
	// Name 是上下文名称。
	Name string `json:"name"`
	// Content 是上下文内容。
	Content string `json:"content"`
	// Priority 决定上下文的排列顺序，数值越大越靠前。
	Priority int `json:"priority,omitempty"`
}

// RunResult 保存一次运行中新产生的消息、模型调用记录和终止结果。
type RunResult struct {
	// NewMessages 是本次运行新增的 Assistant 和 Tool 消息。
	NewMessages []ai.Message `json:"new_messages,omitempty"`
	// Invocations 是本次运行已完成的模型调用记录：主运行的调用按完成顺序
	// 排列；子代理调用在其工具批次结算时按 drain 顺序追加，Sequence 单调
	// 递增，但 ProviderRequestIndex 不保证随账本顺序递增（跨子代理弱序）。
	Invocations []governor.Invocation `json:"invocations,omitempty"`
	// Termination 是本次运行的结构化终止结果。
	Termination governor.Termination `json:"termination"`
}

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
	// ImageURLs 是可选附加的图片 URL 列表；正文必填，图片以 image 块追加在
	// 文本块之后发送给模型。URL 必须为 http/https。
	ImageURLs []string `json:"image_urls,omitempty"`
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

	content := []ai.ContentBlock{ai.TextBlock(message.Content)}
	if len(message.ImageURLs) > 0 {
		if role != ai.RoleUser {
			return ai.Message{}, fmt.Errorf("%w: only customer messages may attach images", pierrors.ErrRequestInvalid)
		}
		if len(message.ImageURLs) > MaxImagesPerMessage {
			return ai.Message{}, fmt.Errorf("%w: at most %d images per message, got %d",
				pierrors.ErrRequestInvalid, MaxImagesPerMessage, len(message.ImageURLs))
		}
		for _, imageURL := range message.ImageURLs {
			block := ai.ImageBlock(imageURL)
			if err := block.Validate(); err != nil {
				return ai.Message{}, fmt.Errorf("%w: %v", pierrors.ErrRequestInvalid, err)
			}
			content = append(content, block)
		}
	}
	return ai.Message{Role: role, Content: content}, nil
}
