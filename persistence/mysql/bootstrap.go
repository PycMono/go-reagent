package mysql

import (
	"github.com/PycMono/go-reagent/conversation"
	"go.uber.org/fx"
)

func newRegisteredStore(connection *Connection) conversation.Store {
	return NewStore(connection, connection)
}

// Module provides the optional MySQL connection and conversation Store.
var Module = fx.Options(
	fx.Provide(NewConnection),
	fx.Provide(newRegisteredStore),
)
