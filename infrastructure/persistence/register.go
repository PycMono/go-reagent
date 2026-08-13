package persistence

import (
	sqlsdk "github.com/PycMono/go-mysql-sdk"
	"github.com/PycMono/go-mysql-sdk/transaction"
	"github.com/PycMono/go-reagent/domain/repository"
	conversationrepo "github.com/PycMono/go-reagent/domain/repository/conversation"
	conversationpersistence "github.com/PycMono/go-reagent/infrastructure/persistence/conversation"
	"go.uber.org/fx"
)

// Register registers persistence implementations against domain interfaces.
var Register = fx.Options(
	fx.Provide(func(provider sqlsdk.Provider, transactions transaction.Manager, idService repository.IIDService) conversationrepo.IConversationRepository {
		return conversationpersistence.NewConversationRepo(provider, transactions, idService)
	}),
)
