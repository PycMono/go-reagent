package mcp

import "go.uber.org/fx"

var Register = fx.Options(
	fx.Provide(NewExtensions),
)
