package gingext

import (
	"context"
	"errors"
	"strings"
	"time"

	ginsdk "github.com/PycMono/go-gin-sdk"
	sdkmiddleware "github.com/PycMono/go-gin-sdk/middleware"
	"github.com/PycMono/go-gin-sdk/session"
	logsdk "github.com/PycMono/go-logger-sdk"
	"github.com/PycMono/go-reagent/application/identity"
	"github.com/PycMono/go-reagent/config"
	mw "github.com/PycMono/go-reagent/infrastructure/middleware"
	"github.com/gin-gonic/gin"
	"go.uber.org/fx"
)

// NewEngine initializes the local Web server's security and recovery chain.
//
// 可观测性链按设计 §16.4 安装一次：Tracing → Metrics。Telemetry 关闭时全局
// Provider 为 Noop，Span 不产生导出，业务行为不变；Metrics 中间件写入
// Runtime 的 Noop Manager。
type EngineParams struct {
	fx.In

	Config        *config.Config
	Sessions      *session.Manager
	Authenticator identity.Authenticator `optional:"true"`
}

func NewEngine(params EngineParams) (*gin.Engine, error) {
	return newEngine(params.Config, params.Sessions, params.Authenticator)
}

type visitorSessions interface {
	Load(context.Context, string) (*session.Session, error)
	Create(context.Context, session.Session) (string, error)
}

func newEngine(conf *config.Config, sessions visitorSessions, authenticator identity.Authenticator) (*gin.Engine, error) {
	if conf == nil || conf.Identity.Mode == "" {
		return nil, errors.New("server identity is not configured; set identity.mode to anonymous or host")
	}
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(sdkmiddleware.Tracing())
	router.Use(sdkmiddleware.Metrics())
	router.Use(requestLogger())
	switch conf.Identity.Mode {
	case config.IdentityModeAnonymous:
		principal, err := mw.AnonymousPrincipal(conf.Identity.TenantID)
		if err != nil {
			return nil, err
		}
		router.Use(applicationOnly(mw.Visitor(conf, sessions)))
		router.Use(applicationOnly(principal))
	case config.IdentityModeHost:
		principal, err := mw.HostPrincipal(authenticator)
		if err != nil {
			return nil, err
		}
		router.Use(applicationOnly(principal))
	default:
		return nil, errors.New("server identity mode must be anonymous or host")
	}
	return router, nil
}

func applicationOnly(handler gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.URL.Path == "/health" || c.Request.URL.Path == "/static" || strings.HasPrefix(c.Request.URL.Path, "/static/") {
			c.Next()
			return
		}
		handler(c)
	}
}

// NewSessionManager 创建访客会话管理器；节点号范围已由 config.Load 校验。
// 依赖 go-cache-sdk 的 Redis 客户端（redis driver 以 ClientName "cache" 初始化）。
func NewSessionManager(conf *config.Config) *session.Manager {
	return session.NewManager(int64(conf.SnowflakeNodeID))
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
