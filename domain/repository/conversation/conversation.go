package conversation

import (
	"context"

	conversationentity "github.com/PycMono/go-reagent/domain/entity/conversation"
)

// IConversationRepository persists conversations, their message history, and model usage.
type IConversationRepository interface {
	// FindByUserIDAndConversationID finds conversation metadata. Missing data returns (nil, false, nil).
	FindByUserIDAndConversationID(ctx context.Context, userID string, conversationID string) (*conversationentity.Conversation, bool, error)
	// Create creates conversation metadata and assigns its string ID when empty.
	Create(ctx context.Context, conversation *conversationentity.Conversation) error
	// ListMessagesByConversationID returns bounded history in chronological order.
	ListMessagesByConversationID(ctx context.Context, conversationID string, messageLimit int) ([]*conversationentity.Message, error)
	// AppendTurn atomically advances the conversation version and stores messages and model invocations.
	AppendTurn(
		ctx context.Context,
		userID string,
		conversationID string,
		expectedVersion uint64,
		messages []*conversationentity.Message,
		invocations []*conversationentity.ModelInvocation,
	) error
}

// IAgentConversationRepository adds tenant-bound Web conversation operations.
// Implementations derive verified tenant/user ownership from the context.
type IAgentConversationRepository interface {
	CreateBound(context.Context, *conversationentity.Conversation) error
	CommitAgentVersion(ctx context.Context, userID, conversationID, expectedAgentVersionID, targetVersionID string) error
}
