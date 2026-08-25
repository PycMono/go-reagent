package main

import (
	chatservice "github.com/PycMono/go-reagent/application/service/chat"
	chattools "github.com/PycMono/go-reagent/application/tool/chat"
	"github.com/PycMono/go-reagent/config"
	"github.com/PycMono/go-reagent/conversation"
	agentprofiledriver "github.com/PycMono/go-reagent/infrastructure/driver/agentprofile"
	mcpdriver "github.com/PycMono/go-reagent/infrastructure/driver/mcp"
	"github.com/PycMono/go-reagent/infrastructure/notice"
	infrastructureweb "github.com/PycMono/go-reagent/infrastructure/web"
	"github.com/PycMono/go-reagent/pi"
	"go.uber.org/fx"
)

var Register = fx.Options(
	agentRegister,
	infrastructureweb.Register,
	conversation.Register,
	chatservice.Register,
	mcpdriver.Register,
	notice.Register,
	fx.Provide(
		config.NewFromEnvironment,
		config.NewPlatform,
		config.NewWorkDir,
		config.NewCompactionConfig,
		config.NewExtraToolHandlers,
		agentprofiledriver.NewCatalog,
	),
)

var agentRegister = fx.Options(
	pi.CoreRegister,
	pi.ReadOnlyToolsRegister,
	pi.SubagentRegister,
	chattools.Register,
)
