package infrastructure

import (
	"github.com/PycMono/go-reagent/infrastructure/driver/mysql"
	redisdriver "github.com/PycMono/go-reagent/infrastructure/driver/redis"
	"github.com/PycMono/go-reagent/infrastructure/persistence"
	"github.com/PycMono/go-reagent/infrastructure/serviceimpl"
	"go.uber.org/fx"
)

// Register registers infrastructure drivers and persistence adapters.
var Register = fx.Options(
	fx.Provide(mysql.NewProvider, mysql.NewTransactionManager),
	redisdriver.Register,
	serviceimpl.Register,
	persistence.Register,
)
