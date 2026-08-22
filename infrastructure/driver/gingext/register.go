package gingext

import (
	"context"

	ginsdk "github.com/PycMono/go-gin-sdk"
	"go.uber.org/fx"
)

// Register 提供 Gin Engine 与 HTTP Server 的 Fx 装配（与
// infrastructure/driver 下其他驱动的 register.go 约定一致）。
var Register = fx.Options(
	fx.Provide(NewEngine, NewHTTPServer),
	fx.Invoke(registerLifecycle),
)

func registerLifecycle(lifecycle fx.Lifecycle, server *ginsdk.HTTPServer) {
	lifecycle.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			go server.Serve(ctx)
			return nil
		},
		OnStop: server.Shutdown,
	})
}
