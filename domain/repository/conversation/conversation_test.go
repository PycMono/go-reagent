package conversation_test

import (
	"context"

	conversationentity "github.com/PycMono/go-reagent/domain/entity/conversation"
	conversationrepo "github.com/PycMono/go-reagent/domain/repository/conversation"
)

type fakeStore struct{}

func (fakeStore) FindByUserIDAndConversationID(context.Context, string, string) (*conversationentity.Conversation, bool, error) {
	return nil, false, nil
}

func (fakeStore) Create(context.Context, *conversationentity.Conversation) error {
	return nil
}

func (fakeStore) ListMessagesByConversationID(context.Context, string, int) ([]*conversationentity.Message, error) {
	return nil, nil
}

func (fakeStore) AppendTurn(context.Context, string, string, uint64, []*conversationentity.Message, []*conversationentity.ModelInvocation) error {
	return nil
}

var _ conversationrepo.IConversationRepository = fakeStore{}
