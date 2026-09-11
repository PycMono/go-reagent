package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/PycMono/go-reagent/application/service/agenttraining"
	trainingpersistence "github.com/PycMono/go-reagent/infrastructure/persistence/agenttraining"

	ginsdk "github.com/PycMono/go-gin-sdk"
	logsdk "github.com/PycMono/go-logger-sdk"
	sdkobservability "github.com/PycMono/go-observability-sdk"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
)

func main() {
	recoverStopped := flag.Bool("recover-stopped-training", false, "recover training after confirming the old server and all subprocesses have stopped")
	flag.Parse()
	logsdk.SetLogger(newApplicationLogger())
	app := fx.New(newAppOptions(*recoverStopped)...)
	if err := app.Err(); err != nil {
		logsdk.Error(context.Background(), "server bootstrap failed", logsdk.Err(err))
		os.Exit(1)
	}
	// fx 的 OnStart 错误经 fxevent.NopLogger 静默回滚，App.Run 随后直接
	// os.Exit(1)，进程看似"正常结束"却无任何日志。改为手动 Start/Wait/Stop
	// 把启动与退出阶段的错误显式打出来。
	startCtx, cancelStart := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelStart()
	if err := app.Start(startCtx); err != nil {
		logsdk.Error(context.Background(), "server start failed", logsdk.Err(err))
		os.Exit(1)
	}
	sig := <-app.Done()
	logsdk.Info(context.Background(), "server shutting down", logsdk.Any("signal", sig.String()))
	stopCtx, cancelStop := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelStop()
	if err := app.Stop(stopCtx); err != nil {
		logsdk.Error(context.Background(), "server stop failed", logsdk.Err(err))
		os.Exit(1)
	}
}

func newAppOptions(recoverStopped ...bool) []fx.Option {
	return []fx.Option{
		Register,
		fx.Invoke(func(lc fx.Lifecycle, r *trainingpersistence.Repository, s *agenttraining.Service) {
			lc.Append(fx.Hook{OnStart: func(ctx context.Context) error {
				cursor := ""
				for {
					items, err := r.RecoverySessions(ctx, cursor)
					if err != nil {
						return err
					}
					if len(items) == 0 {
						return nil
					}
					for _, item := range items {
						if item.Running() && (len(recoverStopped) == 0 || !recoverStopped[0]) {
							return fmt.Errorf("training %s was interrupted; stop the old server and subprocesses, then use --recover-stopped-training", item.ID)
						}
						if err := s.RecoverStopped(ctx, item); err != nil {
							return err
						}
						cursor = item.ID
					}
				}
			}})
		}),
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
