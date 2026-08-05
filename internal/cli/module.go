package cli

import (
	"github.com/PycMono/go-reagent/conversation"
	"github.com/PycMono/go-reagent/internal/cli/app"
	"github.com/PycMono/go-reagent/internal/cli/dispatch"
	persistencemysql "github.com/PycMono/go-reagent/persistence/mysql"
	"go.uber.org/fx"
)

// Module is the bundled command's private configuration, persistence, reporting, and lifecycle graph.
var Module = fx.Options(
	fx.Provide(NewConfig, NewPlatform, NewWorkDir, NewPrompt),
	persistencemysql.Module,
	conversation.Module,
	dispatch.Module,
	app.Module,
)
