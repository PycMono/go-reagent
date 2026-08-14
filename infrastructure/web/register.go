// Package web contains infrastructure that must only be linked into the Web
// executable. Keeping it outside the root infrastructure package prevents the
// CLI from loading Gin and its transitive process-wide initializers.
package web

import (
	"github.com/PycMono/go-reagent/infrastructure"
	"github.com/PycMono/go-reagent/infrastructure/controller"
	"github.com/PycMono/go-reagent/infrastructure/driver/gingext"
	"go.uber.org/fx"
)

var Register = fx.Options(
	infrastructure.Register,
	controller.Register,
	fx.Provide(gingext.NewEngine, gingext.NewHTTPServer),
	fx.Invoke(gingext.RegisterLifecycle),
)
