package app

import "go.uber.org/fx"

// Module is the complete go-reagent dependency graph.
var Module = fx.Options(
	fx.Provide(
		NewConfig,
		NewWorkDir,
		NewLLMProvider,
		NewRegistry,
		NewReporter,
		NewAgentEngine,
		NewPrompt,
		NewAgentRunner,
	),
	fx.Invoke(RegisterAgentLifecycle),
)
