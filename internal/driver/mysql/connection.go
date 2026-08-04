package mysql

import (
	"context"
	"errors"
	"fmt"

	sqlsdk "github.com/PycMono/go-mysql-sdk"
	"github.com/PycMono/go-mysql-sdk/transaction"
	"github.com/PycMono/go-reagent/internal/config"
	"go.uber.org/fx"
	"gorm.io/gorm"
)

var ErrDisabled = errors.New("mysql conversation persistence is disabled")

type sdkProvider interface {
	sqlsdk.Provider
	transaction.Manager
}

type opener func(*sqlsdk.Options) (sdkProvider, error)

type Connection struct {
	provider sdkProvider
}

func NewConnection(lifecycle fx.Lifecycle, cfg *config.Config) (*Connection, error) {
	return newConnection(lifecycle, cfg, openSDKProvider)
}

func newConnection(lifecycle fx.Lifecycle, cfg *config.Config, open opener) (*Connection, error) {
	if cfg == nil {
		return nil, errors.New("初始化 MySQL 连接失败: 配置不能为空")
	}
	connection := &Connection{}
	if !cfg.Conversation.Enabled {
		return connection, nil
	}
	if lifecycle == nil || open == nil {
		return nil, errors.New("初始化 MySQL 连接失败")
	}

	provider, err := callOpenerSafely(open, toSDKOptions(cfg.MySQL))
	if err != nil || provider == nil {
		return nil, errors.New("初始化 MySQL 连接失败")
	}
	connection.provider = provider
	lifecycle.Append(fx.Hook{OnStop: connection.close})
	return connection, nil
}

func toSDKOptions(cfg config.MySQLConfig) *sqlsdk.Options {
	return &sqlsdk.Options{
		DB:            "mysql",
		Host:          cfg.Host,
		Port:          cfg.Port,
		Database:      cfg.Database,
		User:          cfg.User,
		Password:      cfg.Password,
		Timeout:       cfg.ConnTimeout,
		MaxOpen:       cfg.MaxOpen,
		MaxIdle:       cfg.MaxIdle,
		Lifetime:      cfg.ConnLifetime,
		LogLevel:      cfg.LogLevel,
		SlowThreshold: cfg.SlowThreshold,
	}
}

func openSDKProvider(options *sqlsdk.Options) (provider sdkProvider, err error) {
	defer func() {
		if recover() != nil {
			provider = nil
			err = errors.New("初始化 MySQL 连接失败")
		}
	}()
	return sqlsdk.NewTransProvider(options), nil
}

func callOpenerSafely(open opener, options *sqlsdk.Options) (provider sdkProvider, err error) {
	defer func() {
		if recover() != nil {
			provider = nil
			err = errors.New("初始化 MySQL 连接失败")
		}
	}()
	return open(options)
}

func (c *Connection) UseDB(ctx context.Context) *gorm.DB {
	if c == nil || c.provider == nil {
		return nil
	}
	return c.provider.UseDB(ctx)
}

func (c *Connection) Transaction(ctx context.Context, callback func(context.Context) error) error {
	if c == nil || c.provider == nil {
		return ErrDisabled
	}
	return c.provider.Transaction(ctx, callback)
}

func (c *Connection) close(ctx context.Context) error {
	db := c.UseDB(ctx)
	if db == nil {
		return errors.New("关闭 MySQL 连接失败: 数据库不可用")
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("关闭 MySQL 连接失败: %w", err)
	}
	if err := sqlDB.Close(); err != nil {
		return fmt.Errorf("关闭 MySQL 连接失败: %w", err)
	}
	return nil
}
