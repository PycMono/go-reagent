package chat

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"time"

	conversationrepo "github.com/PycMono/go-reagent/domain/repository/conversation"
)

type conversationCursor struct {
	UpdatedAt time.Time `json:"updated_at"`
	ID        string    `json:"id"`
}

type messageCursor struct {
	TurnVersion uint64 `json:"turn_version"`
	Ordinal     uint32 `json:"ordinal"`
}

func encodeConversationCursor(cursor conversationrepo.ListCursor) string {
	return encodeCursor(conversationCursor{UpdatedAt: cursor.UpdatedAt, ID: cursor.ID})
}

func decodeConversationCursor(value string) (*conversationrepo.ListCursor, error) {
	var cursor conversationCursor
	if err := decodeCursor(value, &cursor); err != nil {
		return nil, err
	}
	if cursor.UpdatedAt.IsZero() || cursor.ID == "" {
		return nil, errors.New("invalid conversation cursor")
	}
	return &conversationrepo.ListCursor{UpdatedAt: cursor.UpdatedAt, ID: cursor.ID}, nil
}

func encodeMessageCursor(cursor conversationrepo.MessageCursor) string {
	return encodeCursor(messageCursor{TurnVersion: cursor.TurnVersion, Ordinal: cursor.Ordinal})
}

func decodeMessageCursor(value string) (*conversationrepo.MessageCursor, error) {
	var cursor messageCursor
	if err := decodeCursor(value, &cursor); err != nil {
		return nil, err
	}
	if cursor.TurnVersion == 0 {
		return nil, errors.New("invalid message cursor")
	}
	return &conversationrepo.MessageCursor{TurnVersion: cursor.TurnVersion, Ordinal: cursor.Ordinal}, nil
}

func encodeCursor(value any) string {
	content, _ := json.Marshal(value)
	return base64.RawURLEncoding.EncodeToString(content)
}

func decodeCursor(value string, target any) error {
	if value == "" {
		return errors.New("cursor is empty")
	}
	content, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("cursor contains trailing JSON")
	}
	return nil
}
