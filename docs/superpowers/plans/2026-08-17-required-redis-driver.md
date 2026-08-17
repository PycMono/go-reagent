# Required Redis Driver Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Redis a required go-reagent Web dependency using the same go-cache-sdk adapter pattern and configuration shape as micro-framework.

**Architecture:** Add a strict `RedisConfig`, then expose one `redis.UniversalClient` from `infrastructure/driver/redis`. An Fx invoke forces the otherwise lazy provider to connect and PING before HTTP startup, while an OnStop hook removes and closes the SDK-managed `cache` client.

**Tech Stack:** Go 1.26, Uber Fx 1.23, go-cache-sdk 1.0.3, go-redis/v9 9.19.0, Configor.

**Spec:** `docs/superpowers/specs/2026-08-17-required-redis-driver-design.md`

## Global Constraints

- Redis is required; do not add an `enabled` flag or a disabled client.
- Preserve the micro-framework keys `addr`, `password`, `db`, and `pool_size`.
- Use `AppName = "go-reagent"` and `ClientName = "cache"`.
- Keep `github.com/PycMono/go-cache-sdk` at `v1.0.3` and `github.com/redis/go-redis/v9` at `v9.19.0`.
- Do not add Session, cache repositories, rate limiting, queues, locks, or `/ready`.
- Never include Redis passwords in committed examples or error text.
- Preserve and never stage `pi/recovery.go` and `pi/test/recovery_test.go`.
- Modify ignored `config.json` locally, but never stage or commit it.

---

### Task 1: Required Redis Configuration

**Files:**
- Modify: `config/config.go`
- Modify: `config/validate.go`
- Modify: `config/config_test.go`
- Modify: `config.example.json`

**Interfaces:**
- Produces: `config.RedisConfig` and `Config.Redis`.
- Produces: `(*RedisConfig).normalizeAndValidate() error` called unconditionally by `Config.normalizeAndValidate`.
- Consumed by: Task 2 `redis.NewClient`.

- [ ] **Step 1: Write failing configuration tests**

Add a valid reusable JSON fragment and tests that prove parsing, normalization, strict absence handling, range validation, and password redaction:

```go
const validRedisJSON = `"redis":{"addr":["127.0.0.1:6379"],"password":"redis-secret","db":0,"pool_size":5}`

func TestLoadConfigParsesAndNormalizesRequiredRedis(t *testing.T) {
	path := writeConfig(t, `{
		"currentPlatform":"x",
		"platforms":[{"id":"x","protocol":"openai","baseURL":"https://x.test/","apiKey":"k","model":"m","pricing":{"input_usd_per_million_tokens":0,"output_usd_per_million_tokens":0}}],
		"redis":{"addr":[" 127.0.0.1:6379 "],"password":"redis-secret","db":2,"pool_size":5}
	}`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cfg.Redis.Addr, []string{"127.0.0.1:6379"}) || cfg.Redis.Password != "redis-secret" || cfg.Redis.DB != 2 || cfg.Redis.PoolSize != 5 {
		t.Fatalf("Redis = %#v", cfg.Redis)
	}
}

func TestLoadConfigRejectsInvalidRequiredRedisWithoutLeakingPassword(t *testing.T) {
	const credential = "never-print-redis-password"
	tests := []struct {
		name  string
		redis string
		want  string
	}{
		{name: "missing", redis: `{}`, want: "redis.addr"},
		{name: "empty address", redis: `{"addr":["  "],"password":"` + credential + `","db":0,"pool_size":5}`, want: "redis.addr"},
		{name: "negative db", redis: `{"addr":["127.0.0.1:6379"],"password":"` + credential + `","db":-1,"pool_size":5}`, want: "redis.db"},
		{name: "zero pool", redis: `{"addr":["127.0.0.1:6379"],"password":"` + credential + `","db":0,"pool_size":0}`, want: "redis.pool_size"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document := `{"currentPlatform":"x","platforms":[{"id":"x","protocol":"openai","baseURL":"https://x.test/","apiKey":"k","model":"m","pricing":{"input_usd_per_million_tokens":0,"output_usd_per_million_tokens":0}}],"redis":` + test.redis + `}`
			_, err := Load(writeConfig(t, document))
			if err == nil || !strings.Contains(err.Error(), test.want) || strings.Contains(err.Error(), credential) {
				t.Fatalf("Load() error = %v, want %q without credential", err, test.want)
			}
		})
	}
}
```

Add `slices` to the test imports. Add valid Redis sections to every successful JSON, YAML, TOML, environment-overlay, example-fallback, and permissive-JSON fixture. Leave fixtures that intentionally fail earlier platform, HTTP, Bot, Conversation, or MySQL validation unchanged.

- [ ] **Step 2: Run the config tests to verify RED**

Run:

```bash
GOCACHE=/tmp/go-reagent-go-cache go test ./config
```

Expected: compilation fails because `Config.Redis` does not exist, proving the tests exercise the missing contract.

- [ ] **Step 3: Implement RedisConfig and validation**

Add to `Config`:

```go
Redis RedisConfig `json:"redis" yaml:"redis" toml:"redis"`
```

Add the type:

```go
type RedisConfig struct {
	Addr     []string `json:"addr" yaml:"addr" toml:"addr"`
	Password string   `json:"password" yaml:"password" toml:"password"`
	DB       int      `json:"db" yaml:"db" toml:"db"`
	PoolSize int      `json:"pool_size" yaml:"pool_size" toml:"pool_size"`
}
```

Call Redis validation after existing Conversation/MySQL validation so established invalid-field tests keep their current error priority:

```go
if err := config.Conversation.normalizeAndValidate(&config.MySQL); err != nil {
	return err
}
return config.Redis.normalizeAndValidate()
```

Implement:

```go
func (config *RedisConfig) normalizeAndValidate() error {
	if len(config.Addr) == 0 {
		return errors.New("redis.addr 不能为空")
	}
	for index := range config.Addr {
		config.Addr[index] = strings.TrimSpace(config.Addr[index])
		if config.Addr[index] == "" {
			return errors.New("redis.addr 不能包含空地址")
		}
	}
	if config.DB < 0 {
		return errors.New("redis.db 不能小于 0")
	}
	if config.PoolSize < 1 {
		return errors.New("redis.pool_size 必须大于 0")
	}
	return nil
}
```

Add the safe Redis example to `config.example.json` with an empty password.

- [ ] **Step 4: Run the config tests to verify GREEN**

Run:

```bash
gofmt -w config/config.go config/validate.go config/config_test.go
GOCACHE=/tmp/go-reagent-go-cache go test ./config
jq empty config.example.json
```

Expected: config tests pass and the example remains valid JSON.

- [ ] **Step 5: Commit the configuration contract**

```bash
git add config/config.go config/validate.go config/config_test.go config.example.json
git diff --cached --check
git commit -m "feat: require Redis configuration"
```

---

### Task 2: Redis SDK Driver and Lifecycle

**Files:**
- Create: `infrastructure/driver/redis/redis.go`
- Create: `infrastructure/driver/redis/redis_test.go`
- Create: `infrastructure/driver/redis/register.go`

**Interfaces:**
- Consumes: `config.Config.Redis` from Task 1.
- Produces: `NewClient(*config.Config) (goredis.UniversalClient, error)`.
- Produces: `RegisterLifecycle(fx.Lifecycle, goredis.UniversalClient)`.
- Produces: `redis.Register` for Task 3.

- [ ] **Step 1: Write failing Driver tests**

Create `redis_test.go` with tests for exact SDK mapping and stable errors. Use `goredis.NewUniversalClient` as a non-network fake return value:

```go
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
```

Add a lifecycle test using this recording `fx.Lifecycle` implementation. Inject a `clientCloser` callback into the internal helper and assert that the registered `OnStop` calls it with exactly `"cache"` and returns its error:

```go
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
```

- [ ] **Step 2: Run Driver tests to verify RED**

Run:

```bash
GOCACHE=/tmp/go-reagent-go-cache go test ./infrastructure/driver/redis
```

Expected: compilation fails because the package implementation and interfaces do not exist.

- [ ] **Step 3: Implement the Driver**

Create `redis.go`:

```go
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
		AppName: appName, ClientName: clientName,
		Addr: append([]string(nil), cfg.Redis.Addr...), Password: cfg.Redis.Password,
		DB: cfg.Redis.DB, PoolSize: cfg.Redis.PoolSize,
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
```

Create `register.go`:

```go
package redis

import "go.uber.org/fx"

var Register = fx.Options(
	fx.Provide(NewClient),
	fx.Invoke(RegisterLifecycle),
)
```

- [ ] **Step 4: Run Driver tests to verify GREEN**

Run:

```bash
gofmt -w infrastructure/driver/redis/redis.go infrastructure/driver/redis/redis_test.go infrastructure/driver/redis/register.go
GOCACHE=/tmp/go-reagent-go-cache go test ./infrastructure/driver/redis
```

Expected: all Redis Driver tests pass.

- [ ] **Step 5: Commit the Driver**

```bash
git add infrastructure/driver/redis/redis.go infrastructure/driver/redis/redis_test.go infrastructure/driver/redis/register.go
git diff --cached --check
git commit -m "feat: add required Redis driver"
```

---

### Task 3: Infrastructure Registration and Required Startup

**Files:**
- Modify: `infrastructure/register.go`
- Create: `infrastructure/register_test.go`
- Modify: `go.mod`
- Modify: `go.sum`

**Interfaces:**
- Consumes: `redis.Register` from Task 2.
- Produces: an `infrastructure.Register` graph that cannot start without a Redis client.

- [ ] **Step 1: Write a failing registration boundary test**

Create `infrastructure/register_test.go`. The test asks Go for the actual `cmd/server` dependency graph and requires the Redis Driver package, proving the production register path includes it without connecting to an external Redis during unit tests:

```go
package infrastructure_test

import (
	"os/exec"
	"strings"
	"testing"
)

const redisDriverPackage = "github.com/PycMono/go-reagent/infrastructure/driver/redis"

func TestServerDependsOnRequiredRedisDriver(t *testing.T) {
	command := exec.Command("go", "list", "-deps", "./cmd/server")
	command.Dir = ".."
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("go list cmd/server: %v: %s", err, strings.TrimSpace(string(output)))
	}
	for _, dependency := range strings.Fields(string(output)) {
		if dependency == redisDriverPackage {
			return
		}
	}
	t.Fatalf("cmd/server does not depend on required Redis Driver %s", redisDriverPackage)
}
```

- [ ] **Step 2: Run infrastructure tests to verify RED**

Run:

```bash
GOCACHE=/tmp/go-reagent-go-cache go test ./infrastructure ./infrastructure/driver/redis
```

Expected: `TestServerDependsOnRequiredRedisDriver` fails because `infrastructure.Register` does not include the Redis module.

- [ ] **Step 3: Register Redis and promote direct dependencies**

Update `infrastructure/register.go`:

```go
import (
	"github.com/PycMono/go-reagent/infrastructure/driver/mysql"
	redisdriver "github.com/PycMono/go-reagent/infrastructure/driver/redis"
	// existing imports
)

var Register = fx.Options(
	fx.Provide(mysql.NewProvider, mysql.NewTransactionManager),
	redisdriver.Register,
	serviceimpl.Register,
	persistence.Register,
)
```

Move these unchanged versions into the direct `require` block in `go.mod`:

```go
github.com/PycMono/go-cache-sdk v1.0.3
github.com/redis/go-redis/v9 v9.19.0
```

Run `go mod tidy` only after the source imports compile.

- [ ] **Step 4: Run registration and package tests to verify GREEN**

Run:

```bash
gofmt -w infrastructure/register.go infrastructure/register_test.go
go mod tidy
GOCACHE=/tmp/go-reagent-go-cache go test ./config ./infrastructure/... ./application/web ./cmd/server
```

Expected: all listed tests pass and Redis is instantiated by the Fx graph.

- [ ] **Step 5: Commit infrastructure wiring**

```bash
git add infrastructure/register.go infrastructure/register_test.go go.mod go.sum
git diff --cached --check
git commit -m "feat: require Redis at service startup"
```

---

### Task 4: Documentation, Local Configuration, and Full Verification

**Files:**
- Modify: `README.md`
- Modify: `docs/web-chat.md`
- Modify locally only: `config.json`

**Interfaces:**
- Documents: required Redis startup contract and secure local configuration.
- Produces locally: a runnable ignored `config.json` using `127.0.0.1:6379`, DB 0, pool size 5, and the user's local password.

- [ ] **Step 1: Update committed documentation**

Add the safe Redis block to README and Web Chat examples:

```json
"redis": {
  "addr": ["127.0.0.1:6379"],
  "password": "",
  "db": 0,
  "pool_size": 5
}
```

State that Redis is required, startup performs a connection/PING, and production credentials belong only in ignored config or environment overrides.

- [ ] **Step 2: Update ignored local config**

Add this block to local `config.json` without printing or staging the surrounding platform credentials:

```json
"redis": {
  "addr": ["127.0.0.1:6379"],
  "password": "123456",
  "db": 0,
  "pool_size": 5
}
```

Confirm `git check-ignore config.json` succeeds and `git status --short` does not list it.

- [ ] **Step 3: Verify docs and commit only committed files**

Run:

```bash
jq empty config.example.json config.json
git diff --check
git add README.md docs/web-chat.md
git diff --cached --name-only
git commit -m "docs: describe required Redis dependency"
```

Expected staged names are exactly `README.md` and `docs/web-chat.md`.

- [ ] **Step 4: Run full automated verification**

Run:

```bash
GOCACHE=/tmp/go-reagent-go-cache go test ./...
GOCACHE=/tmp/go-reagent-go-cache go test -race ./...
GOCACHE=/tmp/go-reagent-go-cache go build -o /tmp/go-reagent-server ./cmd/server
file /tmp/go-reagent-server
git diff --check
```

Expected: all tests pass, race reports no races, and the server binary is produced.

- [ ] **Step 5: Verify real startup behavior**

With the configured local Redis available, start the service using the ignored local config and verify `/health` responds. Then point a temporary config copy at an unavailable Redis port and verify Fx exits before the HTTP listener starts. Do not edit or expose real platform API keys while creating the temporary negative-path config.

- [ ] **Step 6: Confirm final workspace boundaries**

Run:

```bash
git status --short
git log --oneline -8
```

Expected: only the user's pre-existing `pi/recovery.go` and `pi/test/recovery_test.go` remain modified; `config.json` remains ignored; all Redis feature files are committed.
