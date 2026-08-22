package observability

import (
	"context"
	"errors"
	"runtime/debug"
	"sync/atomic"
	"time"

	logsdk "github.com/PycMono/go-logger-sdk"
	sdkobservability "github.com/PycMono/go-observability-sdk"
	"github.com/PycMono/go-reagent/config"
	"go.uber.org/fx"
)

// Register 装配进程唯一的 go-observability-sdk Runtime 并接入 Fx 生命周期
// （设计 §12）：OnStart 安装全局对象并启动 Metrics Listener，OnStop 先
// ForceFlush 再 Shutdown。配置非法时 NewRuntime 失败，Fx 启动即中止。
var Register = fx.Options(
	fx.Provide(NewRuntime),
	fx.Invoke(RegisterLifecycle),
)

// NewRuntime 创建唯一 Runtime：Resource、Provider、Exporter、W3C Propagator、
// 私有 Prometheus Registry 与 Metrics Listener 均由 SDK Runtime 拥有，
// 服务层不得再建第二套。
func NewRuntime(conf *config.Config) (*sdkobservability.Runtime, error) {
	definitions, err := DomainDefinitions()
	if err != nil {
		return nil, err
	}
	return sdkobservability.New(
		context.Background(),
		MapConfig(conf.Observability, serviceVersion()),
		sdkobservability.WithMetricDefinitions(definitions...),
		sdkobservability.WithForbiddenLabelKeys(ForbiddenLabelKeys()...),
		sdkobservability.WithErrorHandler(newRateLimitedErrorHandler(5*time.Second)),
	)
}

// RegisterLifecycle 把 Runtime 生命周期挂到 Fx：InstallGlobal 必须先于
// Start，保证 Provider 与 Metrics Endpoint 指向同一 Runtime。
func RegisterLifecycle(lifecycle fx.Lifecycle, runtime *sdkobservability.Runtime) {
	lifecycle.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			if err := runtime.InstallGlobal(); err != nil {
				return err
			}
			return runtime.Start(ctx)
		},
		OnStop: func(ctx context.Context) error {
			return errors.Join(runtime.ForceFlush(ctx), runtime.Shutdown(ctx))
		},
	})
}

// serviceVersion 取 Go Build Info 的模块版本或 VCS revision，缺省 "dev"。
func serviceVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	if info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" && setting.Value != "" {
			return setting.Value
		}
	}
	return "dev"
}

// newRateLimitedErrorHandler 把运行期遥测错误限频写入结构化日志（§12：
// 错误日志限频），窗口内被抑制的错误计数随下一条日志补发。
func newRateLimitedErrorHandler(interval time.Duration) sdkobservability.ErrorHandler {
	var lastLogged atomic.Int64
	var suppressed atomic.Int64
	return func(ctx context.Context, err error) {
		now := time.Now().UnixNano()
		previous := lastLogged.Load()
		if previous != 0 && time.Duration(now-previous) < interval {
			suppressed.Add(1)
			return
		}
		if !lastLogged.CompareAndSwap(previous, now) {
			suppressed.Add(1)
			return
		}
		dropped := suppressed.Swap(0)
		fields := logsdk.Any("error", err.Error())
		if dropped > 0 {
			fields["suppressed_count"] = dropped
		}
		logsdk.Warn(ctx, "observability runtime error", fields)
	}
}
