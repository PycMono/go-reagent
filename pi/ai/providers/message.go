package providers

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/PycMono/go-reagent/pi/ai"
)

type normalizedMessage struct {
	role       ai.Role
	blocks     []ai.ContentBlock
	toolCalls  []normalizedToolCall
	toolCallID string
	isError    bool
}

type normalizedToolCall struct {
	id        string
	name      string
	arguments json.RawMessage
	input     any
}

func normalizeMessages(messages []ai.Message, vision bool) ([]normalizedMessage, error) {
	result := make([]normalizedMessage, 0, len(messages))
	for _, message := range messages {
		blocks, err := normalizeBlocks(message.Role, message.Content, vision)
		if err != nil {
			return nil, fmt.Errorf("message content: %w", err)
		}

		normalized := normalizedMessage{
			role:       message.Role,
			blocks:     blocks,
			toolCallID: message.ToolCallID,
			isError:    message.IsError,
		}
		switch message.Role {
		case ai.RoleSystem, ai.RoleUser:
		case ai.RoleTool:
			if message.ToolCallID == "" {
				return nil, errors.New("tool message requires tool_call_id")
			}
		case ai.RoleAssistant:
			assistantText, err := messageText(blocks)
			if err != nil {
				return nil, err
			}
			if assistantText == "" && len(message.ToolCalls) == 0 {
				return nil, errors.New("assistant message contains no content or tool calls")
			}
			for _, toolCall := range message.ToolCalls {
				var input any
				if err := json.Unmarshal(toolCall.Arguments, &input); err != nil {
					return nil, fmt.Errorf("tool call %q arguments: %w", toolCall.ID, err)
				}
				normalized.toolCalls = append(normalized.toolCalls, normalizedToolCall{
					id: toolCall.ID, name: toolCall.Name,
					arguments: toolCall.Arguments, input: input,
				})
			}
		default:
			return nil, fmt.Errorf("unsupported message role %q", message.Role)
		}
		result = append(result, normalized)
	}
	return result, nil
}

// normalizeBlocks 校验并归一化一条消息的内容块：先做联合类型防御性校验，
// 再按 role 契约拒绝不携带图像的角色；vision=false 时把 user 消息中的
// 图像块降级为脱敏占位文本。其余情况保序透传。
func normalizeBlocks(role ai.Role, content []ai.ContentBlock, vision bool) ([]ai.ContentBlock, error) {
	result := make([]ai.ContentBlock, 0, len(content))
	for _, block := range content {
		if err := block.Validate(); err != nil {
			return nil, err
		}
		if block.Type == ai.ContentTypeImage && role != ai.RoleUser {
			return nil, fmt.Errorf("role %q must not carry image blocks", role)
		}
		if block.Type == ai.ContentTypeImage && !vision {
			result = append(result, ai.TextBlock(ai.ImagePlaceholderText(block.Image.URL)))
			continue
		}
		result = append(result, block)
	}
	return result, nil
}

// messageText 拼接归一化消息的全部文本块；仅用于协议中 content 为纯字符串
// 的角色（system/tool/assistant），这些角色不允许携带图像块。
func (message normalizedMessage) text() (string, error) {
	return messageText(message.blocks)
}

func messageText(blocks []ai.ContentBlock) (string, error) {
	var builder strings.Builder
	for _, block := range blocks {
		if block.Type != ai.ContentTypeText {
			return "", fmt.Errorf("text-only role must not carry %q blocks", block.Type)
		}
		builder.WriteString(block.Text)
	}
	return builder.String(), nil
}
