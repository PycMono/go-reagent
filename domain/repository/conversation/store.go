package conversation

import (
	"context"

	conversationentity "github.com/PycMono/go-reagent/domain/entity/conversation"
)

type Store interface {
	LoadOrCreate(context.Context, conversationentity.Key, int) (conversationentity.Snapshot, error)
	AppendTurn(context.Context, conversationentity.AppendRequest) error
}
