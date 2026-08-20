package web

import (
	chatservice "github.com/PycMono/go-reagent/application/service/chat"
	chattools "github.com/PycMono/go-reagent/application/tool/chat"
	"github.com/PycMono/go-reagent/config"
	"github.com/PycMono/go-reagent/conversation"
	agentprofiledriver "github.com/PycMono/go-reagent/infrastructure/driver/agentprofile"
	infrastructureweb "github.com/PycMono/go-reagent/infrastructure/web"
	"github.com/PycMono/go-reagent/pi"
	"go.uber.org/fx"
)

var Register = fx.Options(
	agentRegister,
	infrastructureweb.Register,
	conversation.Register,
	chatservice.Register,
	fx.Provide(
		config.NewFromEnvironment,
		config.NewPlatform,
		NewChatWorkDir,
		NewChatCompactionConfig,
		agentprofiledriver.NewCatalog,
		RegisterMCPExtensions,
	),
)

var agentRegister = fx.Options(
	pi.CoreRegister,
	pi.ReadOnlyToolsRegister,
	chattools.Register,
	fx.Supply(pi.ThinkingEnabled(false)),
)
