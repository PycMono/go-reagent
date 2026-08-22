package gingext

import (
	"time"

	ginsdk "github.com/PycMono/go-gin-sdk"
	sdkmiddleware "github.com/PycMono/go-gin-sdk/middleware"
	logsdk "github.com/PycMono/go-logger-sdk"
	"github.com/PycMono/go-reagent/config"
	mw "github.com/PycMono/go-reagent/infrastructure/middleware"
	"github.com/gin-gonic/gin"
)

// NewEngine initializes the local Web server's security and recovery chain.
//
// 可观测性链按设计 §16.4 安装一次：Trace Context Boundary → Tracing →
// Metrics。Telemetry 关闭时全局 Provider 为 Noop，Span 不产生导出，
// 业务行为不变；Metrics 中间件写入 Runtime 的 Noop Manager。
func NewEngine(conf *config.Config) (*gin.Engine, error) {
	boundary, err := mw.TraceContextBoundary(conf.Observability.Tracing.TrustedUpstreams)
	if err != nil {
		return nil, err
	}
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(boundary)
	router.Use(sdkmiddleware.Tracing())
	router.Use(sdkmiddleware.Metrics())
	router.Use(requestLogger())
	router.Use(mw.SameOrigin())
	router.Use(mw.Visitor(conf))
	return router, nil
}

func NewHTTPServer(router *gin.Engine, conf *config.Config) *ginsdk.HTTPServer {
	return ginsdk.NewHTTPServer(router, &ginsdk.ServerOptions{
		Host: conf.HTTP.Host, Port: conf.HTTP.Port,
		ReadTimeout:  time.Duration(conf.HTTP.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(conf.HTTP.WriteTimeout) * time.Second,
	})
}

func requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		startedAt := time.Now()
		c.Next()
		logsdk.Info(c.Request.Context(), "http request",
			logsdk.Any("method", c.Request.Method),
			logsdk.Any("path", c.Request.URL.Path),
			logsdk.Any("status", c.Writer.Status()),
			logsdk.Any("latency_ms", time.Since(startedAt).Milliseconds()),
		)
	}
}
