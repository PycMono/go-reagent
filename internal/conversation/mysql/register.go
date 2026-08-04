package mysql

import (
	"github.com/PycMono/go-reagent/internal/conversation"
	driver "github.com/PycMono/go-reagent/internal/driver/mysql"
	"go.uber.org/fx"
)

func newRegisteredStore(connection *driver.Connection) conversation.Store {
	return NewStore(connection, connection)
}

var Register = fx.Options(fx.Provide(newRegisteredStore))
