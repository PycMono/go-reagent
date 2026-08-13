package mysql

import (
	"context"
	"errors"
	"strings"
	"testing"

	sqlsdk "github.com/PycMono/go-mysql-sdk"
	"github.com/PycMono/go-reagent/config"
	"gorm.io/gorm"
)

func TestNewProviderDoesNotOpenWhenPersistenceDisabled(t *testing.T) {
	calls := 0
	provider, err := newProvider(&config.Config{}, func(*sqlsdk.Options) transProvider {
		calls++
		return &providerFake{}
	})
	if err != nil {
		t.Fatal(err)
	}
	if provider == nil || calls != 0 || provider.UseDB(context.Background()) != nil {
		t.Fatalf("provider/opener calls = %#v, %d", provider, calls)
	}
	manager, err := NewTransactionManager(provider)
	if err != nil || !errors.Is(manager.Transaction(context.Background(), func(context.Context) error { return nil }), ErrDisabled) {
		t.Fatalf("disabled transaction manager = %#v, %v", manager, err)
	}
}

func TestNewProviderMapsReferenceSDKOptions(t *testing.T) {
	cfg := enabledProviderConfig()
	var got sqlsdk.Options
	provider, err := newProvider(cfg, func(options *sqlsdk.Options) transProvider {
		got = *options
		return &providerFake{}
	})
	if err != nil || provider == nil {
		t.Fatalf("newProvider() = %#v, %v", provider, err)
	}
	if got.Host != "127.0.0.1" || got.Port != 3306 || got.Database != "biz" ||
		got.User != "root" || got.Password != "123456" || got.MaxOpen != 100 ||
		got.MaxIdle != 10 || got.Lifetime != 3600 || got.Timeout != 3 ||
		got.LogLevel != 3 || got.SlowThreshold != 500 {
		t.Fatalf("SDK Options = %#v", got)
	}
}

func TestNewProviderSanitizesOpenerPanic(t *testing.T) {
	const secret = "never-print-panic-secret"
	_, err := newProvider(enabledProviderConfig(), func(*sqlsdk.Options) transProvider {
		panic("driver panic with " + secret)
	})
	if err == nil || !strings.Contains(err.Error(), "初始化 MySQL Provider 失败") || strings.Contains(err.Error(), secret) {
		t.Fatalf("newProvider() error = %v", err)
	}
}

type providerFake struct{ db *gorm.DB }

func (provider *providerFake) UseDB(context.Context) *gorm.DB { return provider.db }
func (provider *providerFake) Transaction(ctx context.Context, callback func(context.Context) error) error {
	return callback(ctx)
}
func (provider *providerFake) IsInTransaction(context.Context) bool         { return false }
func (provider *providerFake) FindDB4TransContext(context.Context) *gorm.DB { return nil }

func enabledProviderConfig() *config.Config {
	return &config.Config{
		Conversation: config.ConversationConfig{Enabled: true},
		MySQL: config.MySQLConfig{
			Host: "127.0.0.1", Port: 3306, Database: "biz", User: "root", Password: "123456",
			MaxOpen: 100, MaxIdle: 10, ConnLifetime: 3600, ConnTimeout: 3, LogLevel: 3, SlowThreshold: 500,
		},
	}
}
