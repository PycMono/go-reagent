package mysql

import (
	"github.com/PycMono/go-reagent/internal/cli/conversation"
	driver "github.com/PycMono/go-reagent/internal/cli/driver/mysql"
	"go.uber.org/fx"
)

func newRegisteredStore(connection *driver.Connection) conversation.Store {
	return NewStore(connection, connection)
}

var Module = fx.Options(fx.Provide(newRegisteredStore))
