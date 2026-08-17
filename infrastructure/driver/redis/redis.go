package redis

import (
	"context"
	"errors"

	"github.com/PycMono/go-cache-sdk/redis/connect"
	"github.com/PycMono/go-reagent/config"
	goredis "github.com/redis/go-redis/v9"
	"go.uber.org/fx"
)

const (
	appName    = "go-reagent"
	clientName = "cache"
)

type clientInitializer func(context.Context, *connect.Config) (goredis.UniversalClient, error)
type clientCloser func(string) error

func NewClient(cfg *config.Config) (goredis.UniversalClient, error) {
	return newClient(cfg, connect.InitClient)
}

func newClient(cfg *config.Config, initialize clientInitializer) (goredis.UniversalClient, error) {
	if cfg == nil || initialize == nil {
		return nil, errors.New("初始化 Redis Client 失败")
	}
	client, err := initialize(context.Background(), &connect.Config{
		AppName:    appName,
		ClientName: clientName,
		Addr:       append([]string(nil), cfg.Redis.Addr...),
		Password:   cfg.Redis.Password,
		DB:         cfg.Redis.DB,
		PoolSize:   cfg.Redis.PoolSize,
	})
	if err != nil || client == nil {
		return nil, errors.New("初始化 Redis Client 失败")
	}
	return client, nil
}

func RegisterLifecycle(lifecycle fx.Lifecycle, _ goredis.UniversalClient) {
	registerLifecycle(lifecycle, connect.CloseClient)
}

func registerLifecycle(lifecycle fx.Lifecycle, closeClient clientCloser) {
	lifecycle.Append(fx.Hook{OnStop: func(context.Context) error {
		return closeClient(clientName)
	}})
}
