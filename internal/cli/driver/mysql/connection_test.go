package mysql

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	sqlsdk "github.com/PycMono/go-mysql-sdk"
	"github.com/PycMono/go-reagent"
	"go.uber.org/fx/fxtest"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func TestNewConnectionDoesNotOpenWhenPersistenceDisabled(t *testing.T) {
	lifecycle := fxtest.NewLifecycle(t)
	calls := 0
	connection, err := newConnection(lifecycle, &reagent.Config{}, func(*sqlsdk.Options) (sdkProvider, error) {
		calls++
		return &sdkProviderFake{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if connection == nil || calls != 0 {
		t.Fatalf("connection/opener calls = %#v, %d", connection, calls)
	}
	lifecycle.RequireStart()
	lifecycle.RequireStop()
}

func TestNewConnectionMapsExactSDKOptions(t *testing.T) {
	lifecycle := fxtest.NewLifecycle(t)
	cfg := &reagent.Config{
		Conversation: reagent.ConversationConfig{Enabled: true},
		MySQL: reagent.MySQLConfig{
			Host: "127.0.0.1", Port: 3306, Database: "biz", User: "root", Password: "123456",
			MaxOpen: 100, MaxIdle: 10, ConnLifetime: 3600, ConnTimeout: 3,
			LogLevel: 3, SlowThreshold: 500,
		},
	}
	var got sqlsdk.Options
	connection, err := newConnection(lifecycle, cfg, func(options *sqlsdk.Options) (sdkProvider, error) {
		got = *options
		return &sdkProviderFake{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if connection == nil || got.DB != "mysql" || got.Host != "127.0.0.1" || got.Port != 3306 ||
		got.Database != "biz" || got.User != "root" || got.Password != "123456" ||
		got.MaxOpen != 100 || got.MaxIdle != 10 || got.Lifetime != 3600 || got.Timeout != 3 ||
		got.LogLevel != 3 || got.SlowThreshold != 500 {
		t.Fatalf("SDK Options = %#v", got)
	}
}

func TestNewConnectionSanitizesOpenerFailure(t *testing.T) {
	const password = "never-print-mysql-password"
	lifecycle := fxtest.NewLifecycle(t)
	cfg := &reagent.Config{
		Conversation: reagent.ConversationConfig{Enabled: true},
		MySQL:        reagent.MySQLConfig{Password: password},
	}
	_, err := newConnection(lifecycle, cfg, func(*sqlsdk.Options) (sdkProvider, error) {
		return nil, fmt.Errorf("dial dsn with %s failed", password)
	})
	if err == nil || !strings.Contains(err.Error(), "初始化 MySQL 连接失败") || strings.Contains(err.Error(), password) || strings.Contains(err.Error(), "dsn") {
		t.Fatalf("newConnection() error = %v", err)
	}
}

func TestNewConnectionSanitizesOpenerPanic(t *testing.T) {
	const secret = "never-print-panic-secret"
	lifecycle := fxtest.NewLifecycle(t)
	_, err := newConnection(lifecycle, enabledConnectionConfig(), func(*sqlsdk.Options) (sdkProvider, error) {
		panic("driver panic with " + secret)
	})
	if err == nil || !strings.Contains(err.Error(), "初始化 MySQL 连接失败") || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "panic") {
		t.Fatalf("newConnection() error = %v", err)
	}
}

func TestConnectionLifecycleClosesSQLDatabase(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(gormmysql.New(gormmysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectClose()
	lifecycle := fxtest.NewLifecycle(t)
	connection, err := newConnection(lifecycle, enabledConnectionConfig(), func(*sqlsdk.Options) (sdkProvider, error) {
		return &sdkProviderFake{db: db}, nil
	})
	if err != nil || connection == nil {
		t.Fatalf("newConnection() = %#v, %v", connection, err)
	}
	lifecycle.RequireStart()
	lifecycle.RequireStop()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestConnectionLifecycleReturnsSafeCloseErrors(t *testing.T) {
	tests := []struct {
		name string
		db   *gorm.DB
	}{
		{name: "nil GORM database"},
		{name: "invalid SQL database", db: &gorm.DB{Config: &gorm.Config{}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lifecycle := fxtest.NewLifecycle(t)
			_, err := newConnection(lifecycle, enabledConnectionConfig(), func(*sqlsdk.Options) (sdkProvider, error) {
				return &sdkProviderFake{db: tt.db}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			lifecycle.RequireStart()
			if err := lifecycle.Stop(context.Background()); err == nil {
				t.Fatal("Stop() error = nil")
			}
		})
	}
}

func TestConnectionDisabledMethodsAreSafe(t *testing.T) {
	connection := &Connection{}
	if connection.UseDB(context.Background()) != nil {
		t.Fatal("UseDB() returned non-nil database")
	}
	if err := connection.Transaction(context.Background(), func(context.Context) error { return nil }); !errors.Is(err, ErrDisabled) {
		t.Fatalf("Transaction() error = %v", err)
	}
}

type sdkProviderFake struct {
	db *gorm.DB
}

func (f *sdkProviderFake) UseDB(context.Context) *gorm.DB { return f.db }
func (f *sdkProviderFake) Transaction(ctx context.Context, callback func(context.Context) error) error {
	return callback(ctx)
}
func (f *sdkProviderFake) IsInTransaction(context.Context) bool         { return false }
func (f *sdkProviderFake) FindDB4TransContext(context.Context) *gorm.DB { return nil }

func enabledConnectionConfig() *reagent.Config {
	return &reagent.Config{
		Conversation: reagent.ConversationConfig{Enabled: true},
		MySQL: reagent.MySQLConfig{
			Host: "127.0.0.1", Port: 3306, Database: "biz", User: "root", Password: "password",
			MaxOpen: 100, MaxIdle: 10, ConnLifetime: 3600, ConnTimeout: 3, LogLevel: 3, SlowThreshold: 500,
		},
	}
}
