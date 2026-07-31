package internal

import (
	"github.com/PycMono/go-reagent/internal/app"
	"github.com/PycMono/go-reagent/internal/config"
	ctxpkg "github.com/PycMono/go-reagent/internal/context"
	"github.com/PycMono/go-reagent/internal/dispatch"
	"github.com/PycMono/go-reagent/internal/engine"
	"github.com/PycMono/go-reagent/internal/provider"
	"github.com/PycMono/go-reagent/internal/tools"
	"go.uber.org/fx"
)

// Register is the complete go-reagent dependency graph.
var Register = fx.Options(
	config.Register,
	ctxpkg.Register,
	provider.Register,
	tools.Register,
	dispatch.Register,
	engine.Register,
	app.Register,
)
