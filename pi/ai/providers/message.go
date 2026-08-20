package providers

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/PycMono/go-reagent/pi/ai"
)

type normalizedMessage struct {
	role       ai.Role
	text       string
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

func normalizeMessages(messages []ai.Message) ([]normalizedMessage, error) {
	result := make([]normalizedMessage, 0, len(messages))
	for _, message := range messages {
		text, err := ai.TextContent(message.Content)
		if err != nil {
			return nil, fmt.Errorf("message content: %w", err)
		}

		normalized := normalizedMessage{
			role:       message.Role,
			text:       text,
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
			if text == "" && len(message.ToolCalls) == 0 {
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
