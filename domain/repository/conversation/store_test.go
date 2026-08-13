package conversation_test

import (
	"context"

	conversationentity "github.com/PycMono/go-reagent/domain/entity/conversation"
	conversationrepo "github.com/PycMono/go-reagent/domain/repository/conversation"
)

type fakeStore struct{}

func (fakeStore) LoadOrCreate(context.Context, conversationentity.Key, int) (conversationentity.Snapshot, error) {
	return conversationentity.Snapshot{}, nil
}

func (fakeStore) AppendTurn(context.Context, conversationentity.AppendRequest) error {
	return nil
}

var _ conversationrepo.Store = fakeStore{}
