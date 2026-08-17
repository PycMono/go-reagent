package redis

import "go.uber.org/fx"

var Register = fx.Options(
	fx.Provide(NewClient),
	fx.Invoke(RegisterLifecycle),
)
