# PycMono SDK OTel 终态设计（go-logger-sdk / go-context-sdk / go-gin-sdk）

> 状态：已确认（Approved）
>
> 更新时间：2026-08-21
>
> 适用范围：go-logger-sdk v1.1.0、go-context-sdk v1.2.0、go-gin-sdk v0.7.0 的文件级改造规格。
>
> 关联文档：[Agent Tracing 与成本可观测性设计](./2026-08-20-agent-tracing-observability-design.md)（第 16 章定义跨服务契约，本文档定义 SDK 终态实现）。

## 1. 背景与目标

### 1.1 代码位置

| SDK | Go Module（权威标识） | 本地仓库路径 |
|---|---|---|
| go-logger-sdk | `github.com/PycMono/go-logger-sdk` | `/Users/allen/projects/work/github/sdk/go-logger-sdk` |
| go-context-sdk | `github.com/PycMono/go-context-sdk` | `/Users/allen/projects/work/github/sdk/go-context-sdk` |
| go-gin-sdk | `github.com/PycMono/go-gin-sdk` | `/Users/allen/projects/work/github/sdk/go-gin-sdk` |

本文档中的所有文件名均相对于对应仓库根目录（如 `field.go` 指 `<repo>/field.go`，`tracing/span.go` 指 `<repo>/tracing/span.go`）。本地路径为开发机检出位置，其他环境以 Go Module 路径为准。

### 1.2 当前实现与终态差距

| SDK | 现状问题 |
|---|---|
| go-logger-sdk | OTel `trace_id`/`span_id` 注入与 `SetLogger` 竞态修复已经具备；终态保留这些正确性能力 |
| go-context-sdk | 已切到 OTel/W3C，但仍保留自定义 `Span` 包装、私有 TracerProvider/`Init`、独立 Request-ID 与 bizctx `requestid`；这些均不属于终态 |
| go-gin-sdk | 已有 OTel `Tracing()`，但仍安装独立 `RequestID()`、写 `request.id` 属性并传播第二个 Header；Logger API 还带无参调用兼容形态 |

目标形态（三层各可替换）：

```text
SDK 库  ──只认──▶ OTel API(可空转,无 Provider 注入时零行为)
应用    ──装配──▶ OTel SDK + OTLP Exporter(发数据)
部署    ──选择──▶ Tempo 或 Jaeger(存储,代码无感)
```

## 2. 关键决策

1. **纯 W3C 终态**：当前没有已上线服务或存量调用方，v1.2.0 不保留旧 API、旧行为或旧签名约束，不解析或注入 B3，不桥接旧 ctx，也不设计迁移期、混跑、双栈、灰度、发布窗口协调或回滚兼容能力。各服务在首次需要 Tracing 的部署前完成装配即可。
2. **`trace_id` 是唯一技术关联 ID**：HTTP 入站有合法 W3C 父上下文时续用其 TraceID，无父上下文时由 OTel SDK 在创建 SERVER root Span 时生成；日志、跨服务传播和客户端排障回执均读取同一个 OTel SpanContext。删除独立 UUID Request-ID、`request-id` 传播、`request.id` Span 属性及 bizctx `requestid` 字段。若未来需要工单号、幂等键等业务关联值，必须另立有明确语义的业务字段，不得复用 TraceID。
3. **只使用 OTel 全局装配**：`tracing`/`bizctx` 生产包只 import OTel API（`go.opentelemetry.io/otel` + `otel/trace`），不装配 OTel SDK/Exporter，也不维护私有 Provider、Propagator 或 `Init`；测试和可运行示例可以依赖 OTel SDK，HTTP 出站示例可以依赖 `otelhttp`。应用通过标准 `otel.SetTracerProvider(tp)` 与 `otel.SetTextMapPropagator(propagation.TraceContext{})` 完成一次装配，go-context-sdk 与 `otelhttp` 读取同一组全局对象。默认全局 Provider 为 Noop：可保留合法入站父上下文的 TraceID，但不会为无父请求生成 root TraceID；关闭导出但仍需为无父请求生成 TraceID 时，应用可注入无 Exporter、`NeverSample` 的 SDK Provider。
4. **框架管理 Span 生命周期**：删除自定义 `Span` 结构体和所有旧式包装方法，包括 `StartChildSpan`、`SetTag`、`LogFields`、`Finish`、`SetBaggage`、`WithOpentracingContext`、`WithContext`、`Empty`。`StartSpan` 直接返回标准 `trace.Span`，但定位为跨 Module 的底层 instrumentation API，只供 Middleware、Decorator、Provider、Tool Runtime、MQ/Job Runner 等框架代码创建和结束 Span；普通业务代码不直接持有 `trace.Span`，只透传 Context，并可通过 `WithKV` 给当前 Span 补充属性。v1.2.0 不实现 deprecated API 或适配层；`apidiff` 仅作为可选审计工具，不是发布门禁。
5. **进程内不桥接旧 ctx**：同一进程内中间件与业务代码版本原子升级，不存在"新代码读旧 ctx"的场景。
6. **trace 与 bizctx 保持两个 ctx value**：OTel Span（系统级，W3C 头）与 BizContext（业务级，`x-bizctx-*` 头）各管一件事；不自定义统一 value（标准 instrumentation 只认 OTel 私有 key）。BizContext 删除无独立业务语义的 `requestid`，其余字段不变。
7. **彻底移除 `jaeger-client-go` 与 `opentracing-go`**；Jaeger 后端可继续作为存储使用（经 OTLP），与客户端库无关。
8. **OTel 版本一致**：三个 SDK 及下游服务必须使用同一 OTel minor 版本；semconv 使用精确版本化 import path（`go.opentelemetry.io/otel/semconv/v1.<minor>.0`），并在属性定义处记录 revision（与主方案 §8.2 一致）；instrumentation scope name 使用模块路径（如 `github.com/PycMono/go-context-sdk`），scope version 为对应 SDK 版本。实施启动时以 go-context-sdk 为准锁定实际版本并回填下表，其余仓库与其保持一致：

| 项目 | 锁定的值 | 状态 |
|---|---|---|
| OTel API minor | `v1.45.0` | 已锁定 |
| `otelhttp` | `v0.70.0` | 已锁定（与 OTel v1.45.0 对齐，仅示例/应用出站装配） |
| semconv import path | `go.opentelemetry.io/otel/semconv/v1.43.0` | 已锁定 |
| instrumentation scope name | 各模块的 Go Module 路径 | 已定 |

**门禁：上表回填实际版本号之前，不得修改三个 SDK 的 `go.mod`。**

## 3. go-logger-sdk v1.1.0（约 60 行新增）

| 文件 | 改动 |
|---|---|
| `go.mod` | + `go.opentelemetry.io/otel/trace`（纯 API，几乎零传递依赖） |
| `field.go` | 新增 `TraceFields(ctx, fields)`（`fields` 为 nil 时自动初始化，避免写 nil map panic）；`DefaultToFieldsFunc` 的 TODO 实现为调用它 |
| `options.go` | + `DisableTraceContext bool`（默认 false 即开启注入） |
| `logrus.go` | `Loggers` + `enableTrace bool`；`prepare()` 顺序固定：用户 `ToFieldsFunc` 先执行 → 内置 OTel 注入**最后写入**（`trace_id`/`span_id` 为保留字段，用户函数不可覆盖；需要自定义时只能 `DisableTraceContext` 整体关闭内置注入） |
| `prepare.go` | `defaultLogger` 改 `atomic.Pointer[loggerHolder]`（Logger 是接口，需 holder 结构体封装）；`SetLogger(nil)` 忽略并保持当前值 |

关键代码：

```go
// field.go
const (
    TraceIDKey = "trace_id"
    SpanIDKey  = "span_id"
)

// TraceFields 从 ctx 的 OTel SpanContext 提取 trace_id/span_id 注入日志字段。
// 命名通用、来源唯一:仅读取 OTel SpanContext,不解析任何旧 B3 结构。
// 无有效 SpanContext(未接 OTel / Noop)时不注入任何字段。
func TraceFields(ctx context.Context, fields Fields) Fields {
    sc := trace.SpanContextFromContext(ctx)
    if !sc.IsValid() {
        return fields
    }
    if fields == nil {
        fields = make(Fields, 2)
    }
    fields[TraceIDKey] = sc.TraceID().String()
    fields[SpanIDKey]  = sc.SpanID().String()
    return fields
}
```

行为约定：日志只带 `trace_id`/`span_id` 两个指针字段，不复制 Span 属性；二者为保留字段，唯一来源是 OTel SpanContext，禁止用户函数覆盖或手拼；无 Span 时字段缺失（非空串），不报错。

测试：有 SpanContext → 字段值一致；空 ctx → key 缺失；用户 `ToFieldsFunc` 写入 `trace_id` 时**被内置注入覆盖**；`TraceFields(ctx, nil)` 不 panic；并发 `SetLogger` + 打日志过 `-race`。

## 4. go-context-sdk v1.2.0（标准 OTel API 终态）

| 文件 | 改动 |
|---|---|
| `go.mod` | − `opentracing-go`、− `jaeger-client-go`、− `google/uuid` 直接依赖；+ `go.opentelemetry.io/otel`、`otel/trace`；示例使用与 OTel v1.45.0 对齐的 `otelhttp` v0.70.0 |
| `tracing/tracing.go` | 删除私有 Provider、原子 holder、`Init`、`ServiceName`；保留 scope name/version，并新增直接使用 `otel.Tracer(...).Start` 的底层 `StartSpan` |
| `tracing/span.go` | **删除**：不再包装标准 `trace.Span`，不保留 `SetTag`/`Finish`/`LogFields` 等旧式方法 |
| `tracing/preset.go` | 以平级文件重写：集中提供 `Field`、`KV`、`WithKV` 以及模型、Tool、MCP 的标准语义 Field 预设；不创建额外 Field 文件或 `tracing/genai`、`tracing/tool`、`tracing/mcp` 子包 |
| `tracing/span_context.go` | **删除**：不保留 B3 map、旧 ctx key 或转换逻辑 |
| `tracing/http.go` | `Extract`/`Inject` 经 `otel.GetTextMapPropagator()` 使用应用设置的全局 W3C Propagator，并保留非法/多值 `traceparent` 拒绝逻辑；不维护包内 Propagator |
| `tracing/request_id_transport.go` | **删除**：跨服务仅由 W3C Propagator 传播 Trace Context |
| `tracing/util.go` | 删除 `NewRequestID()`；保留 `TraceIDFromContext(ctx) string`（唯一关联 ID 读取入口） |
| `tracing/const.go` | 删除所有旧 Request-ID/B3 常量；新增响应头常量 `HeaderTraceID = "trace-id"` |
| `bizctx/` | 删除 `requestid` key、`RequestID(v)`、`GetRequestID(ctx)` 及对应测试；其他业务字段不动 |
| `README.md` / `CLAUDE.md` | 按标准 OTel API、单一 TraceID、全局 Provider 和无 Request-ID 终态重写示例与说明 |

核心代码：

```go
func StartSpan(
    ctx context.Context,
    name string,
    opts ...trace.SpanStartOption,
) (context.Context, trace.Span) {
    tracer := otel.Tracer(
        instrumentationScopeName,
        trace.WithInstrumentationVersion(instrumentationScopeVersion),
    )
    return tracer.Start(ctx, name, opts...)
}
```

“有父则续用、无父则生成”由标准 OTel TracerProvider 实现；默认全局 Noop Provider 无父时不生成 TraceID。go-context-sdk 不保存 Provider，不提供二次初始化入口，也不负责 Provider 的 Shutdown。`StartSpan` 因 go-context-sdk、go-gin-sdk 与 go-reagent 分属不同 Module 而保持导出，但普通业务接入示例不展示该方法或 `span.End()`。

### 4.1 公开 API 终态

| 符号 | 终态 |
|---|---|
| `StartSpan(ctx, name, opts...) (context.Context, trace.Span)` | **新增**：唯一 Span 创建 Helper，统一 instrumentation scope |
| `Field` / `KV(key, value)` / `WithKV(ctx, fields...)` | **新增**：普通业务唯一补充 Span 属性的入口；没有 Recording Span 时安全空转 |
| 模型、Tool、MCP Field 预设 | **新增**：全部位于 `tracing/preset.go`，返回 `Field`，不负责创建 Span |
| `Extract(ctx, http.Header) context.Context` / `Inject(ctx, http.Header)` | **终态公开 API**：仅处理 W3C |
| `TraceIDFromContext(ctx) string` / `HeaderTraceID` | **终态公开 API**：读取唯一关联 ID 与定义响应头 |
| `Span`、`SpanContext` 及其全部方法 | **删除**：调用方直接使用标准 `trace.Span`、`trace.SpanFromContext`、`SetAttributes`、`AddEvent`、`End` |
| `Init` / `ServiceName` | **删除**：应用使用 `otel.SetTracerProvider`；服务身份由 OTel Resource `service.name` 定义 |
| `StartSpanFromContext` / `StartNewSpan` / `SpanFromContext` / `SpanFromOpentracing` | **删除**：分别由 `StartSpan` 和标准 OTel API 取代 |
| `Extract`/`Inject` 的旧 Request 版本、`ErrNullRequest`、`AppName` | **删除** |
| `HeaderRequestID` / `NewRequestID` / `NewRequestIDTransport` / 旧 B3 常量 | **删除** |
| `bizctx.RequestID` / `bizctx.GetRequestID` | **删除** |

### 4.2 最终接口签名

```go
func StartSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span)

type Field = attribute.KeyValue

func KV(key string, value any) Field

// WithKV 只更新当前 Recording Span，并原样返回 ctx；不创建或结束 Span。
func WithKV(ctx context.Context, fields ...Field) context.Context

// Extract 从 Header 提取 W3C traceparent/tracestate 并返回携带 Remote Parent 的 ctx。
func Extract(ctx context.Context, h http.Header) context.Context

// Inject 将 ctx 中的 SpanContext 以 W3C traceparent/tracestate 写入 Header。
func Inject(ctx context.Context, h http.Header)

func TraceIDFromContext(ctx context.Context) string

const HeaderTraceID = "trace-id"
```

Header 为 nil、无 `traceparent`、`traceparent` 非法时，`Extract` 原样返回 ctx，不报错；调用 `otel.GetTextMapPropagator()` 前必须显式检查 `h.Values("traceparent")`，多值或逗号合并值视为非法忽略；成功提取的父 SpanContext 标记为 Remote。应用必须把全局 Propagator 固定为 `propagation.TraceContext{}`，不得加入 Baggage。

### 4.3 Field 与平级预设

`KV` 支持 OTel 原生标量和切片类型：`string`、`bool`、常用有符号整数、`float32`、`float64`、`[]string`、`[]bool`、`[]int`、`[]int64`、`[]float64`。空 key 或不支持的值返回无效 Field，`WithKV` 忽略无效 Field；Telemetry 不能因属性类型错误改变业务结果，也不得用 `fmt.Sprint` 隐式序列化未知对象。

`WithKV` 读取 `trace.SpanFromContext(ctx)`，仅当 Span 正在记录时调用 `SetAttributes`，并原样返回 `ctx` 以保持与 `bizctx.WithKV` 一致的调用形态。Field 不写入新的 Context value；在 Span 创建之前调用不会延迟作用到未来 Span，父 Span 的 Field 也不会自动复制到子 Span。

`tracing/preset.go` 提供以下最小预设集合：

| 类别 | 方法 | 属性 |
|---|---|---|
| 模型 | `OperationName(string)` | `gen_ai.operation.name` |
| 模型 | `ProviderName(string)` | `gen_ai.provider.name` |
| 模型 | `RequestModel(string)` | `gen_ai.request.model` |
| 模型 | `ResponseModel(string)` | `gen_ai.response.model` |
| 模型 | `FinishReasons(...string)` | `gen_ai.response.finish_reasons` |
| 模型 | `InputTokens(int)` | `gen_ai.usage.input_tokens` |
| 模型 | `OutputTokens(int)` | `gen_ai.usage.output_tokens` |
| Tool | `ToolName(string)` | `gen_ai.tool.name` |
| Tool | `ToolCallID(string)` | `gen_ai.tool.call.id` |
| Tool | `ToolType(string)` | `gen_ai.tool.type` |
| MCP | `MCPMethodName(string)` | `mcp.method.name` |
| MCP | `MCPProtocolVersion(string)` | `mcp.protocol.version` |
| MCP | `MCPSessionID(string)` | `mcp.session.id` |
| MCP | `MCPResourceURI(string)` | `mcp.resource.uri` |

预设使用 Development 状态的 OTel GenAI/MCP 属性名并在源码注释中固定 semantic-conventions revision。因 OTel Go v1.42.0 起不再从通用生成包导出这些 Development declarations，SDK 在 `preset.go` 内集中声明 key；业务调用方不得散落手写这些标准 key。`gen_ai.tool.call.arguments`、`gen_ai.tool.call.result` 和 `gen_ai.tool.description` 不提供预设，避免鼓励把 Prompt、参数、输出或大文本写入 Trace。`reagent.*` 属于项目私有语义，不进入通用 SDK 预设。

不提供 `Fail(ctx, errorCode)`。谁创建 Span，谁根据返回 error、结构化结果、取消或超时设置稳定错误状态并结束 Span；业务代码只返回错误，不能承担第二套手工标记责任。

### 4.4 TraceID 生命周期与边界

系统不再创建或传播独立 Request-ID，唯一技术关联 ID 为当前 OTel SpanContext 的 TraceID：

- **入站与生成**：`Tracing()` 先调用 `Extract`，再调用 `StartSpan` 创建 SERVER Span。有父上下文时续用其 128-bit TraceID；无父时由应用注入的全局 OTel SDK TracerProvider 创建 root Span 并生成 TraceID。禁止在 SDK 中手工生成 TraceID。
- **外部信任边界**：公网入口不得直接信任外部 `traceparent`/`tracestate`。网关应终止外部 Trace Context、创建新的内部 root Span，再向内部服务注入新的 W3C Context；仅明确允许的可信上游可以续接 Remote Parent。直接暴露公网且不经过网关的服务必须在应用层实现等价策略。TraceID 始终是不可信技术关联值，不得用于鉴权、幂等或业务身份。
- **响应回写**：SERVER Span 创建后、`c.Next()` 之前读取 `TraceIDFromContext(ctx)`；有效时以 32 位小写十六进制写入响应头 `trace-id`。Noop 且无合法父上下文，或其他无有效 SpanContext 的场景省略该头，不生成 UUID 或其他格式兜底。
- **浏览器可见性**：`CORS()` 把 `trace-id` 加入 `Access-Control-Expose-Headers`，但不加入入站 `Access-Control-Allow-Headers`。被 CORS 中间件直接终止的 OPTIONS 预检请求不要求创建 Span 或返回 `trace-id`。
- **日志与 Span**：日志只记录 OTel SpanContext 自动注入的 `trace_id`/`span_id`；不重复写 `request.id` Span 属性，也不进入 Baggage 或 Metrics Label。
- **出站**：HTTP Client 使用 `otelhttp.NewTransport(base)`，由全局 W3C Propagator 注入 Trace Context 并创建 CLIENT Span；不增加第二个传播头。
- **业务关联值**：工单号、幂等键、消息 ID 等若未来需要跨服务传播，应定义独立、具名、受校验的业务契约，不得复用 TraceID。

### 4.5 测试

使用标准全局 SDK TracerProvider 和 In-Memory SpanRecorder 验证：父 Span 续链 trace_id 一致；无父自动新建 root Span；`StartSpan` 返回标准 `trace.Span`；`WithKV` 写入当前 Recording Span、Noop 安全空转且不把 Field 传播到未来 Span；`KV` 的支持类型、空 key 和不支持类型均不 panic；每个预设的 key、类型和值精确；Extract W3C 后父标记 Remote；非法/多值 traceparent 静默忽略；Noop 可保留合法父 TraceID，但无父不生成；出站仅传播 W3C；包无私有 Provider、`Init` 或 init 副作用；依赖图无 jaeger/opentracing，生产源码不直接 import `google/uuid`。OTel SDK v1.45.0 自身间接依赖 `google/uuid`，测试和示例使用 SDK 时允许该间接依赖存在。测试结束必须恢复全局 Provider/Propagator，避免污染其他用例。

## 5. go-gin-sdk v0.7.0

| 文件 | 改动 |
|---|---|
| `middleware/trace.go` | 重写为 OTel（见下）；删除 OpenTracing/自家 map 双写；创建 SERVER Span 后回写 `trace-id` 响应头 |
| `middleware/request_id.go` | **删除**：不再生成、校验或传播独立 Request-ID |
| `middleware/metrics.go` | **移出 v0.7**：OTel HTTP server 指标（名称、单位、Histogram 边界、路由 Label、注册方式）另立 v0.8 规格，避免半成品规格进入实施 |
| `middleware/logger.go` | 内容安全策略（见 §5.1）；终态签名为 `Logger(LoggerOptions) gin.HandlerFunc`，不保留无参/可变参数兼容形态 |
| `middleware/bizctx.go` | 白名单删除 `requestid`（`x-bizctx-requestid` 不再读取） |
| `middleware/cors.go` | `Access-Control-Expose-Headers` 增加 `trace-id`；不把它加入入站 `Access-Control-Allow-Headers` |
| `prepare.go` | `DefaultOptions()` 默认链更新为 `CORS → Tracing → Bizctx → gin.Recovery()`（Recovery 保留在链尾：panic 被恢复后 `Tracing()` 才能读到 500） |
| `go.mod` | context-sdk → v1.2、logger-sdk → v1.1；+ `otel/codes`、`otel/semconv`（固定 release）；jaeger/opentracing 间接依赖自动清零；v0.8 起 + `otel/metric` |

`trace.go` 参考实现：

```go
func Tracing() gin.HandlerFunc {
    return func(c *gin.Context) {
        ctx := tracing.Extract(c.Request.Context(), c.Request.Header)
        ctx, span := tracing.StartSpan(ctx, "HTTP "+c.Request.Method,
            trace.WithSpanKind(trace.SpanKindServer))   // 显式 SERVER Kind
        defer span.End()                              // 覆盖 panic/abort/提前返回

        c.Request = c.Request.WithContext(ctx)         // 只写 OTel ctx
        if traceID := tracing.TraceIDFromContext(ctx); traceID != "" {
            c.Header(tracing.HeaderTraceID, traceID)   // 必须在响应提交前写入
        }
        c.Next()

        // 路由模板命名,动态 ID 不进入 Span 名称;未匹配路由用固定名
        route := c.FullPath()
        if route == "" {
            route = "route_not_found"
        }
        span.SetName(c.Request.Method + " " + route)
        span.SetAttributes(
            semconv.HTTPRequestMethodKey.String(c.Request.Method),
            semconv.HTTPRoute(route),
            semconv.UserAgentOriginal(c.Request.UserAgent()),
            semconv.HTTPResponseStatusCode(c.Writer.Status()),
        )
        // Server Span 仅 5xx 标 Error;1xx-4xx 是正常业务结果
        if status := c.Writer.Status(); status >= 500 {
            span.SetStatus(codes.Error, http.StatusText(status))
        } else if c.Errors.ByType(gin.ErrorTypePrivate).String() != "" {
            span.SetStatus(codes.Error, "handler error")   // 稳定描述,不写错误正文
        }
    }
}
```

### 5.1 `logger.go` 内容安全策略

默认从严，与主方案 §11 内容策略一致：

- **v0.7 固定不采集请求/响应 Body**，只记录长度、状态、耗时等元数据。Body 采集能力整体移出，未来另立独立安全规格（覆盖授权、Content-Type 白名单、脱敏、大小上限、保留与删除），届时单独评审。
- Header 为**不可覆盖 Denylist**：`Authorization`、`Cookie`、`Set-Cookie` 永不记录（匹配大小写不敏感，纳入测试）；其余 Header 默认不记。`Logger(options LoggerOptions) gin.HandlerFunc` 接收 `type LoggerOptions struct { HeaderAllowlist []string }`，默认行为由 `Logger(LoggerOptions{})` 明确表达，不提供可变参数或旧式无参调用。
- URL 只记录 method、route（`FullPath()`）、path；**不记录 raw query**（query 可能含 token/code）。
- 流式响应（SSE/WebSocket/文件下载，按路由配置或最终 Content-Type 判定）的 `Write`/`WriteString`/`Flush` 路径均不缓冲 body。
- 上述规则纳入敏感信息负向测试（构造含 password/token 的请求，断言日志无泄露）。

测试：W3C 入站头 → SERVER Span 与 `trace-id` 响应头使用同一 32 位小写十六进制 trace_id；无头且已设置全局 SDK Provider → 新 Trace 并回写其 ID；Span 名称为路由模板（带路径参数的 URL 不产生新名称）；3xx/4xx 不标 Error；5xx 与私有错误标 Error 且正文不外泄；Span 不含冗余 `request.id` 属性；全局 Noop 时中间件不 panic，合法父 TraceID 可回写、无父请求不写 `trace-id`；CORS 实际响应暴露 `trace-id` 且预检请求不要求该头；`Logger(LoggerOptions{})` 与 Header Allowlist 行为明确；SSE 请求 logger 不缓冲 body。

## 6. 交付与验收

go-gin-sdk 同时依赖 logger/context 两个终态版本，因此构建发布顺序为：先发布 go-logger-sdk v1.1.0 与 go-context-sdk v1.2.0，再发布 go-gin-sdk v0.7.0。当前无上线服务，不要求跨服务协调发布；每个服务在首次需要 Tracing 的部署前接入对应版本即可。

每个包统一验收三条：

1. 未设置全局 OTel SDK Provider 时：标准全局 Noop 不联网、不启动后台 goroutine、不记录本地 Span 且不 panic；合法父 TraceID 可以续用和回写，无父请求不生成 TraceID。
2. 设置全局 SDK TracerProvider 后：HTTP 请求有合法父上下文则续链，无父则创建 root Span；响应 `trace-id`、日志 `trace_id` 和 SERVER Span 使用同一值。go-context-sdk 与 otelhttp 必须观察到同一个全局 Provider。
3. `go mod graph` 无 jaeger/opentracing；go-context-sdk 生产源码无 `google/uuid` import、私有 Provider、自定义 `Span`、`Init` 和独立 Request-ID。测试/示例所用 OTel SDK 自身的 `google/uuid` 间接依赖不视为违规。README/CLAUDE 与公开 API、示例代码完全一致。

### 6.1 服务装配清单

每个服务首次启用 Tracing 时完成以下标准 OTel 装配：

```text
创建 Resource + TracerProvider(可选 MeterProvider)
→ otel.SetTracerProvider(tp):供 go-context-sdk/otelhttp 等全部 instrumentation 使用
→ otel.SetTextMapPropagator(propagation.TraceContext{})
→ 安装 SERVER Middleware(go-gin-sdk Tracing,或自建 Engine 的等价中间件)
→ 日志字段注入(logger-sdk v1.1 默认生效)
→ 包装出站 HTTP Transport(otelhttp,仅 W3C,见 §4.3)
→ 应用生命周期 OnStop:ForceFlush + Shutdown
```

Provider 只在应用启动时通过 `otel.SetTracerProvider` 设置一次，生命周期由应用管理。未设置 Provider 时使用标准全局 Noop；未设置 Propagator 时跨进程断链。若关闭 Exporter 但仍要求无父请求生成 TraceID，应用注入无 Exporter、`NeverSample` 的 SDK Provider，不得由 SDK 手工生成兜底 ID。

非 HTTP 入口同样以 Span 为关联边界：MQ Consumer 使用全局 Propagator 从消息 Carrier 提取上下文，无父时创建 `SpanKindConsumer` root Span；定时任务和 CLI 在第一条业务日志前调用 `StartSpan` 创建 root Span。所有下游调用必须传递返回的 Context。没有有效 SpanContext 时日志不伪造 `trace_id`；消息号、任务号等使用独立业务字段。

各服务的中间件安装落点逐一登记：`go-reagent` 使用自建 Engine，安装点为 `infrastructure/driver/gingext/gingext.go`（不经 go-gin-sdk `DefaultOptions`）；其他服务若用 `DefaultOptions()` 则 `Tracing()` 透明生效。

### 6.2 交付要求

- 三个 SDK 发布对应终态 tag；不建设兼容层、迁移矩阵或回滚方案。`apidiff` 可用于审计最终公开面，但不作为发布门禁。
- `go mod tidy` 后提交 `go.mod`/`go.sum`；go-reagent 依赖图中的 jaeger/opentracing 间接依赖必须消失。
- 更新 go-context-sdk、go-gin-sdk 及 SDK 级 README/CLAUDE，删除 `Init`、自定义 `Span`、Request-ID、旧中间件顺序和旧 HTTP Client 包装示例。
- 全工作区编译验证不存在 `StartSpanFromContext`、`Finish`、`SetTag`、`LogFields`、`bizctx.RequestID/GetRequestID` 或自定义 `request_id` 日志注入调用。
- 若生产请求经过网关/CDN，首次部署前验证 `trace-id` 与 `Access-Control-Expose-Headers` 能完整到达客户端；外部 `traceparent` 按 §4.3 信任边界处理。

## 7. 非目标

- 除删除无独立语义的 `requestid` preset/getter 外，不修改 `bizctx` 其他字段与 `x-bizctx-*` 通道。
- 不实现 B3 解析/注入、旧 ctx 桥接、deprecated API 或迁移层（见 §2 决策 1、4、5）。
- 不在 SDK 库内装配 TracerProvider/Exporter（应用层职责）。
- 不统一 trace 与 bizctx 的 ctx value（见 §2 决策 6）。
- 不新增独立 Request-ID、Correlation-ID 或业务幂等 ID；此类业务需求另立规格。
