package app

import "go.uber.org/fx"

// Module provides the one-shot runner and binds it to the Fx lifecycle.
var Module = fx.Options(
	fx.Provide(NewAgentRunner),
	fx.Invoke(RegisterAgentLifecycle),
)
