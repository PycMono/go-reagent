package infrastructure

import (
	"github.com/PycMono/go-reagent/infrastructure/controller"
	"github.com/PycMono/go-reagent/infrastructure/driver/gingext"
	"github.com/PycMono/go-reagent/infrastructure/driver/mysql"
	"github.com/PycMono/go-reagent/infrastructure/persistence"
	"github.com/PycMono/go-reagent/infrastructure/serviceimpl"
	"go.uber.org/fx"
)

// WebRegister extends the persistence graph with the local Gin server.
// The CLI keeps using Register and therefore does not acquire an HTTP lifecycle.
var WebRegister = fx.Options(
	Register,
	controller.Register,
	fx.Provide(gingext.NewEngine, gingext.NewHTTPServer),
	fx.Invoke(gingext.RegisterLifecycle),
)

// Register registers infrastructure drivers and persistence adapters.
var Register = fx.Options(
	fx.Provide(mysql.NewProvider, mysql.NewTransactionManager),
	serviceimpl.Register,
	persistence.Register,
)
