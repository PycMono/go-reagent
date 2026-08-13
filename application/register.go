package application

import (
	"github.com/PycMono/go-reagent/config"
	"github.com/PycMono/go-reagent/conversation"
	"github.com/PycMono/go-reagent/infrastructure"
	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/transport"
	"go.uber.org/fx"
)

// Register assembles the complete go-reagent service graph.
var Register = fx.Options(
	pi.Register,
	infrastructure.Register,
	conversation.Register,
	transport.Register,
	fx.Provide(
		config.NewFromEnvironment,
		config.NewPlatform,
		NewWorkDir,
		NewPrompt,
		NewAgentRunner,
	),
	fx.Invoke(RegisterAgentLifecycle),
)
