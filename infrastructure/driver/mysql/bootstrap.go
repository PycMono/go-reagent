package mysql

import "go.uber.org/fx"

// Module provides the optional MySQL connection.
var Module = fx.Options(fx.Provide(NewConnection))
