package persistence

import (
	conversationrepo "github.com/PycMono/go-reagent/domain/repository/conversation"
	conversationpersistence "github.com/PycMono/go-reagent/infrastructure/persistence/conversation"
	"go.uber.org/fx"
)

// Register registers persistence implementations against domain interfaces.
var Register = fx.Options(
	fx.Provide(fx.Annotate(
		conversationpersistence.NewConversationRepo,
		fx.As(new(conversationrepo.IConversationRepository)),
		fx.As(new(conversationrepo.IConversationManagementRepository)),
	)),
)
