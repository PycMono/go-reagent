package gingext

import (
	"context"

	ginsdk "github.com/PycMono/go-gin-sdk"
	"go.uber.org/fx"
)

func RegisterLifecycle(lifecycle fx.Lifecycle, server *ginsdk.HTTPServer) {
	lifecycle.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			go server.Serve(ctx)
			return nil
		},
		OnStop: server.Shutdown,
	})
}
