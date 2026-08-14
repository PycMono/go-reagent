package infrastructure

import (
	"github.com/PycMono/go-reagent/infrastructure/driver/mysql"
	"github.com/PycMono/go-reagent/infrastructure/persistence"
	"github.com/PycMono/go-reagent/infrastructure/serviceimpl"
	"go.uber.org/fx"
)

// Register registers infrastructure drivers and persistence adapters.
var Register = fx.Options(
	fx.Provide(mysql.NewProvider, mysql.NewTransactionManager),
	serviceimpl.Register,
	persistence.Register,
)
