package infrastructure

import (
	"github.com/PycMono/go-reagent/config"
	"github.com/PycMono/go-reagent/domain/repository"
	"github.com/PycMono/go-reagent/infrastructure/controller"
	"github.com/PycMono/go-reagent/infrastructure/driver/gingext"
	"github.com/PycMono/go-reagent/infrastructure/driver/mysql"
	"github.com/PycMono/go-reagent/infrastructure/driver/observability"
	redisdriver "github.com/PycMono/go-reagent/infrastructure/driver/redis"
	"github.com/PycMono/go-reagent/infrastructure/persistence"
	"github.com/PycMono/go-reagent/infrastructure/serviceimpl"
	"go.uber.org/fx"
)

// Register registers infrastructure drivers, persistence adapters, and Web
// controllers.
var Register = fx.Options(
	fx.Provide(mysql.NewProvider, mysql.NewTransactionManager),
	fx.Provide(func(cfg *config.Config) repository.IIDService {
		return serviceimpl.NewIDService(int64(cfg.SnowflakeNodeID))
	}),
	redisdriver.Register,
	persistence.Register,
	observability.Register,
	controller.Register,
	gingext.Register,
)
