# PycMono SDK OTel Terminal API and Field Presets Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the approved no-compatibility OTel terminal APIs and add flat model/Tool/MCP Field presets to go-context-sdk v1.2.0.

**Architecture:** Keep logging correlation in go-logger-sdk, use go-context-sdk as the shared global OTel/Field facade, and make go-gin-sdk create the HTTP SERVER Span and return its TraceID. This plan defines only SDK capabilities and contracts; Provider, ToolRuntime, and MCP runtime integration remain responsibilities of their consuming frameworks.

**Tech Stack:** Go 1.25+, OpenTelemetry Go v1.45.0, `otelhttp` v0.70.0, semconv v1.43.0, Gin v1.9.1, Logrus, `go test`, race detector.

**Spec:** `docs/superpowers/specs/2026-08-21-pycmono-sdk-otel-migration-design.md`

## Global Constraints

- Work directly on each SDK repository's `master` branch as explicitly requested; do not create a worktree.
- `trace_id` is the only technical correlation ID; delete Request-ID generation, propagation, bizctx storage, middleware, and Span attributes.
- go-context-sdk exposes `StartSpan`, `Extract`, `Inject`, `TraceIDFromContext`, `HeaderTraceID`, `Field`, `KV`, `WithKV`, and the approved flat model/Tool/MCP presets.
- go-context-sdk must use `otel.GetTracerProvider()` and `otel.GetTextMapPropagator()`; it must not own Provider or Propagator state.
- `StartSpan` returns `(context.Context, trace.Span)` and the SDK defines no Span wrapper.
- `StartSpan` is a low-level instrumentation API; ordinary business examples expose only Context propagation and optional `WithKV`.
- Do not add `Fail`, callback-based tracing helpers, nested preset subpackages, Tool arguments/results presets, or a duplicate MCP protocol Span.
- Gin writes a valid TraceID to `trace-id` before `c.Next()` and exposes it through CORS.
- `Logger` has the exact signature `Logger(LoggerOptions) gin.HandlerFunc`.
- Preserve unrelated working-tree changes. Do not create release tags or commits during this run.
- Follow red-green-refactor for observable behavior changes; use `/tmp/codex-pycmono-otel-modcache` because the user Go module cache is not writable in this environment.

---

### Task 1: Verify and document go-logger-sdk terminal behavior

**Files:**
- Modify: `/Users/allen/projects/work/github/sdk/go-logger-sdk/README.md`

**Interfaces:**
- Consumes: existing `TraceFields`, reserved `trace_id`/`span_id`, atomic `SetLogger` implementation.
- Produces: documentation matching v1.1.0 terminal behavior; no public API change.

- [x] **Step 1: Run the focused correlation and concurrency tests**

```bash
GOMODCACHE=/tmp/codex-pycmono-otel-modcache go test -race ./... -run 'TestTrace|TestSetLogger'
```

Expected: PASS. Any failure is an already-implemented terminal behavior regression and must be fixed test-first.

- [x] **Step 2: Replace the custom context-key README example**

Document that `TraceFields` reads only `trace.SpanContextFromContext`, that reserved fields are injected after `ToFieldsFunc`, and that `DisableTraceContext` is the only opt-out.

- [x] **Step 3: Verify the package**

```bash
GOMODCACHE=/tmp/codex-pycmono-otel-modcache go test -race ./...
git diff --check
```

Expected: all tests pass and no whitespace errors.

### Task 2: Replace go-context-sdk with the standard global OTel API

**Files:**
- Modify: `/Users/allen/projects/work/github/sdk/go-context-sdk/tracing/tracing_test.go`
- Modify: `/Users/allen/projects/work/github/sdk/go-context-sdk/tracing/http_test.go`
- Modify: `/Users/allen/projects/work/github/sdk/go-context-sdk/tracing/tracing.go`
- Modify: `/Users/allen/projects/work/github/sdk/go-context-sdk/tracing/http.go`
- Modify: `/Users/allen/projects/work/github/sdk/go-context-sdk/tracing/util.go`
- Modify: `/Users/allen/projects/work/github/sdk/go-context-sdk/tracing/const.go`
- Modify: `/Users/allen/projects/work/github/sdk/go-context-sdk/example/main.go`
- Create: `/Users/allen/projects/work/github/sdk/go-context-sdk/example/main_test.go`
- Modify: `/Users/allen/projects/work/github/sdk/go-context-sdk/example/http/server.go`
- Create: `/Users/allen/projects/work/github/sdk/go-context-sdk/example/http/server_test.go`
- Delete: `/Users/allen/projects/work/github/sdk/go-context-sdk/tracing/span.go`
- Delete: `/Users/allen/projects/work/github/sdk/go-context-sdk/tracing/span_test.go`
- Delete: `/Users/allen/projects/work/github/sdk/go-context-sdk/tracing/preset.go`
- Delete: `/Users/allen/projects/work/github/sdk/go-context-sdk/tracing/request_id_transport.go`
- Delete: `/Users/allen/projects/work/github/sdk/go-context-sdk/tracing/request_id_transport_test.go`

**Interfaces:**
- Produces: `StartSpan(context.Context, string, ...trace.SpanStartOption) (context.Context, trace.Span)`.
- Produces: `Extract(context.Context, http.Header) context.Context`, `Inject(context.Context, http.Header)`, `TraceIDFromContext(context.Context) string`, and `HeaderTraceID`.

- [x] **Step 1: Write terminal StartSpan tests**

Use a temporarily installed global `sdktrace.TracerProvider`, restore the previous global Provider in cleanup, and assert root recording, parent preservation, standard `trace.Span`, instrumentation scope name/version, and Noop behavior with and without a valid parent.

- [x] **Step 2: Verify RED**

```bash
GOMODCACHE=/tmp/codex-pycmono-otel-modcache go test ./tracing -run 'TestStartSpan'
```

Expected: FAIL because `StartSpan` does not exist and the package still owns a private Provider.

- [x] **Step 3: Implement the minimal global helper**

```go
func StartSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
    return otel.Tracer(
        instrumentationScopeName,
        trace.WithInstrumentationVersion(instrumentationScopeVersion),
    ).Start(ctx, name, opts...)
}
```

Delete `Init`, `ServiceName`, atomic Provider storage, `Span`, `StartSpanFromContext`, `SpanFromContext`, and wrapper methods.

- [x] **Step 4: Verify GREEN**

Run the same focused command. Expected: PASS.

- [x] **Step 5: Write global Propagator tests**

Install `propagation.TraceContext{}` globally and restore the previous Propagator in cleanup. Keep literal cases for valid, missing, malformed, multiple, comma-joined `traceparent`, multi-line `tracestate`, nil Header, and injection. Add a sentinel Propagator case proving `Extract` and `Inject` use the global object.

- [x] **Step 6: Verify RED and implement global propagation**

```bash
GOMODCACHE=/tmp/codex-pycmono-otel-modcache go test ./tracing -run 'TestExtract|TestInject'
```

Expected before implementation: sentinel case FAIL. Replace the package-local propagator with `otel.GetTextMapPropagator()` while retaining invalid/multi-value `traceparent` rejection and cloned multi-line `tracestate` joining.

- [x] **Step 7: Remove Request-ID from tracing and bizctx**

Delete `NewRequestID`, `HeaderRequestID`, `NewRequestIDTransport`, `bizRequestID`, `RequestID`, `GetRequestID`, their tests, and the `google/uuid` dependency. Update bizctx tests to cover only `id`, `userid`, `tenantid`, `appid`, and `clientip`.

- [x] **Step 8: Update examples and package documentation**

Use `otel.SetTracerProvider`, `otel.SetTextMapPropagator(propagation.TraceContext{})`, `StartSpan`, standard `SetAttributes`/`AddEvent`/`End`, and `otelhttp.NewTransport(base)`. The HTTP example must Extract before its SERVER Span, create a same-trace CLIENT Span, propagate W3C, stop `http.Server` through Context cancellation, and explicitly Shutdown the Provider without `os.Exit`. Cover the HTTP trace and shutdown paths with real listener/`httptest` tests. Remove migration, mixed-version, Request-ID, private Provider, and wrapper guidance from README/CLAUDE.

- [x] **Step 9: Verify context SDK**

```bash
GOMODCACHE=/tmp/codex-pycmono-otel-modcache go mod tidy
GOMODCACHE=/tmp/codex-pycmono-otel-modcache go test -race ./...
go mod graph | rg 'jaeger|opentracing'
rg -n 'github.com/google/uuid' --glob '*.go' .
git diff --check
```

Expected: tests pass; Jaeger/OpenTracing dependency search and production UUID import search are empty; the test/example OTel SDK may retain its own indirect UUID dependency; no whitespace errors.

### Task 3: Implement go-gin-sdk TraceID-only HTTP behavior

**Files:**
- Modify: `/Users/allen/projects/work/github/sdk/go-gin-sdk/middleware/trace_test.go`
- Create: `/Users/allen/projects/work/github/sdk/go-gin-sdk/middleware/cors_test.go`
- Modify: `/Users/allen/projects/work/github/sdk/go-gin-sdk/middleware/logger_test.go`
- Modify: `/Users/allen/projects/work/github/sdk/go-gin-sdk/prepare_test.go`
- Modify: `/Users/allen/projects/work/github/sdk/go-gin-sdk/middleware/trace.go`
- Modify: `/Users/allen/projects/work/github/sdk/go-gin-sdk/middleware/cors.go`
- Modify: `/Users/allen/projects/work/github/sdk/go-gin-sdk/middleware/logger.go`
- Modify: `/Users/allen/projects/work/github/sdk/go-gin-sdk/prepare.go`
- Delete: `/Users/allen/projects/work/github/sdk/go-gin-sdk/middleware/request_id.go`
- Delete: `/Users/allen/projects/work/github/sdk/go-gin-sdk/middleware/request_id_test.go`

**Interfaces:**
- Consumes: go-context-sdk terminal `StartSpan`, `TraceIDFromContext`, and `HeaderTraceID`.
- Produces: `Tracing()`, `CORS()`, `Bizctx()`, and `Logger(LoggerOptions)` with no Request-ID surface.

- [x] **Step 1: Write TraceID response tests**

Install and restore the global OTel Provider/Propagator. Assert a valid remote parent keeps its literal TraceID, a root request gets a 32-character lowercase TraceID, and JSON/error/streaming responses receive `trace-id` before commit. Assert Span attributes contain no `request.id`; Noop with no parent omits the response header while Noop with a valid remote parent returns it.

- [x] **Step 2: Verify RED**

Run Gin against the local context/logger modules through a temporary `go.work`:

```bash
GOMODCACHE=/tmp/codex-pycmono-otel-modcache go test ./middleware -run 'TestTracing'
```

Expected: FAIL because current middleware calls the removed wrapper and does not write `trace-id`.

- [x] **Step 3: Implement TraceID-only middleware**

Call `tracing.Extract`, then `tracing.StartSpan(..., trace.WithSpanKind(trace.SpanKindServer))`; replace the request context; write a non-empty `TraceIDFromContext` to `HeaderTraceID` before `c.Next()`; retain route-template naming, safe attributes, and status policy; remove bizctx and `request.id` usage.

- [x] **Step 4: Add and implement CORS behavior**

Write a failing test that normal responses expose `trace-id`, inbound allow-headers do not contain it, and OPTIONS still aborts without requiring a TraceID. Add `trace-id` only to `Access-Control-Expose-Headers`.

- [x] **Step 5: Make Logger terminal signature compile-time exact**

Change all tests to call `Logger(LoggerOptions{})` or one literal option. Verify RED from the current variadic merge case, then change production to `func Logger(options LoggerOptions) gin.HandlerFunc` and derive one normalized allowlist.

- [x] **Step 6: Delete Request-ID and update the default chain**

Delete RequestID source/tests. Change the expected and actual chain to `CORS → Tracing → Bizctx → gin.Recovery()`.

- [x] **Step 7: Update dependencies and documentation**

Remove `google/uuid`; keep OTel SDK test-only usage; rewrite README tracing/logger/default-chain sections for W3C and TraceID-only behavior.

- [x] **Step 8: Verify Gin SDK**

```bash
GOMODCACHE=/tmp/codex-pycmono-otel-modcache go mod tidy
GOMODCACHE=/tmp/codex-pycmono-otel-modcache go test -race ./...
go mod graph | rg 'jaeger|opentracing'
rg -n 'github.com/google/uuid' --glob '*.go' .
git diff --check
```

Expected: tests pass; Jaeger/OpenTracing dependency search and production UUID import search are empty; the test OTel SDK may retain its own indirect UUID dependency; no whitespace errors.

### Task 4: Cross-repository terminal audit

**Files:**
- Verify all changed files in the three SDK repositories.

**Interfaces:**
- Consumes: Tasks 1–3.
- Produces: evidence that the three SDKs match the approved terminal design.

- [x] **Step 1: Search for removed surfaces**

```bash
rg -n 'StartSpanFromContext|StartNewSpan|SpanFromContext|SpanFromOpentracing|NewRequestIDTransport|HeaderRequestID|NewRequestID\(|bizctx\.RequestID|GetRequestID|middleware\.RequestID|tracing\.Init|\.Finish\(|\.SetTag\(|\.LogFields\(|Logger\(\)'
```

Expected: no production, test, example, or current README/CLAUDE matches.

- [x] **Step 2: Run all race suites with the temporary workspace**

Run each SDK suite with the task-specific module cache; run Gin with local context/logger modules. Expected: all PASS.

- [x] **Step 3: Inspect diffs and status**

Run `git diff --check`, `git diff --stat`, and `git status --short` in every repository. Confirm only in-scope files changed and the pre-existing go-context-sdk README/CLAUDE edits were incorporated rather than discarded.

### Task 5: Add Field, KV, WithKV, and flat semantic presets

**Files:**
- Modify: `/Users/allen/projects/work/github/sdk/go-context-sdk/tracing/preset.go`
- Create: `/Users/allen/projects/work/github/sdk/go-context-sdk/tracing/preset_test.go`

**Interfaces:**
- Produces: `type Field = attribute.KeyValue`.
- Produces: `KV(string, any) Field` and `WithKV(context.Context, ...Field) context.Context`.
- Produces: the approved flat model, Tool, and MCP preset functions; no `Fail` and no nested preset package.

- [x] **Step 1: Write failing Field tests**

Use an SDK `SpanRecorder` and assert that `WithKV` returns the identical Context, writes valid scalar/slice fields to the current recording Span, ignores blank keys/unsupported values, and does not panic or retain fields with the global Noop Provider.

- [x] **Step 2: Verify RED**

```bash
GOMODCACHE=/tmp/codex-pycmono-otel-modcache go test ./tracing -run 'TestKV|TestWithKV'
```

Expected: FAIL because `Field`, `KV`, and `WithKV` do not exist.

- [x] **Step 3: Implement the minimal Field API**

`KV` maps only documented OTel-native values to `attribute.KeyValue`; blank keys and unknown types return `Field{}`. `WithKV` filters `Field.Valid()`, calls `trace.SpanFromContext(ctx).SetAttributes` only for a recording Span, and returns `ctx` unchanged.

- [x] **Step 4: Verify Field GREEN**

Run the Step 2 command and expect PASS.

- [x] **Step 5: Write failing preset table tests**

Assert exact key, type, and value for `OperationName`, `ProviderName`, `RequestModel`, `ResponseModel`, `FinishReasons`, `InputTokens`, `OutputTokens`, `ToolName`, `ToolCallID`, `ToolType`, `MCPMethodName`, `MCPProtocolVersion`, `MCPSessionID`, and `MCPResourceURI`.

- [x] **Step 6: Implement flat presets**

Implement every preset in `package tracing` using `attribute.Key(...).String`, `.StringSlice`, or `.Int`; record semantic-conventions revision `1.41.0` in the file comment because OTel Go removed the Development declarations from the generic generated package beginning with v1.42.0.

- [x] **Step 7: Verify preset GREEN and package race safety**

```bash
GOMODCACHE=/tmp/codex-pycmono-otel-modcache go test -race ./tracing
```

Expected: PASS.

### Task 6: Document business-facing use and verify all SDKs

**Files:**
- Modify: `/Users/allen/projects/work/github/sdk/go-context-sdk/README.md`
- Modify: `/Users/allen/projects/work/github/sdk/go-context-sdk/CLAUDE.md`

**Interfaces:**
- Consumes: Task 5 API.
- Produces: SDK documentation that keeps Span lifecycle in instrumentation code and shows ordinary business code using only `WithKV`.

- [x] **Step 1: Update documentation**

Document `StartSpan` as a low-level instrumentation API; add `WithKV` and preset examples; state that presets only build Fields, do not create nodes, and that error status/lifecycle belongs to Middleware/Decorator/Runner. Explicitly document that `Fail` does not exist.

- [x] **Step 2: Run context SDK verification**

```bash
GOMODCACHE=/tmp/codex-pycmono-otel-modcache go test -race ./...
GOMODCACHE=/tmp/codex-pycmono-otel-modcache go vet ./...
git diff --check
```

- [x] **Step 3: Run logger and Gin regression suites against local context SDK**

Use a temporary `go.work` containing all three SDK repositories, then run `go test -race ./...` and `go vet ./...` in go-logger-sdk and go-gin-sdk with `GOMODCACHE=/tmp/codex-pycmono-otel-modcache`.

- [x] **Step 4: Audit public surface and forbidden compatibility**

```bash
rg -n 'func Fail|package genai|package mcp|package tool' /Users/allen/projects/work/github/sdk/go-context-sdk/tracing
rg -n 'StartSpanFromContext|StartNewSpan|SetTag|LogFields|NewRequestID' /Users/allen/projects/work/github/sdk/go-context-sdk
```

Expected: both searches are empty except historical prose explicitly describing removed APIs.
