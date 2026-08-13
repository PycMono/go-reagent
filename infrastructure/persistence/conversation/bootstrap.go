package conversation

import (
	conversationrepo "github.com/PycMono/go-reagent/domain/repository/conversation"
	"github.com/PycMono/go-reagent/infrastructure/driver/mysql"
	"go.uber.org/fx"
)

func newRegisteredStore(connection *mysql.Connection) conversationrepo.Store {
	return NewStore(connection, connection)
}

var Module = fx.Options(fx.Provide(newRegisteredStore))
