package gingext

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/PycMono/go-context-sdk/bizctx"
	"github.com/PycMono/go-reagent/config"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestNewEngineInstallsVisitorAndSameOriginBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine, err := NewEngine(&config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	engine.GET("/who", func(c *gin.Context) { c.String(http.StatusOK, bizctx.GetUserID(c.Request.Context())) })
	engine.POST("/write", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	who := httptest.NewRecorder()
	engine.ServeHTTP(who, httptest.NewRequest(http.MethodGet, "/who", nil))
	if who.Body.String() == "" || len(who.Result().Cookies()) != 1 {
		t.Fatalf("visitor response = %q, cookies = %#v", who.Body.String(), who.Result().Cookies())
	}

	request := httptest.NewRequest(http.MethodPost, "/write", nil)
	request.Host = "127.0.0.1:8080"
	request.Header.Set("Origin", "https://evil.test")
	blocked := httptest.NewRecorder()
	engine.ServeHTTP(blocked, request)
	if blocked.Code != http.StatusForbidden {
		t.Fatalf("cross-origin status = %d", blocked.Code)
	}
}

// TestTraceContextBoundaryStripsForgedParent 是设计 §7/§16.4 的阶段 1 验收：
// 公网伪造 Parent 被忽略（剥离后由内部 root Span 生成新 TraceID），可信上游
// Parent 被续接（端到端 trace_id 不变）。
func TestTraceContextBoundaryStripsForgedParent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// 安装 NeverSample SDK Provider 与 W3C Propagator：Span 不导出，
	// 但 SpanContext 生成与 Remote Parent 续接行为与生产一致。
	provider := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.NeverSample()))
	previousProvider := otel.GetTracerProvider()
	previousPropagator := otel.GetTextMapPropagator()
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		otel.SetTracerProvider(previousProvider)
		otel.SetTextMapPropagator(previousPropagator)
		_ = provider.Shutdown(t.Context())
	})

	engine, err := NewEngine(&config.Config{Observability: config.ObservabilityConfig{
		Tracing: config.ObservabilityTracingConfig{TrustedUpstreams: []string{"10.0.0.0/8"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	engine.GET("/ping", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	const parentTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	const traceparent = "00-" + parentTraceID + "-00f067aa0ba902b7-01"

	// 不可信公网来源：伪造 Parent 被剥离，由内部 root Span 生成新 TraceID。
	forged := httptest.NewRequest(http.MethodGet, "/ping", nil)
	forged.RemoteAddr = "203.0.113.9:51000"
	forged.Header.Set("traceparent", traceparent)
	forgedRecorder := httptest.NewRecorder()
	engine.ServeHTTP(forgedRecorder, forged)
	forgedTraceID := forgedRecorder.Header().Get("trace-id")
	if forgedTraceID == "" || forgedTraceID == parentTraceID {
		t.Fatalf("伪造 Parent 应被忽略并生成新 TraceID，实际 trace-id = %q", forgedTraceID)
	}

	// 可信上游：Remote Parent 被续接，回执 TraceID 与父级一致。
	trusted := httptest.NewRequest(http.MethodGet, "/ping", nil)
	trusted.RemoteAddr = "10.1.2.3:51000"
	trusted.Header.Set("traceparent", traceparent)
	trustedRecorder := httptest.NewRecorder()
	engine.ServeHTTP(trustedRecorder, trusted)
	if got := trustedRecorder.Header().Get("trace-id"); got != parentTraceID {
		t.Fatalf("可信上游 Parent 应被续接，trace-id = %q，期望 %q", got, parentTraceID)
	}
}
