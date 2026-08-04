package internal

import (
	"github.com/PycMono/go-reagent/internal/app"
	"github.com/PycMono/go-reagent/internal/config"
	ctxpkg "github.com/PycMono/go-reagent/internal/context"
	"github.com/PycMono/go-reagent/internal/conversation"
	conversationmysql "github.com/PycMono/go-reagent/internal/conversation/mysql"
	"github.com/PycMono/go-reagent/internal/dispatch"
	drivermysql "github.com/PycMono/go-reagent/internal/driver/mysql"
	"github.com/PycMono/go-reagent/internal/engine"
	"github.com/PycMono/go-reagent/internal/provider"
	"github.com/PycMono/go-reagent/internal/tools"
	"go.uber.org/fx"
)

// Register is the complete go-reagent dependency graph.
var Register = fx.Options(
	config.Register,
	drivermysql.Register,
	ctxpkg.Register,
	provider.Register,
	tools.Register,
	dispatch.Register,
	engine.Register,
	conversationmysql.Register,
	conversation.Register,
	app.Register,
)
