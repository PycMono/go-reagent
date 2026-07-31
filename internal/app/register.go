package app

import "go.uber.org/fx"

// Register provides the one-shot runner and binds it to the Fx lifecycle.
var Register = fx.Options(
	fx.Provide(NewAgentRunner),
	fx.Invoke(RegisterAgentLifecycle),
)
