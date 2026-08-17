# Required Redis Driver Design

## 目标

参考 `micro-framework` 的 Redis 配置和 Driver，为 go-reagent Web 服务增加必需的 Redis 基础设施能力。服务启动时必须建立 Redis 连接；配置无效或连接失败时，服务不得继续启动。

本设计只建立配置、连接和生命周期边界，不引入 Session、缓存 Repository、限流、分布式锁或业务键。

## 参考实现与取舍

参考实现位于：

- `/Users/allen/projects/work/github/micro-framework/infrastructure/config/config.go`
- `/Users/allen/projects/work/github/micro-framework/infrastructure/driver/redis/redis.go`
- `/Users/allen/projects/work/github/micro-framework/infrastructure/init.go`

go-reagent 继续采用参考实现使用的：

- `github.com/PycMono/go-cache-sdk/redis/connect`；
- `github.com/redis/go-redis/v9`.UniversalClient；
- `ClientName = "cache"`；
- `addr`、`password`、`db`、`pool_size` 配置字段。

参考实现没有清理 SDK 全局 Client Manager。go-reagent 增加 Fx 停止生命周期，在服务退出时调用 `connect.CloseClient("cache")`，避免测试、热重启和进程关闭期间遗留连接。

`connect.Config.AppName` 固定使用 `go-reagent`。当前 SDK 不使用该字段参与客户端标识，因此不为它增加无业务价值的顶层 `app` 配置。

## 配置契约

`config.Config` 增加：

```go
Redis RedisConfig `json:"redis" yaml:"redis" toml:"redis"`
```

配置类型为：

```go
type RedisConfig struct {
	Addr     []string `json:"addr" yaml:"addr" toml:"addr"`
	Password string   `json:"password" yaml:"password" toml:"password"`
	DB       int      `json:"db" yaml:"db" toml:"db"`
	PoolSize int      `json:"pool_size" yaml:"pool_size" toml:"pool_size"`
}
```

Redis 是无条件强依赖，不增加 `enabled` 开关。配置加载时执行以下标准化和校验：

1. `addr` 至少包含一个地址；
2. 每个地址去除首尾空格，去除后不得为空；
3. `db` 不得小于 0；
4. `pool_size` 必须大于 0；
5. `password` 允许为空，以支持无认证 Redis。

配置错误只指出字段，不回显 Redis 密码。

`config.example.json` 提供安全示例：

```json
"redis": {
  "addr": ["127.0.0.1:6379"],
  "password": "",
  "db": 0,
  "pool_size": 5
}
```

被 Git 忽略的本地 `config.json` 使用开发环境真实 Redis 配置，不得进入提交。

## Driver 边界

新增目录：

```text
infrastructure/driver/redis/
├── redis.go
├── redis_test.go
└── register.go
```

`NewClient` 接收 `*config.Config`，映射成：

```go
&connect.Config{
	AppName:    "go-reagent",
	ClientName: "cache",
	Addr:       cfg.Redis.Addr,
	Password:   cfg.Redis.Password,
	DB:         cfg.Redis.DB,
	PoolSize:   cfg.Redis.PoolSize,
}
```

生产路径调用 `connect.InitClient(context.Background(), redisConfig)`。该 SDK 会创建连接池并立即执行 `PING`，因此构造成功就代表 Redis 在启动阶段可用。

Driver 对初始化错误做稳定包装，不在错误文本中拼接配置、地址查询信息或密码。为了能在单元测试中验证参数映射和错误边界，内部构造函数接收一个与 `connect.InitClient` 同签名的初始化函数；公开构造函数只使用真实 SDK。

## Fx 装配与生命周期

`redis.Register` 包含：

```go
fx.Provide(NewClient)
fx.Invoke(RegisterLifecycle)
```

`RegisterLifecycle` 依赖 `redis.UniversalClient`，因此 Fx 启动图必须实例化 `NewClient`。不能只注册一个无人消费的惰性 Provider，否则服务会在 Redis 不可用时错误地启动成功。

启动行为：

```text
加载并校验配置
  -> 构造 Redis SDK 配置
  -> connect.InitClient
  -> PING 成功
  -> HTTP 服务启动
```

任一步失败都会使 Fx 启动失败。

停止行为：

```text
Fx OnStop
  -> connect.CloseClient("cache")
  -> 从 SDK 全局 clientMap/confMap 移除客户端
  -> 关闭连接池
```

`infrastructure.Register` 引入 `redis.Register`。业务层和 Domain 层不得直接依赖该 Driver；未来业务缓存应先定义 Repository 接口，再由 Infrastructure 实现。

## 依赖管理

因为生产代码将直接导入两个包，`go.mod` 中以下依赖从间接依赖提升为直接依赖：

- `github.com/PycMono/go-cache-sdk v1.0.3`；
- `github.com/redis/go-redis/v9 v9.19.0`。

不升级参考项目使用的版本，避免迁移同时引入 SDK 行为变化。

## 错误处理

配置错误示例：

```text
redis.addr 不能为空
redis.addr 不能包含空地址
redis.db 不能小于 0
redis.pool_size 必须大于 0
```

连接错误对外统一为：

```text
初始化 Redis Client 失败
```

错误文本不得包含密码。Redis 是强依赖，因此不提供 disabled client，也不降级为内存缓存。

## 测试设计

配置测试覆盖：

- JSON 配置能解析完整 Redis 字段；
- 地址首尾空格被去除；
- 缺少地址、空地址、负数 DB、非正连接池大小被拒绝；
- 错误文本不包含密码；
- `config.example.json` 继续能够通过完整配置加载。

Driver 测试覆盖：

- 配置准确映射到 `connect.Config`；
- 固定 `AppName = "go-reagent"` 和 `ClientName = "cache"`；
- nil 配置被拒绝；
- SDK 初始化失败时返回稳定、脱敏错误；
- Fx 注册会实际构造 Client，而不是保留惰性 Provider；
- Fx 停止时关闭并移除 `cache` Client。

验证顺序：

1. 聚焦运行 `config` 和 Redis Driver 测试；
2. 运行 `go test ./...`；
3. 运行 `go test -race ./...`；
4. 构建 `./cmd/server`；
5. 在本地 Redis 配置下启动服务，确认 Redis 不可用时启动失败、可用时 HTTP 服务正常监听。

## 文档与本地配置

更新 README 和 Web Chat 文档，明确：

- Redis 与 MySQL 一样是启动前置依赖；
- 默认开发地址为 `127.0.0.1:6379`；
- 密码只写入被 Git 忽略的 `config.json` 或环境覆盖，不写入示例与提交；
- `CONFIG_PATH` 行为保持不变。

本地 `config.json` 增加 Redis 段，但不暂存、不提交该文件。

## 非目标

本期不包含：

- Redis Session；
- Conversation 缓存；
- Skill 或模型结果缓存；
- 分布式锁、限流和消息队列；
- Redis 业务 Repository；
- `/ready` 或指标端点；
- 修改 `go-cache-sdk` 或 `go-gin-sdk` 上游实现。

## 验收标准

1. Redis 配置缺失或非法时，配置加载失败；
2. Redis 无法连接时，Web 服务启动失败且不会监听 HTTP 端口；
3. Redis 可连接时，Fx 图获得唯一的 `redis.UniversalClient`；
4. 服务停止时清理 `cache` Client；
5. 配置和运行错误不泄露 Redis 密码；
6. 现有聊天、MySQL 持久化、天气工具和前端测试不回归；
7. 用户原有的 `pi/recovery.go` 与 `pi/test/recovery_test.go` 修改不进入本功能提交。
