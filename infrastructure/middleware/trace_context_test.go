package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	sdkmiddleware "github.com/PycMono/go-gin-sdk/middleware"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// TestTraceContextBoundary 是设计 §7/§16.4 的阶段 1 验收：公网伪造 Parent
// 被忽略（剥离后由内部 root Span 生成新 TraceID），可信上游 Parent 被续接
//（端到端 trace_id 不变）。
func TestTraceContextBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
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

	boundary, err := TraceContextBoundary([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	engine.Use(boundary, sdkmiddleware.Tracing())
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

// TestTraceContextBoundaryUnit 覆盖中间件本身：无头部直通、非法配置失败、
// 不可信来源剥离、tracestate 一并剥离。
func TestTraceContextBoundaryUnit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	boundary, err := TraceContextBoundary(nil)
	if err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	engine.Use(boundary)
	engine.GET("/ping", func(c *gin.Context) {
		if c.Request.Header.Get("traceparent") != "" || c.Request.Header.Get("tracestate") != "" {
			c.Status(http.StatusTeapot)
			return
		}
		c.Status(http.StatusNoContent)
	})

	// 无头部：直通。
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/ping", nil))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("无头部应直通，status = %d", recorder.Code)
	}

	// 空可信列表：traceparent 与 tracestate 一并剥离。
	request := httptest.NewRequest(http.MethodGet, "/ping", nil)
	request.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	request.Header.Set("tracestate", "vendor=x")
	recorder = httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("不可信来源的 Trace Context 应被剥离，status = %d", recorder.Code)
	}

	// 非法配置：构造失败。
	if _, err := TraceContextBoundary([]string{"not-an-ip"}); err == nil {
		t.Fatal("非法 CIDR/IP 必须构造失败")
	}
}
