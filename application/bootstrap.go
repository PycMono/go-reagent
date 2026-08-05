package application

import (
	"github.com/PycMono/go-reagent/config"
	"github.com/PycMono/go-reagent/conversation"
	persistencemysql "github.com/PycMono/go-reagent/persistence/mysql"
	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/transport"
	"go.uber.org/fx"
)

// Module is the complete go-reagent business-service graph.
var Module = fx.Options(
	pi.Module,
	persistencemysql.Module,
	conversation.Module,
	transport.Module,
	fx.Provide(
		config.NewFromEnvironment,
		config.NewPlatform,
		NewWorkDir,
		NewPrompt,
		NewAgentRunner,
	),
	fx.Invoke(RegisterAgentLifecycle),
)
