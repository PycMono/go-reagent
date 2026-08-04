package mysql

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/PycMono/go-reagent/internal/conversation"
	"github.com/PycMono/go-reagent/internal/schema"
)

func encodeMessage(message schema.Message) (messageRow, error) {
	if !isPersistedRole(message.Role) {
		return messageRow{}, fmt.Errorf("mysql conversation: unsupported message role %q", message.Role)
	}
	payload, err := json.Marshal(message)
	if err != nil {
		return messageRow{}, fmt.Errorf("mysql conversation: encode message: %w", err)
	}
	return messageRow{Role: string(message.Role), Payload: jsonPayload(payload)}, nil
}

func decodeMessage(row messageRow) (schema.Message, error) {
	var message schema.Message
	if err := json.Unmarshal(row.Payload, &message); err != nil {
		return schema.Message{}, errors.Join(conversation.ErrCorruptMessage, fmt.Errorf("decode payload: %w", err))
	}
	if !isPersistedRole(message.Role) {
		return schema.Message{}, errors.Join(conversation.ErrCorruptMessage, fmt.Errorf("unsupported payload role %q", message.Role))
	}
	if row.Role != string(message.Role) {
		return schema.Message{}, errors.Join(
			conversation.ErrCorruptMessage,
			fmt.Errorf("stored role %q does not match payload role %q", row.Role, message.Role),
		)
	}
	return message, nil
}

func isPersistedRole(role schema.Role) bool {
	return role == schema.RoleUser || role == schema.RoleAssistant || role == schema.RoleTool
}
