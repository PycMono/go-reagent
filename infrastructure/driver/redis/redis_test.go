package redis

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/PycMono/go-cache-sdk/redis/connect"
	"github.com/PycMono/go-reagent/config"
	goredis "github.com/redis/go-redis/v9"
	"go.uber.org/fx"
)

func TestNewClientMapsReferenceConfiguration(t *testing.T) {
	cfg := &config.Config{Redis: config.RedisConfig{
		Addr: []string{"127.0.0.1:6379"}, Password: "secret", DB: 2, PoolSize: 5,
	}}
	var got connect.Config
	wantClient := goredis.NewUniversalClient(&goredis.UniversalOptions{Addrs: []string{"127.0.0.1:1"}})
	t.Cleanup(func() { _ = wantClient.Close() })

	client, err := newClient(cfg, func(_ context.Context, value *connect.Config) (goredis.UniversalClient, error) {
		got = *value
		got.Addr = append([]string(nil), value.Addr...)
		return wantClient, nil
	})
	if err != nil || client != wantClient {
		t.Fatalf("newClient() = %#v, %v", client, err)
	}
	if got.AppName != "go-reagent" || got.ClientName != "cache" ||
		!slices.Equal(got.Addr, cfg.Redis.Addr) || got.Password != "secret" || got.DB != 2 || got.PoolSize != 5 {
		t.Fatalf("connect.Config = %#v", got)
	}
}

func TestNewClientRejectsInvalidInputsWithoutLeakingCause(t *testing.T) {
	const secret = "never-print-redis-secret"
	tests := []struct {
		name string
		cfg  *config.Config
		init clientInitializer
	}{
		{name: "nil config", cfg: nil, init: connect.InitClient},
		{name: "nil initializer", cfg: &config.Config{}, init: nil},
		{name: "initializer failure", cfg: &config.Config{}, init: func(context.Context, *connect.Config) (goredis.UniversalClient, error) {
			return nil, errors.New(secret)
		}},
		{name: "nil client", cfg: &config.Config{}, init: func(context.Context, *connect.Config) (goredis.UniversalClient, error) {
			return nil, nil
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, err := newClient(test.cfg, test.init)
			if client != nil || err == nil || !strings.Contains(err.Error(), "初始化 Redis Client 失败") || strings.Contains(err.Error(), secret) {
				t.Fatalf("newClient() = %#v, %v", client, err)
			}
		})
	}
}

type recordingLifecycle struct {
	hooks []fx.Hook
}

func (lifecycle *recordingLifecycle) Append(hook fx.Hook) {
	lifecycle.hooks = append(lifecycle.hooks, hook)
}

func TestRegisterLifecycleClosesManagedClient(t *testing.T) {
	lifecycle := &recordingLifecycle{}
	wantErr := errors.New("close failed")
	calledWith := ""
	registerLifecycle(lifecycle, func(name string) error {
		calledWith = name
		return wantErr
	})
	if len(lifecycle.hooks) != 1 || lifecycle.hooks[0].OnStop == nil {
		t.Fatalf("hooks = %#v", lifecycle.hooks)
	}
	err := lifecycle.hooks[0].OnStop(context.Background())
	if !errors.Is(err, wantErr) || calledWith != "cache" {
		t.Fatalf("OnStop() = %v, client = %q", err, calledWith)
	}
}
