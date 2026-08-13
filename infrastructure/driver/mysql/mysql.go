package mysql

import (
	"context"
	"errors"

	sqlsdk "github.com/PycMono/go-mysql-sdk"
	"github.com/PycMono/go-mysql-sdk/transaction"
	"github.com/PycMono/go-reagent/config"
	"gorm.io/gorm"
)

var ErrDisabled = errors.New("mysql conversation persistence is disabled")

type transProvider interface {
	sqlsdk.Provider
	transaction.Manager
}

type opener func(*sqlsdk.Options) transProvider

// NewProvider initializes the MySQL provider used by persistence repositories.
func NewProvider(cfg *config.Config) (sqlsdk.Provider, error) {
	return newProvider(cfg, func(options *sqlsdk.Options) transProvider {
		return sqlsdk.NewTransProvider(options)
	})
}

func newProvider(cfg *config.Config, open opener) (provider sqlsdk.Provider, err error) {
	if cfg == nil {
		return nil, errors.New("初始化 MySQL Provider 失败: 配置不能为空")
	}
	if !cfg.Conversation.Enabled {
		return disabledProvider{}, nil
	}
	if open == nil {
		return nil, errors.New("初始化 MySQL Provider 失败")
	}
	defer func() {
		if recover() != nil {
			provider = nil
			err = errors.New("初始化 MySQL Provider 失败")
		}
	}()
	provider = open(toSDKOptions(cfg.MySQL))
	if provider == nil {
		return nil, errors.New("初始化 MySQL Provider 失败")
	}
	return provider, nil
}

func toSDKOptions(cfg config.MySQLConfig) *sqlsdk.Options {
	return &sqlsdk.Options{
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

// NewTransactionManager exposes the transaction capability of NewProvider.
func NewTransactionManager(provider sqlsdk.Provider) (transaction.Manager, error) {
	manager, ok := provider.(transaction.Manager)
	if !ok {
		return nil, errors.New("初始化 MySQL 事务管理器失败")
	}
	return manager, nil
}

type disabledProvider struct{}

func (disabledProvider) UseDB(context.Context) *gorm.DB { return nil }
func (disabledProvider) Transaction(context.Context, func(context.Context) error) error {
	return ErrDisabled
}
func (disabledProvider) IsInTransaction(context.Context) bool         { return false }
func (disabledProvider) FindDB4TransContext(context.Context) *gorm.DB { return nil }
