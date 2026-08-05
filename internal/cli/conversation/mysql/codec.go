package mysql

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/PycMono/go-reagent/internal/cli/conversation"
	"github.com/PycMono/go-reagent/pi/ai"
)

func encodeMessage(message ai.Message) (messageRow, error) {
	if !isPersistedRole(message.Role) {
		return messageRow{}, fmt.Errorf("mysql conversation: unsupported message role %q", message.Role)
	}
	payload, err := json.Marshal(message)
	if err != nil {
		return messageRow{}, fmt.Errorf("mysql conversation: encode message: %w", err)
	}
	return messageRow{Role: string(message.Role), Payload: jsonPayload(payload)}, nil
}

func decodeMessage(row messageRow) (ai.Message, error) {
	var message ai.Message
	if err := json.Unmarshal(row.Payload, &message); err != nil {
		return ai.Message{}, errors.Join(conversation.ErrCorruptMessage, fmt.Errorf("decode payload: %w", err))
	}
	if !isPersistedRole(message.Role) {
		return ai.Message{}, errors.Join(conversation.ErrCorruptMessage, fmt.Errorf("unsupported payload role %q", message.Role))
	}
	if row.Role != string(message.Role) {
		return ai.Message{}, errors.Join(
			conversation.ErrCorruptMessage,
			fmt.Errorf("stored role %q does not match payload role %q", row.Role, message.Role),
		)
	}
	return message, nil
}

func isPersistedRole(role ai.Role) bool {
	return role == ai.RoleUser || role == ai.RoleAssistant || role == ai.RoleTool
}
