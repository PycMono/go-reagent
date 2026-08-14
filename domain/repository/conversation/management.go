package conversation

import (
	"context"
	"time"

	conversationentity "github.com/PycMono/go-reagent/domain/entity/conversation"
)

type ListCursor struct {
	UpdatedAt time.Time
	ID        string
}

type MessageCursor struct {
	TurnVersion uint64
	Ordinal     uint32
}

type ListQuery struct {
	UserID  string
	Keyword string
	Cursor  *ListCursor
	Limit   int
}

type ListPage struct {
	Items   []*conversationentity.ListItem
	HasMore bool
}

type MessageQuery struct {
	UserID         string
	ConversationID string
	Cursor         *MessageCursor
	Limit          int
}

type MessagePage struct {
	Items   []*conversationentity.Message
	HasMore bool
}

// IConversationManagementRepository provides owner-scoped Web chat operations.
type IConversationManagementRepository interface {
	Create(context.Context, *conversationentity.Conversation) error
	FindByUserIDAndConversationID(context.Context, string, string) (*conversationentity.Conversation, bool, error)
	ListByUserID(context.Context, ListQuery) (ListPage, error)
	ListMessages(context.Context, MessageQuery) (MessagePage, error)
	Rename(context.Context, string, string, string) error
	RenameIfUntitled(context.Context, string, string, string) error
	Delete(context.Context, string, string) error
}
