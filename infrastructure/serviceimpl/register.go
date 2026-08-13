package serviceimpl

import (
	"github.com/PycMono/go-reagent/config"
	"github.com/PycMono/go-reagent/domain/repository"
	"go.uber.org/fx"
)

var Register = fx.Options(
	fx.Provide(func(cfg *config.Config) (repository.IIDService, error) {
		return NewIDService(int64(cfg.SnowflakeNodeID))
	}),
)
