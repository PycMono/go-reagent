package main

import (
	"context"
	"errors"

	ginsdk "github.com/PycMono/go-gin-sdk"
	logsdk "github.com/PycMono/go-logger-sdk"
	sdkobservability "github.com/PycMono/go-observability-sdk"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
)

func main() {
	logsdk.SetLogger(newApplicationLogger())
	fx.New(newAppOptions()...).Run()
}

func newAppOptions() []fx.Option {
	return []fx.Option{
		Register,
		fx.Invoke(registerLifecycle),
		fx.WithLogger(func() fxevent.Logger { return fxevent.NopLogger }),
	}
}

// registerLifecycle 集中管理进程级服务的生命周期（对齐 go-open-ecosystem-center
// 入口约定）：OnStart 先 InstallGlobal + Start 遥测运行时（保证 Provider 与
// Metrics Endpoint 指向同一 Runtime），再拉起 HTTP 服务；OnStop 先关停 HTTP
// 服务，再强制 flush 并关闭遥测。
func registerLifecycle(lifecycle fx.Lifecycle, server *ginsdk.HTTPServer, runtime *sdkobservability.Runtime) {
	lifecycle.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			if err := runtime.InstallGlobal(); err != nil {
				return err
			}
			if err := runtime.Start(ctx); err != nil {
				return err
			}
			go server.Serve(ctx)
			return nil
		},
		OnStop: func(ctx context.Context) error {
			return errors.Join(
				server.Shutdown(ctx),
				runtime.ForceFlush(ctx),
				runtime.Shutdown(ctx),
			)
		},
	})
}

func newApplicationLogger() logsdk.Logger {
	return logsdk.NewLogrus(logsdk.Options{LogFormat: "json", Module: "go-reagent-web"})
}
