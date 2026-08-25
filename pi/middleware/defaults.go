package middleware

// Defaults 返回默认中间件集合，切片顺序即执行顺序。
//
// Tracing 在最外层包住整条链与真实 Tool 调用；未注册 Tool 在
// ToolRuntime 入口即返回，不经过本链，不创建执行 Span。
func Defaults() []Handler {
	return []Handler{
		Tracing,
		PanicRecovery,
		SchemaValidation,
		Logging,
		EventForwarding,
	}
}
