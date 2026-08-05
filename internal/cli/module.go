package cli

import (
	"github.com/PycMono/go-reagent/internal/cli/app"
	"github.com/PycMono/go-reagent/internal/cli/conversation"
	conversationmysql "github.com/PycMono/go-reagent/internal/cli/conversation/mysql"
	"github.com/PycMono/go-reagent/internal/cli/dispatch"
	drivermysql "github.com/PycMono/go-reagent/internal/cli/driver/mysql"
	"go.uber.org/fx"
)

// Module is the bundled command's private configuration, persistence, reporting, and lifecycle graph.
var Module = fx.Options(
	fx.Provide(NewConfig, NewPlatform, NewWorkDir, NewPrompt),
	drivermysql.Module,
	conversationmysql.Module,
	conversation.Module,
	dispatch.Module,
	app.Module,
)
