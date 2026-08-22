# PycMono Observability SDK 设计

- **日期：** 2026-08-21
- **状态：** 待审核
- **目标仓库：** `/Users/allen/projects/work/github/sdk/go-observability-sdk`
- **模块路径：** `github.com/PycMono/go-observability-sdk`

## 1. 背景

PycMono 当前已经完成日志、Context 与 Gin 三个 SDK 的 OTel/W3C 终态设计：

- `go-logger-sdk` 从标准 OTel `SpanContext` 注入 `trace_id`、`span_id`。
- `go-context-sdk/tracing` 提供 W3C 传播、标准 Span 创建和属性补充能力，不装配 Provider 或 Exporter。
- `go-gin-sdk` 创建 HTTP SERVER Span 并回写 `trace-id`，不拥有应用级 OTel 生命周期。

各微服务仍缺少统一的 OTel SDK、OTLP Trace Exporter、Prometheus Metrics Exporter、Resource、Metrics Endpoint 和关闭流程。若由每个服务自行实现，会产生配置、命名、Label、Histogram Bucket、错误处理和生命周期差异。

同时，业务和框架需要一套低门槛的通用 Metrics API，不能要求每个调用点重复创建 OTel Instrument，也不能让不同服务各自定义 Counter、Timer、Gauge 的行为。

因此新增 `go-observability-sdk`，统一提供：

1. 通用 Metrics 埋点门面。
2. Metrics Manager、Context Label Injector 和默认实例。
3. OTel Metrics Adaptor 与 Instrument 缓存。
4. TracerProvider、MeterProvider、Exporter、Resource 与 W3C Propagator 装配。
5. 独立内部 Prometheus Metrics Server。
6. 可验证、可关闭、Disabled 时完全 Noop 的应用级生命周期。

## 2. 目标与非目标

### 2.1 目标

- 所有 PycMono 微服务复用同一套 Observability 基础设施。
- 业务使用 `Counter`、`Timer`、`Value`、`Emit` 等通用方法记录指标。
- 业务代码不依赖 Prometheus Client，不负责创建 OTel Provider 或 Exporter。
- 正式指标可以预先固定类型、单位、Label 和 Histogram Bucket。
- 自定义指标可以按统一规则懒创建，不能在后续调用中改变已冻结的结构。
- Trace 通过 OTLP/gRPC 导出；Metrics 通过 Prometheus 拉取。
- Metrics Histogram 默认通过当前 sampled Trace Context 产生 Exemplar。
- 配置或启动错误阻止服务启动；运行期遥测错误不得改变业务结果。
- 核心 SDK 不依赖 Gin、Fx、go-reagent 或任何业务 Domain。
- 未启用时不联网、不监听端口、不启动后台 goroutine，并安全空转。

### 2.2 非目标

- 不提供 Dashboard、Alert Rule、Collector、Tempo、Prometheus 或 Grafana 的部署系统。
- 不管理业务健康检查、Readiness、Liveness 或 pprof。
- 不采集 Prompt、模型响应、Tool 参数、Tool 输出或其他业务正文。
- 不把 `trace_id`、`span_id`、`user_id` 等高基数值写入 Metrics Label。
- 不替代 `go-context-sdk/tracing` 的 Span 和 W3C Context API。
- 不在核心模块中引入 Fx；服务自行用 Fx Hook 调用生命周期方法。
- 首版不提供 Agent、Model、Tool、MCP、订单等领域专属的强类型 Metrics API；领域指标由产生事实的模块集中定义。

## 3. 核心决策

1. **OTel 是唯一内部标准。** 通用 Metrics API 最终写入 OTel Meter；Prometheus 只是 Metrics 导出后端。
2. **保留通用埋点门面。** 业务不需要在每个调用点创建和保存 OTel Instrument，可直接使用统一的包级方法或注入的 Manager。
3. **正式指标显式定义。** Dashboard、告警和跨服务约定依赖的指标必须预先声明类型、单位、允许 Label 与 Bucket。
4. **自定义指标允许有界懒创建。** 未显式定义的指标可在首次合法记录时创建；首次调用冻结类型和 Label Key 集合，后续冲突记录被丢弃并限频告警；Runtime 同时限制懒创建 Instrument 总数，防止动态指标名造成无界增长。
5. **业务埋点 Fail-open。** `Counter`、`Timer` 等记录方法不返回错误；错误只进入 SDK ErrorHandler。构造和启动错误仍明确返回。
6. **私有 Prometheus Registry。** 每个 Runtime 使用自己的 Registry，不污染进程全局 Registry，不与测试或其他组件发生重复注册。
7. **Metrics 使用独立内部端口。** `/metrics` 不安装到业务 Gin Engine，也不进入业务 Trace 和 HTTP Metrics。
8. **全局 OTel 对象只安装一次。** Runtime 显式执行 `InstallGlobal`，向所有 SDK 和标准 OTel instrumentation 提供同一组 Provider 与 Propagator。
9. **核心不依赖 Fx。** Fx 只在服务组合层调用 `InstallGlobal`、`Start`、`Shutdown`。
10. **当前没有历史服务兼容约束。** 新仓库直接定义终态 API，不建设旧命名、旧行为或其他 Metrics Client 的兼容层。

## 4. 依赖版本

首版固定以下兼容组合：

| 依赖 | 版本 | 用途 |
|---|---:|---|
| Go | `1.25.0` | 与现有 SDK 一致 |
| `go.opentelemetry.io/otel` | `v1.45.0` | 全局 API、Resource、Propagator |
| `go.opentelemetry.io/otel/sdk` | `v1.45.0` | TracerProvider |
| `go.opentelemetry.io/otel/sdk/metric` | `v1.45.0` | MeterProvider、Reader、View、Exemplar |
| `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc` | `v1.45.0` | OTLP/gRPC Trace Exporter |
| `go.opentelemetry.io/otel/exporters/prometheus` | `v0.67.0` | Prometheus Metrics Exporter |
| `go.opentelemetry.io/contrib/instrumentation/runtime` | `v0.70.0` | 可选 Go Runtime Metrics |
| `github.com/prometheus/client_golang` | `v1.24.1` | 私有 Registry 与 `promhttp.HandlerFor` |

不得引入 Jaeger Client、OpenTracing、B3 Propagator 或独立 TraceID 生成器。

## 5. 包结构与依赖方向

```text
go-observability-sdk/
├── config.go
├── runtime.go
├── resource.go
├── tracing.go
├── metrics_server.go
├── option.go
├── errors.go
├── metrics/
│   ├── api.go
│   ├── label.go
│   ├── manager.go
│   ├── default.go
│   ├── definition.go
│   ├── noop.go
│   └── preset.go
└── internal/
    └── otelmetrics/
        ├── adaptor.go
        ├── instrument.go
        └── cache.go
```

职责：

- 根包 `observability`：配置、Resource、Provider、Exporter、Metrics Server 和生命周期。
- `observability/metrics`：业务和框架使用的公开 Metrics API。
- `internal/otelmetrics`：OTel Instrument 创建、缓存、结构冻结和记录实现。

依赖方向：

```text
业务 / Framework
→ observability/metrics
→ internal/otelmetrics
→ OTel Metric API
→ MeterProvider
→ Prometheus Exporter
```

业务代码不得 import `internal/otelmetrics`、OTel SDK 或 Prometheus Client。

## 6. 通用 Metrics API

### 6.1 Label

```go
package metrics

type Label = attribute.KeyValue

func Any(key string, value any) Label
func String(key, value string) Label
func Bool(key string, value bool) Label
func Int(key string, value int) Label
func Int64(key string, value int64) Label
func Float64(key string, value float64) Label
```

`Any` 仅支持可稳定映射为 Prometheus Label 的 OTel 标量类型：

```text
string, bool,
int, int8, int16, int32, int64,
float32, float64
```

空 Key、切片、对象或其他不支持的类型返回无效 Label。记录方法忽略无效 Label，不 panic，也不使用 `fmt.Sprint` 隐式序列化未知对象。数组属性可用于 Trace，但不得作为 Metrics Label。

### 6.2 Adaptor

```go
type Adaptor interface {
    Counter(
        ctx context.Context,
        name string,
        value float64,
        labels ...Label,
    )

    UpDownCounter(
        ctx context.Context,
        name string,
        value float64,
        labels ...Label,
    )

    Histogram(
        ctx context.Context,
        name string,
        value float64,
        labels ...Label,
    )

    Timer(
        ctx context.Context,
        name string,
        seconds float64,
        labels ...Label,
    )

    Value(
        ctx context.Context,
        name string,
        value float64,
        labels ...Label,
    )
}
```

映射关系：

| API | OTel Instrument | 默认 Unit |
|---|---|---|
| `Counter` | `Float64Counter` | `1` |
| `UpDownCounter` | `Float64UpDownCounter` | `1` |
| `Histogram` | `Float64Histogram` | `1` |
| `Timer` | `Float64Histogram` | `s` |
| `Value` | `Float64Gauge` | `1` |

`Counter` 和 `Timer` 拒绝负数、NaN 和 Inf；其他数值 Instrument 拒绝 NaN 和 Inf。`Histogram` 保留记录负数的能力，以支持温差、偏差等有符号分布。非法值不写入并触发限频告警。

### 6.3 Manager

```go
type Injector func(
    ctx context.Context,
    labels []Label,
) []Label

type Manager interface {
    Adaptor

    ErrCounter(
        ctx context.Context,
        name string,
        value float64,
        labels ...Label,
    )

    Emit(
        ctx context.Context,
        name string,
        seconds float64,
        err error,
        labels ...Label,
    )

    Wrap(
        ctx context.Context,
        name string,
        fn func() error,
        labels ...Label,
    ) error

    Defer(
        ctx context.Context,
        name string,
        labels ...Label,
    ) func(*error)
}

func NewManager(
    adaptor Adaptor,
    injectors ...Injector,
) Manager
```

组合方法行为：

- `ErrCounter(ctx, name, value)` 记录 `<name>.errors`。
- `Emit(ctx, name, seconds, err)` 总是记录 `<name>.requests` 与 `<name>.duration`；`err != nil` 时额外记录 `<name>.errors`。
- `Wrap` 测量函数耗时、调用 `Emit`，并将原始错误返回给调用方。
- `Wrap` 遇到 panic 时以非空错误状态记录一次，再原样重新 panic。
- `Defer` 用于 named return error；传入 nil error 指针时只记录请求量和耗时，不 panic。

`Wrap` 不得吞掉业务错误。`Defer` 是可选快捷方式，框架中优先使用已经持有开始时间和最终结果的 `Emit`。

### 6.4 默认实例与包级方法

```go
func Default() Manager
func SetDefault(manager Manager)

func Counter(ctx context.Context, name string, value float64, labels ...Label)
func UpDownCounter(ctx context.Context, name string, value float64, labels ...Label)
func Histogram(ctx context.Context, name string, value float64, labels ...Label)
func Timer(ctx context.Context, name string, seconds float64, labels ...Label)
func Value(ctx context.Context, name string, value float64, labels ...Label)
func ErrCounter(ctx context.Context, name string, value float64, labels ...Label)
func Emit(ctx context.Context, name string, seconds float64, err error, labels ...Label)
func Wrap(ctx context.Context, name string, fn func() error, labels ...Label) error
func Defer(ctx context.Context, name string, labels ...Label) func(*error)
```

约束：

- 默认实例初始为 Noop Manager。
- `SetDefault(nil)` 恢复为 Noop Manager。
- 默认实例通过原子 holder 存取，允许并发记录与 Runtime 安装/关闭。
- 包级方法每次读取当前默认 Manager，不缓存旧 Manager。
- Runtime 安装前和关闭后调用均安全空转。

### 6.5 使用示例

```go
metrics.Counter(
    ctx,
    "reagent.model.requests",
    1,
    metrics.String("provider", "deepseek"),
    metrics.String("model", "deepseek-chat"),
    metrics.String("outcome", "success"),
)

metrics.Timer(
    ctx,
    "reagent.model.duration",
    elapsed.Seconds(),
    metrics.String("provider", "deepseek"),
    metrics.String("model", "deepseek-chat"),
    metrics.String("outcome", "success"),
)
```

使用组合方法：

```go
err := metrics.Wrap(
    ctx,
    "order.create",
    func() error {
        return service.Create(ctx, request)
    },
    metrics.String("channel", channel),
)
```

该调用产生：

```text
order.create.requests
order.create.errors（仅失败时递增）
order.create.duration
```

## 7. 指标命名、定义与缓存

### 7.1 命名

直接记录方法接收最终 OTel Instrument 名称：

```go
metrics.Counter(ctx, "reagent.model.requests", 1)
metrics.Timer(ctx, "reagent.model.duration", elapsed.Seconds())
```

业务代码不追加 `_total`、`_seconds` 等 Prometheus 后缀。Exporter 固定使用 `UnderscoreEscapingWithSuffixes` 转换策略：

```text
reagent.model.requests + Counter(unit=1)
→ reagent_model_requests_total

reagent.model.duration + Histogram(unit=s)
→ reagent_model_duration_seconds
```

组合方法接收基础名称，并固定追加 `.requests`、`.errors`、`.duration`。

### 7.2 Definition

```go
type Kind string

const (
    KindCounter       Kind = "counter"
    KindUpDownCounter Kind = "up_down_counter"
    KindHistogram     Kind = "histogram"
    KindTimer         Kind = "timer"
    KindGauge         Kind = "gauge"
)

type Definition struct {
    Name        string
    Kind        Kind
    Description string
    Unit        string
    Labels      []string
    Buckets     []float64
}
```

Runtime 通过 Option 接收正式定义：

```go
runtime, err := observability.New(
    ctx,
    config,
    observability.WithMetricDefinitions(
        metrics.Definition{
            Name:        "reagent.model.duration",
            Kind:        metrics.KindTimer,
            Description: "Physical model request duration",
            Unit:        "s",
            Labels: []string{
                "provider",
                "model",
                "phase",
                "outcome",
            },
            Buckets: []float64{
                0.01, 0.025, 0.05, 0.1, 0.25,
                0.5, 1, 2.5, 5, 10, 30, 60,
            },
        },
    ),
)
```

Definition 在 `New` 阶段完成校验和去重：

- Name 必须符合 OTel Instrument 命名要求。
- Kind 必须属于公开枚举。
- Unit 为空时按 §6.2 的映射补为默认 Unit；归一化后再参与 Definition 去重和冲突判断。
- Timer 的 Unit 只能为 `s`；其他 Kind 不设置 Kind 专属 Unit 白名单，非空 Unit 必须为不超过 63 字节、无空白字符的可打印 ASCII 字符串，并应遵循 UCUM 大小写与语义约定。
- Histogram 和 Timer 必须提供至少一个 Bucket；其他 Kind 不允许提供 Bucket。Bucket 必须为有限数、严格递增且不重复。
- Label Key 必须非空、不重复且不属于默认禁止集合。
- 同名 Definition 完全相同时去重；任一结构不同则 `New` 返回错误。

### 7.3 正式指标

Dashboard、告警、容量评估和跨服务契约依赖的指标必须显式提供 Definition。显式 Definition 固定：

- Instrument 类型。
- Unit 和 Description。
- Histogram Bucket。
- 允许的 Label Key 集合。

允许的 Label 集合是上限白名单，不是必填集合。记录时缺少的允许 Label 不会补空值；OTel 数据点只携带实际存在的 Label，因此同一指标可以产生不同 Label 子集的 Series。该行为用于避免 SDK 伪造 `unknown` 或空字符串；Dashboard 和告警查询必须容忍 Label 缺失。额外 Label 被忽略并限频告警，避免在调用点意外扩大 Series。

### 7.4 懒创建指标

未显式定义的自定义指标允许在首次合法记录时懒创建：

- Name 使用与 Definition 相同的 OTel Instrument 名称校验：长度为 1–255 字节，以 ASCII 字母开头，其余字符只能为 `[A-Za-z0-9_.-/]`。
- Name 必须是调用点中的稳定常量或从有限注册表选择的值，不得拼接 UserID、RequestID、原始 Path、错误正文或其他运行期动态值。
- 首次调用冻结 Kind、Unit 和按 Key 排序后的 Label Key 集合。
- 后续调用缺少已冻结 Label 时允许记录，缺失 Label 不写入。
- 后续调用携带额外 Label 时忽略额外 Label并限频告警。
- 后续使用同名不同 Kind 时丢弃本次记录并限频告警。
- 同名 Instrument 的并发首次创建使用双重检查，只允许注册一次。
- 每个 Runtime 最多懒创建 `Metrics.MaxLazyInstruments` 个 Instrument；正式 Definition 和 Go Runtime Instrumentation 不计入该上限。
- 达到上限后，已有 Instrument 继续正常记录，新的懒创建 Name 被丢弃并按 Runtime 级固定错误分类限频告警。不得把被拒绝的原始 Name 放入限频 Key，避免限频器自身被动态名称打成无界缓存。

懒创建只用于未进入 Dashboard、告警或跨服务契约的服务私有指标。指标升级为正式能力时必须补充 Definition 和测试。

OTel SDK 的 Cardinality Limit 只限制单个 Instrument 在一个采集周期内的属性组合数量，不限制 Instrument 或指标名总数，不能替代上述懒创建上限。

### 7.5 默认禁止 Label

以下 Key 默认禁止作为 Metrics Label：

```text
trace_id, span_id, request_id, user_id,
run_id, conversation_id, session_id,
gen_ai.tool.call.id,
file.path, command, query,
prompt, request.body, response.body,
error.message, error.stack
```

禁止集合不可通过调用点解除。SDK 仅允许通过构造 Option 增加禁止项，不提供删除默认禁止项的能力。

### 7.6 通用技术 Definition

`metrics/preset.go` 只提供跨服务通用的技术指标定义，不包含业务 Domain：

```go
func HTTPServerDefinitions() []Definition

func OperationDefinitions(
    name string,
    labels []string,
    durationBuckets []float64,
) ([]Definition, error)
```

`HTTPServerDefinitions` 固定 HTTP SERVER 指标的类型、单位、Label 与 Bucket。Runtime 默认加载这组 Definition；未使用 HTTP Middleware 时只保留定义，不创建 Instrument 或产生 Series。

Preset Definition 与调用方 Definition 使用 §7.2 的同一冲突规则：同名且归一化后完全相同则去重，任一结构不同则 `New` 返回错误。首版不提供覆盖 Preset Definition 的 Option。

`OperationDefinitions` 为通用 `Emit/Wrap/Defer` 生成：

```text
<name>.requests  Counter(unit=1)
<name>.errors    Counter(unit=1)
<name>.duration  Timer(unit=s)
```

调用方仍需在 `New` 时通过 `WithMetricDefinitions` 注册返回值。空名称、非法 Label 或非法 Bucket 直接返回错误。

### 7.7 错误分类 Label 命名

- OTel 标准技术指标遵循对应语义约定，例如 HTTP SERVER 指标使用 `error.type`。
- 领域指标可以使用领域契约定义的稳定分类字段，例如 go-reagent Model 指标使用 `error_code`。
- `error.type` 与 `error_code` 不做全局互换；二者都只能记录稳定枚举或归一化值，不得记录错误正文、堆栈或动态消息。

## 8. Context Label Injector

Injector 用于把有限、低基数、跨调用点稳定的上下文值统一补充为 Label：

```go
func transportInjector(
    ctx context.Context,
    labels []metrics.Label,
) []metrics.Label {
    transport := transportFromContext(ctx)
    if transport == "" {
        return labels
    }
    return append(labels, metrics.String("transport", transport))
}
```

合并顺序：

1. 按注册顺序执行 Injector。
2. 合并调用点显式 Label。
3. 同名 Key 以调用点显式 Label 为准。
4. 删除无效和禁止 Label。
5. 应用 Definition 或已冻结结构的 Label 白名单。

Injector 在 Manager 创建时固定，运行期不可增删，避免数据结构和并发语义漂移。

`service.name`、`service.version`、`deployment.environment.name`、`service.instance.id` 等部署身份属于 OTel Resource，不通过 Injector 写入每条数据点。

## 9. Runtime 配置与公开 API

### 9.1 Config

```go
type Config struct {
    Enabled     bool
    ServiceName string
    Version     string
    Environment string
    InstanceID  string

    Tracing TracingConfig
    Metrics MetricsConfig
}

type TracingConfig struct {
    Enabled     bool
    Endpoint    string
    Insecure    bool
    Headers     map[string]string
    SampleRatio float64

    Timeout            time.Duration
    MaxQueueSize       int
    MaxExportBatchSize int
}

type MetricsConfig struct {
    Enabled bool

    Host string
    Port string
    Path string

    ReadHeaderTimeout  time.Duration
    WriteTimeout       time.Duration
    RuntimeMetrics     bool
    MaxLazyInstruments int
}
```

默认值：

```text
Enabled: false
Tracing.Enabled: false
Tracing.SampleRatio: 1.0
Tracing.Timeout: 5s
Tracing.MaxQueueSize: 2048
Tracing.MaxExportBatchSize: 512
Metrics.Enabled: false
Metrics.Host: 127.0.0.1
Metrics.Port: 9464
Metrics.Path: /metrics
Metrics.ReadHeaderTimeout: 5s
Metrics.WriteTimeout: 10s
Metrics.RuntimeMetrics: true
Metrics.MaxLazyInstruments: 256
```

开关组合语义：

| `Enabled` | `Tracing.Enabled` | `Metrics.Enabled` | 行为 |
|---|---|---|---|
| `false` | 任意 | 任意 | 返回完整 Noop Runtime；子开关和子配置一律忽略且不校验，不创建 Exporter、Provider 实现、Registry、Listener 或后台 goroutine |
| `true` | `false` | `false` | 创建可安装、可关闭的 Runtime，但 Trace 与 Metrics getter 均返回 Noop 实现，不创建 Exporter、Reader、Registry 或 Listener |
| `true` | `true` | `false` | 仅创建 Trace Provider/Exporter；Metrics getter 返回 Noop，`Metrics.RuntimeMetrics` 被忽略 |
| `true` | `false` | `true` | 仅创建 MeterProvider、Prometheus Exporter/Handler；TracerProvider getter 返回 Noop |
| `true` | `true` | `true` | 创建完整 Trace 与 Metrics 能力 |

`Enabled=true` 时只校验已启用子系统的专属配置；ServiceName 等共享 Resource 配置仍按公共规则校验。关闭的子系统不得因残留 Endpoint、Port 或其他字段阻止启动。

所有 Runtime getter 始终非 nil。被关闭的 Tracing/Metrics 子系统分别返回 Noop Provider/Manager；Metrics 未启用时 `MetricsHandler()` 返回固定 404 的无状态 Handler。`Enabled=false` 的 `InstallGlobal`、`Start`、`ForceFlush` 和 `Shutdown` 均直接空转，不获取全局 Runtime 所有权，也不修改进程当前的全局 OTel 对象或默认 Manager。

校验：

- `Enabled=true` 时 ServiceName 必填。
- `Tracing.Enabled=true` 时 Endpoint 必须为合法 OTLP/gRPC Target。
- SampleRatio 必须位于 `[0,1]`。
- Queue、Batch、Timeout 必须为正数，且 Batch 不大于 Queue。
- `Insecure=true` 只允许 Loopback Endpoint 或非生产 Environment。Environment 去除首尾空白并转为小写后，只有 `local`、`dev`、`development`、`test`、`testing`、`staging` 属于非生产；空值和其他值均按生产环境处理。
- `Metrics.Enabled=true` 时 Port 必须是十进制字符串，解析值位于 `1–65535`；保留 string 类型用于直接传入 `net.JoinHostPort` 和 `net.Listen`。
- `Metrics.Enabled=true` 时 `MaxLazyInstruments` 必须为正数；零值归一化为默认值 `256`，负数返回配置错误。
- Metrics Path 必须以 `/` 开头，不允许 Query、Fragment、通配或重复斜杠。
- Metrics Host 为空时不隐式扩展为公网监听；仍归一化为 `127.0.0.1`。
- OTLP Header 值不写入日志或错误详情。

### 9.2 Runtime API

```go
func New(
    ctx context.Context,
    config Config,
    options ...Option,
) (*Runtime, error)

func (r *Runtime) InstallGlobal() error
func (r *Runtime) Start(ctx context.Context) error
func (r *Runtime) ForceFlush(ctx context.Context) error
func (r *Runtime) Shutdown(ctx context.Context) error

func (r *Runtime) TracerProvider() trace.TracerProvider
func (r *Runtime) MeterProvider() metric.MeterProvider
func (r *Runtime) Propagator() propagation.TextMapPropagator
func (r *Runtime) MetricsHandler() http.Handler
func (r *Runtime) Metrics() metrics.Manager
```

Options：

```go
type ErrorHandler func(context.Context, error)

func WithErrorHandler(handler ErrorHandler) Option
func WithMetricDefinitions(definitions ...metrics.Definition) Option
func WithMetricsInjectors(injectors ...metrics.Injector) Option
func WithResource(resource *resource.Resource) Option
func WithSpanExporter(exporter sdktrace.SpanExporter) Option
func WithMetricReader(reader sdkmetric.Reader) Option
func WithMetricViews(views ...sdkmetric.View) Option
func WithMetricsHandler(handler http.Handler) Option
```

Exporter、Reader、Resource 和 Handler 注入主要用于测试、私有部署扩展和 In-Memory 验证；默认生产路径仍使用 Config 创建标准组件。

## 10. Runtime 生命周期

### 10.1 New

`New` 必须：

1. 归一化顶层 `Enabled`；为 `false` 时立即返回完整 Noop Runtime，跳过子配置、Options 和 Definition 校验。
2. `Enabled=true` 时归一化并校验启用子系统的 Config，并创建或合并 OTel Resource。
3. `Tracing.Enabled=true` 时创建 OTLP Trace Exporter、BatchSpanProcessor、TracerProvider 和 ParentBased Sampler；否则使用 Noop TracerProvider。
4. `Metrics.Enabled=true` 时创建私有 `prometheus.Registry`。
5. `Metrics.Enabled=true` 时使用私有 Registry 创建 OTel Prometheus Exporter。
6. `Metrics.Enabled=true` 时创建 MeterProvider，并使用默认 TraceBased Exemplar Filter；否则使用 Noop MeterProvider 和 Noop Manager。
7. `Metrics.Enabled=true && Metrics.RuntimeMetrics=true` 时，使用显式 MeterProvider 调用 OTel Runtime Instrumentation `runtime.Start` 注册 Observable Callback；注册失败则 `New` 返回错误。
8. `Metrics.Enabled=true` 时创建 OTel Metrics Adaptor、Manager 和 Metrics Handler。
9. `Metrics.Enabled=true` 时加载通用技术 Definition，再合并并校验调用方 Definition，但不提前创建未使用的 Instrument。
10. 不修改全局 OTel 对象，不监听端口；除 OTel Provider、Processor 和 Reader 自身的内部机制外，不启动 SDK 自有 goroutine。Metrics HTTP Server 的 goroutine 只能由 `Start` 创建。

`Enabled=false` 时返回功能完整的 Noop Runtime：所有 getter 非 nil，生命周期方法安全空转，不创建 Exporter、Listener 或后台 goroutine。

### 10.2 InstallGlobal

`InstallGlobal` 必须：

- `Enabled=false` 时直接空转，不获取全局 Runtime 所有权，也不修改任何全局对象。
- 安装 Runtime 的 TracerProvider。
- 安装 Runtime 的 MeterProvider。
- 安装唯一的 `propagation.TraceContext{}`。
- 将 Runtime Manager 安装为 `metrics.Default()`。
- 保存安装前的全局 Provider、Propagator 和 Manager，供测试及 Shutdown 恢复。

并发与重复调用：

- 同一 Runtime 重复调用幂等。
- 当前存在另一个未关闭 Runtime 时返回 `ErrGlobalAlreadyInstalled`。
- Shutdown 完成后允许新的 Runtime 安装。

### 10.3 Start

`Start` 仅负责进程内后台能力：

- Metrics 未启用时安全空转。
- Go Runtime Metrics 已在 `New` 阶段向显式 MeterProvider 注册；`Start` 不重复注册。
- 使用 `net.Listen` 同步绑定 Host 与 Port；端口冲突立即返回错误。
- 绑定成功后在内部 goroutine 调用 `http.Server.Serve`。
- 正常 `http.ErrServerClosed` 不上报为错误。
- 非正常 Serve 错误交给 ErrorHandler，不 panic。
- 同一 Runtime 重复 Start 幂等。

`Start` 要求已经成功执行 `InstallGlobal`；否则返回 `ErrGlobalNotInstalled`，避免 Provider 与 HTTP Endpoint 指向不同 Runtime。

### 10.4 ForceFlush

`ForceFlush`：

- 调用 TracerProvider `ForceFlush`。
- Prometheus Metrics 使用 Pull Reader，不执行伪造的 Push Flush。
- Tracing 未启用时安全空转。
- 尊重传入 Context 的 Deadline。

### 10.5 Shutdown

关闭顺序固定为：

```text
停止 Metrics HTTP Server
→ ForceFlush TracerProvider
→ Shutdown TracerProvider
→ Shutdown MeterProvider/Reader
→ 恢复先前的全局 Provider、Propagator 和 Metrics Manager
→ 释放全局 Runtime 所有权
```

要求：

- Shutdown 幂等。
- 多个错误用 `errors.Join` 返回。
- Context 超时后不得无限等待。
- Shutdown 后记录 Metrics 安全空转，不向已关闭 Provider 写入。
- OTel Runtime Instrumentation v0.70.0 的 `runtime.Start` 不返回独立 Stop Handle；其 Observable Callback 随 MeterProvider/Reader Shutdown 一并失效，不额外执行不存在的 Stop 操作。

## 11. Resource 与 Trace 装配

Resource 至少包含：

```text
service.name
service.version（非空时）
deployment.environment.name（非空时）
service.instance.id（非空时）
host.name
process.runtime.name = go
process.runtime.version
telemetry.sdk.language = go
telemetry.sdk.name = opentelemetry
telemetry.sdk.version
```

用户注入 Resource 与 SDK 默认 Resource 按 OTel `resource.Merge` 合并；Schema URL 冲突在 `New` 阶段返回错误。

Tracing：

- 使用 OTLP/gRPC Exporter。
- 使用 `sdktrace.NewBatchSpanProcessor`。
- Sampler 为 `ParentBased(TraceIDRatioBased(sample_ratio))`。
- W3C Propagator 只安装 `propagation.TraceContext{}`，不加入 Baggage 或 B3。
- Collector 不可达时使用 OTel 有界队列和 ErrorHandler，不影响业务返回。
- 公网入站 Trace Context 信任策略仍由网关或 HTTP Middleware 负责，本 SDK 不判断请求来源。

## 12. Prometheus Metrics Server

### 12.1 Registry 与 Exporter

- 每个 Runtime 创建独立 `prometheus.NewRegistry()`。
- OTel Exporter必须使用 `WithRegisterer(privateRegistry)`。
- 转换策略显式固定为 `UnderscoreEscapingWithSuffixes`，不依赖未来版本默认值。
- 保留 `target_info` 和 Instrumentation Scope 信息。
- 不注册 `prometheus.DefaultRegisterer` 中的 Collector。
- Go Runtime Metrics 通过 OTel Runtime Instrumentation 写入同一个 MeterProvider，不额外注册 Prometheus Go Collector，避免重复指标。
- Runtime Instrumentation 在 `New` 阶段使用 `runtime.WithMeterProvider(runtimeMeterProvider)` 注册 Observable Callback，不依赖尚未安装的全局 MeterProvider，也不自行启动常驻采集 goroutine。

### 12.2 Handler

默认 Handler 使用：

```go
promhttp.HandlerFor(
    privateRegistry,
    promhttp.HandlerOpts{
        ErrorHandling: promhttp.ContinueOnError,
    },
)
```

Metrics Server 使用私有 `http.ServeMux`：

- 仅配置的 Metrics Path 返回 Handler。
- 其他路径返回 404。
- 不暴露 Health、pprof 或业务路由。
- 不安装 Trace、Metrics、Logger 或鉴权 Middleware，避免自观测递归。
- 默认只监听 Loopback。
- 对集群网段开放由部署配置和 NetworkPolicy 控制。

### 12.3 Exemplar

MeterProvider 使用 OTel 默认 `TraceBasedFilter`。调用方必须把原始业务 Context 传给 `Counter`、`Timer` 等记录方法。当前 Context 含 sampled Span 时，Counter 与 Histogram 可导出带 `trace_id`、`span_id` 的 Prometheus Exemplar；TraceID 不成为普通 Label。

## 13. ErrorHandler 与限频

```go
type ErrorHandler func(context.Context, error)
```

默认实现使用标准库 `slog.Default()`，不依赖 `go-logger-sdk`，避免基础设施模块形成发布环。

错误分为两类：

### 13.1 启动错误

以下错误从 `New`、`InstallGlobal` 或 `Start` 明确返回并阻止服务启动：

- 配置非法。
- Resource 合并失败。
- Definition 冲突。
- Exporter、Provider 或 Reader 创建失败。
- 不安全的生产配置。
- Metrics 端口绑定失败。
- 不同 Runtime 重复安装。

### 13.2 运行期错误

以下错误 Fail-open，只调用 ErrorHandler：

- 指标名或数值非法。
- 额外或禁止 Label。
- 同名不同类型。
- Instrument 创建失败。
- OTLP Collector 暂时不可达。
- Metrics Server 异常退出。

已进入有界 Instrument Cache 的冲突，按“错误分类 + 指标名 + 冲突字段”组合限频，在一个窗口内最多记录一次，默认窗口 1 分钟。非法指标名、懒创建 Instrument 超限等发生在有界 Cache 之外的错误，只能使用 Runtime 级固定错误分类作为限频 Key，不得把原始 Name、Label Value 或其他调用方动态值写入限频器状态。错误日志不得包含 OTLP Header、业务正文、原始错误正文 Label 或完整 Label Value 集合。

## 14. 与现有 SDK 的边界

### 14.1 go-context-sdk

保持 API 层职责：

- `tracing.StartSpan` 创建标准 Span。
- `tracing.WithKV` 补充当前 Span 属性。
- `tracing.Extract/Inject` 使用全局 W3C Propagator。
- `TraceIDFromContext` 读取唯一技术关联 ID。

`go-context-sdk` 不依赖本 SDK，不创建 Provider、Exporter 或 Metrics Server。

### 14.2 go-logger-sdk

继续从标准 OTel SpanContext 注入 Trace 字段，不依赖本 SDK。Runtime 安装的全局 Provider使其自然获得有效上下文。

### 14.3 go-gin-sdk

go-gin-sdk v0.8 增加 `middleware.Metrics()`，只依赖 `github.com/PycMono/go-observability-sdk/metrics`，不依赖根包 Runtime、Prometheus Client 或 OTel SDK。

默认中间件顺序：

```text
CORS
→ Tracing
→ Metrics
→ Bizctx
→ Recovery
```

Metrics Middleware 自动记录标准 HTTP SERVER 指标：

```text
http.server.request.duration
http.server.request.body.size
http.server.response.body.size
http.server.active_requests
```

固定 Label：

| 指标 | Label |
|---|---|
| `http.server.request.duration` | `http.request.method`、`http.route`、`http.response.status_code`、`error.type` |
| `http.server.request.body.size` | `http.request.method`、`http.route`、`http.response.status_code`、`error.type` |
| `http.server.response.body.size` | `http.request.method`、`http.route`、`http.response.status_code`、`error.type` |
| `http.server.active_requests` | `http.request.method` |

规则：

- `http.route` 在 `c.Next()` 后读取 `c.FullPath()`，禁止使用原始 URL Path。
- 未匹配路由归一为 `unknown`。
- 5xx 使用稳定 `error.type`，不记录错误正文。
- 不记录 Query、User-Agent、IP、TraceID、请求体或响应体。
- `http.server.active_requests` 使用 UpDownCounter，开始 `+1`、结束 `-1`。
- Active Requests 的开始和结束使用完全相同的 Method Label，不在结束时追加 Route 或 Status。
- Request Body Size 只在实际读到字节数或可信 Content-Length 时记录；未知长度不伪造为 0。
- Response Body Size 只在 `c.Writer.Size() >= 0` 时记录。
- 健康检查是否计入通过 Filter Option 控制。
- Metrics Endpoint 使用独立端口，不经过 Gin。
- Tracing 位于 Metrics 之前，确保记录时 Context 中存在当前 SERVER Span，可生成 Exemplar。

## 15. go-reagent 接入

go-reagent 的 `infrastructure/observability` 改为配置和 Fx 组合层，不再自行创建 OTel Provider、Exporter 或 Metrics Server。

它负责：

- 将服务配置转换为 `observability.Config`。
- 传入 go-reagent 正式 Metric Definition。
- 创建 Runtime。
- 通过 Fx Hook 管理 Runtime 生命周期。

`pi/harness/observability` 继续拥有 Agent 领域指标语义，集中定义并记录：

- Agent Run。
- Model Request 与 Invocation。
- Token、Cost、TTFT。
- Retry、Context Overflow。
- Tool Execution 与 Queue。
- Compaction。

这些调用只依赖 `observability/metrics`，不 import Prometheus Client、OTel SDK 或基础设施配置。

go-reagent 的 HTTP SERVER Span 由自建 `gingext` Engine 安装；阶段 5 接入时必须在该 Engine 的实际中间件链中同时安装 `middleware.Metrics()`，不能只依赖 go-gin-sdk 默认 Engine 的中间件组装。

示例：

```go
metrics.Counter(
    ctx,
    "reagent.model.requests",
    1,
    metrics.String("provider", provider),
    metrics.String("model", model),
    metrics.String("phase", phase),
    metrics.String("outcome", outcome),
    metrics.String("error_code", errorCode),
)
```

Model、Tool、MCP 等公共强类型 Metrics Preset 只有在至少两个独立服务出现相同语义、字段和生命周期后才提升到 SDK；首版不提前固化单一业务实现。

## 16. Fx 接入方式

核心 SDK 不 import Fx。服务组合层使用：

```go
func RegisterLifecycle(
    lifecycle fx.Lifecycle,
    runtime *observability.Runtime,
) {
    lifecycle.Append(fx.Hook{
        OnStart: func(ctx context.Context) error {
            if err := runtime.InstallGlobal(); err != nil {
                return err
            }
            return runtime.Start(ctx)
        },
        // Shutdown 内部已先 ForceFlush Trace，再关闭 Provider；Hook 无需重复 Flush。
        OnStop: runtime.Shutdown,
    })
}
```

首版不在本模块增加 Fx 子包，确保 `go.mod` 不引入 Fx。若多个服务的组合代码未来出现真实分歧，再单独设计 Fx Adapter 模块。

## 17. 并发与状态机

Runtime 状态：

```text
created
→ installed
→ started
→ shutting_down
→ stopped
```

允许：

- `InstallGlobal` 在 installed/started 状态幂等。
- `Start` 在 started 状态幂等。
- `ForceFlush` 在 installed/started 状态执行。
- `Shutdown` 在任意状态可调用并幂等。

拒绝：

- 未 InstallGlobal 直接 Start。
- stopped 后重新 Start。
- 一个进程同时安装两个活跃 Runtime。

并发保障：

- Instrument Cache 使用 `sync.Map` 加创建锁双重检查。
- 同名 Instrument 只创建一次。
- Default Manager 使用原子 holder。
- Runtime 状态迁移使用互斥锁或等价原子状态机。
- Injector 和 Definition 在 New 后不可修改。
- 所有公共 API 必须通过 `go test -race ./...`。

## 18. 安全与基数

- Metrics 默认监听 `127.0.0.1`。
- 生产环境禁止不安全的远程 OTLP Endpoint。
- Secret 仅在运行时注入 OTLP Header，不进入可提交配置示例。
- ErrorHandler 不输出 Header、完整 Label Map 或业务正文。
- Metrics 不采集 Baggage。
- Route 必须使用模板，不使用动态 Path。
- Provider、Model、Tool、Phase、Outcome、ErrorCode 等 Label 必须来自配置、注册表或稳定枚举。
- 未知动态值映射为 `other` 或 `unknown`，不能直接产生无界 Series。
- 懒创建指标名必须来自源码常量或有限注册表，并受 `MaxLazyInstruments` 硬上限保护；不得依赖 OTel 单 Instrument Cardinality Limit 代替指标名上限。
- `trace_id` 只通过 Exemplar 关联 Trace，禁止作为普通 Label。

## 19. 测试与验收

### 19.1 Metrics API 单测

- Counter、UpDownCounter、Histogram、Timer、Value 正确映射。
- Any 支持类型、空 Key、不支持类型和无效 Label。
- Counter 和 Timer 的负数、NaN、Inf 被拒绝；Histogram 允许有限负数。
- ErrCounter、Emit 的名称和记录次数正确。
- Wrap 返回原始错误；panic 记录后重新 panic。
- Defer 处理 nil、成功和失败 named error。
- Noop Manager 所有方法不 panic。

### 19.2 Definition 与缓存

- 同名相同 Definition 去重。
- 同名不同 Kind、Unit、Label、Bucket 启动失败。
- Preset 与调用方同名 Definition 完全相同时去重、结构不同时启动失败，且不可覆盖 Preset。
- 空 Unit 按 Kind 补默认值；Timer 非 `s`、非法 Unit、Kind 与 Bucket 组合非法时启动失败。
- Bucket 非递增、重复、NaN、Inf 启动失败。
- 正式指标缺少允许 Label 时不补空值并正常记录，额外 Label 被忽略。
- 懒创建首次冻结结构。
- 懒创建名称执行 OTel 名称校验；达到 `MaxLazyInstruments` 后拒绝新名称但保留已有 Instrument。
- 并发触达懒创建上限时，成功创建数不超过上限且无计数穿透。
- 非法名称和超限错误使用固定的 Runtime 级限频 Key，连续动态名称不会扩大限频器状态。
- 并发首次记录只创建一个 Instrument。
- 同名不同类型运行期丢弃并限频告警。

### 19.3 Injector

- 多 Injector 顺序稳定。
- 调用点显式 Label 覆盖同名注入值。
- 禁止 Label 无法通过 Injector 加入。
- 并发调用无数据竞争。

### 19.4 Runtime

- Disabled 时无 Listener、Exporter 和后台 goroutine。
- Config 真值表覆盖全部开关组合；顶层 Disabled 忽略子配置，启用时只校验已启用子系统。
- Port 范围、非生产 Environment 白名单和 `MaxLazyInstruments` 默认值/非法值校验完整。
- Runtime Metrics 只在 Metrics 启用且开关打开时注册到 Runtime 自有 MeterProvider，并随 MeterProvider Shutdown 失效。
- InstallGlobal 正确安装并在 Shutdown 恢复。
- 不同 Runtime 并发安装被拒绝。
- Start 同步报告端口冲突。
- Shutdown 幂等并释放端口。
- 多错误使用 `errors.Join` 返回。
- OTLP Collector 不可达不影响业务函数结果。

### 19.5 Prometheus 集成

- 私有 Registry 不包含全局 Registry 的 Collector。
- `/metrics` 可抓取，其他路径返回 404。
- 名称、Unit 后缀和 Counter `_total` 符合固定转换策略。
- Histogram Bucket 与 Definition 一致。
- Resource 通过 `target_info` 暴露。
- sampled Span Context 产生包含 TraceID/SpanID 的 Exemplar。
- 非 sampled 或无 Span Context 时正常记录且不产生伪造 Exemplar。

### 19.6 go-gin-sdk 集成

- 请求量由 Duration Histogram Count 正确表达。
- Active Requests 成对增减，panic 和 abort 路径不泄漏。
- 路由 Label 使用模板；动态 Path 不增加 Series。
- 404 使用 `unknown`。
- 5xx 不泄露错误正文。
- 当前 SERVER Span 可关联 Histogram Exemplar。

### 19.7 go-reagent 集成

- P0 Agent、Model、Token、Cost、Retry、Tool、Compaction 指标可抓取。
- Label 与主观测设计一致且无禁止字段。
- Metrics、RunTotals 与 Ledger 在同一 Fixture 下对账。
- 自建 `gingext` Engine 的实际中间件链已安装 `middleware.Metrics()`，HTTP SERVER 指标可抓取。
- Metrics Disabled 不改变 Agent、Provider、Stream 或 Tool 的业务结果。
- 全工作区 `go test -count=1 -race ./...` 通过。

## 20. 实施顺序

| 阶段 | 交付 | 验收 |
|---|---|---|
| 1. Metrics Core | Label、Adaptor、Manager、Noop、默认实例、Definition、Instrument Cache | Metrics API、冲突、Injector、并发测试通过 |
| 2. Runtime | Config、Resource、TracerProvider、MeterProvider、OTLP 与 Prometheus Exporter | Disabled、配置、Provider、Exporter 测试通过 |
| 3. Metrics Server | 私有 Registry、独立 HTTP Server、Start/Shutdown | 抓取、端口冲突、关闭与隔离测试通过 |
| 4. Gin Metrics | go-gin-sdk v0.8 `middleware.Metrics()` | 路由模板、状态、大小、Active、Exemplar 测试通过 |
| 5. go-reagent | Runtime Fx 装配、正式 Definition、P0 Metrics | Trace、Metrics、Logs、Ledger 集成验收通过 |
| 6. 其他服务 | 统一 Config 和生命周期接入 | 服务不再自建 Provider、Exporter 或 Metrics Server |

## 21. 文档同步要求

实施前后必须同步：

1. `2026-08-20-agent-tracing-observability-design.md`
   - `infrastructure/observability` 从“自行创建 Provider、Exporter、Metrics Listener”改为“配置并装配 go-observability-sdk Runtime”。
   - Metrics 业务语义、Label 和 Bucket 继续由 go-reagent 定义。
2. `2026-08-21-pycmono-sdk-otel-migration-design.md`
   - “各应用自行装配标准 OTel Provider”更新为“由 go-observability-sdk 提供统一装配”。
   - `go-context-sdk`、`go-logger-sdk`、`go-gin-sdk` 仍只依赖标准 OTel Context/API 边界。
3. `go-observability-sdk/README.md`
   - 配置、Fx 接入、通用 Metrics API、Definition、Disabled、Metrics Endpoint 和 Shutdown 示例。
4. `go-gin-sdk/README.md`
   - Metrics Middleware、默认顺序、HTTP 指标和 Label 基数说明。

## 22. 正式验收标准

| 编号 | 要求 | 验收 |
|---|---|---|
| OBS-SDK-001 | 提供统一 Counter、UpDownCounter、Histogram、Timer、Value、ErrCounter、Emit、Wrap、Defer API | 单测验证名称、类型、单位、错误与 Noop 行为 |
| OBS-SDK-002 | 正式 Definition 固定 Kind、Unit、Label 和 Bucket | Unit 默认值、Unit/Kind、Bucket/Kind、Preset 冲突在启动期验证；额外 Label 运行期被忽略并告警 |
| OBS-SDK-003 | Runtime 统一安装 OTel Provider、OTLP Trace Exporter、Prometheus Exporter和 W3C Propagator | Trace 与 Metrics 使用同一 Resource；全局对象唯一 |
| OBS-SDK-004 | Metrics 使用独立内部端口和私有 Registry | `/metrics` 可抓取，其他路径 404，无全局 Collector 污染 |
| OBS-SDK-005 | 当前 sampled Trace Context 可通过 Exemplar 关联 Metrics | Histogram/Counter Exemplar 包含同一 TraceID/SpanID，普通 Label 无 TraceID |
| OBS-SDK-006 | Disabled 和运行期故障不改变业务结果 | 开关真值表、无网络、Listener、后台 goroutine；Exporter 故障 Fail-open |
| OBS-SDK-007 | 生命周期确定且可测试 | Install/Start/Flush/Shutdown 状态、幂等、端口释放、全局恢复通过 |
| OBS-SDK-008 | 核心 SDK 不依赖 Gin、Fx 或业务 Domain | `go mod graph` 与 Package Boundary Test 通过 |
| OBS-SDK-009 | go-gin-sdk 自动记录低基数 HTTP Metrics | 路由模板、状态、大小、Active、Filter 和 Exemplar 测试通过 |
| OBS-SDK-010 | go-reagent 通过通用 API 完成 P0 Metrics | 指标抓取、Label、Bucket、对账和 Race 验收通过 |
| OBS-SDK-011 | 懒创建指标名称合法且总量有界 | 动态名称规则、名称校验、`MaxLazyInstruments` 上限、并发边界和限频告警测试通过 |
