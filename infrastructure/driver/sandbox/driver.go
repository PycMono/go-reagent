// Package sandbox 是组合根的平台沙箱装配点。它用固定的平台后端替换 SDK
// 默认 HostRunner，并在启动时执行完整后端探针。
package sandbox

import (
	"context"

	harnesssandbox "github.com/PycMono/go-reagent/pi/harness/sandbox"
	"github.com/PycMono/go-reagent/pi/harness/tools"
	"go.uber.org/fx"
)

var Register = fx.Options(
	fx.Decorate(chooseRunner),
	fx.Invoke(forceInstantiateRunner),
)

func chooseRunner(root tools.Root) (harnesssandbox.Runner, error) {
	return harnesssandbox.NewRunner(string(root))
}

// forceInstantiateRunner 强制实例化 Runner，并在 OnStart 执行生产参数探针。
func forceInstantiateRunner(lc fx.Lifecycle, runner harnesssandbox.Runner) {
	lc.Append(fx.Hook{OnStart: func(ctx context.Context) error {
		switch r := runner.(type) {
		case *harnesssandbox.SeatbeltRunner:
			return harnesssandbox.ProbeSeatbelt(ctx, r)
		case *harnesssandbox.BubblewrapRunner:
			return harnesssandbox.ProbeBubblewrap(ctx, r)
		default:
			return nil
		}
	}})
}
