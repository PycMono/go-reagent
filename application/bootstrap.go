package application

import (
	"github.com/PycMono/go-reagent/config"
	"github.com/PycMono/go-reagent/conversation"
	"github.com/PycMono/go-reagent/infrastructure/driver/mysql"
	conversationpersistence "github.com/PycMono/go-reagent/infrastructure/persistence/conversation"
	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/transport"
	"go.uber.org/fx"
)

// Module is the complete go-reagent business-service graph.
var Module = fx.Options(
	pi.Module,
	mysql.Module,
	conversationpersistence.Module,
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
