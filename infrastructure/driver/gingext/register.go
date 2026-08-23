package gingext

import (
	"go.uber.org/fx"
)

// Register 提供 Gin Engine 与 HTTP Server 的 Fx 装配（与
// infrastructure/driver 下其他驱动的 register.go 约定一致）。
// 进程级生命周期（Serve/Shutdown）由 cmd/server 入口集中管理，
// 不属于本驱动的装配职责。
var Register = fx.Options(
	fx.Provide(NewEngine, NewHTTPServer, NewSessionManager),
)
