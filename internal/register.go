package internal

import (
	"github.com/PycMono/go-reagent/internal/app"
	"github.com/PycMono/go-reagent/internal/bootstrap"
	"github.com/PycMono/go-reagent/internal/config"
	"github.com/PycMono/go-reagent/internal/conversation"
	conversationmysql "github.com/PycMono/go-reagent/internal/conversation/mysql"
	"github.com/PycMono/go-reagent/internal/dispatch"
	drivermysql "github.com/PycMono/go-reagent/internal/driver/mysql"
	"github.com/PycMono/go-reagent/internal/engine"
	"go.uber.org/fx"
)

// Register is the complete go-reagent dependency graph.
var Register = fx.Options(
	config.Register,
	bootstrap.Module,
	drivermysql.Register,
	dispatch.Register,
	engine.Register,
	conversationmysql.Register,
	conversation.Register,
	app.Register,
)
