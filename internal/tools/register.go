package tools

import "go.uber.org/fx"

// Register provides shared tool resources, grouped tools/middleware, and the Registry.
var Register = fx.Options(
	fx.Provide(
		NewWorkspace,
		NewProcessSupervisor,
		fx.Annotate(NewReadTool, fx.As(new(Tool)), fx.ResultTags(`group:"agent_tools"`)),
		fx.Annotate(NewEditTool, fx.As(new(Tool)), fx.ResultTags(`group:"agent_tools"`)),
		fx.Annotate(NewWriteTool, fx.As(new(Tool)), fx.ResultTags(`group:"agent_tools"`)),
		fx.Annotate(NewApplyPatchTool, fx.As(new(Tool)), fx.ResultTags(`group:"agent_tools"`)),
		fx.Annotate(NewExecTool, fx.As(new(Tool)), fx.ResultTags(`group:"agent_tools"`)),
		fx.Annotate(NewProcessTool, fx.As(new(Tool)), fx.ResultTags(`group:"agent_tools"`)),
		fx.Annotate(newRecoveryMiddlewareRegistration, fx.ResultTags(`group:"tool_middlewares"`)),
		fx.Annotate(newContextMiddlewareRegistration, fx.ResultTags(`group:"tool_middlewares"`)),
		fx.Annotate(newSchemaValidationMiddlewareRegistration, fx.ResultTags(`group:"tool_middlewares"`)),
		fx.Annotate(newLoggingMiddlewareRegistration, fx.ResultTags(`group:"tool_middlewares"`)),
		fx.Annotate(newOutputLimitMiddlewareRegistration, fx.ResultTags(`group:"tool_middlewares"`)),
		fx.Annotate(newEventForwardingMiddlewareRegistration, fx.ResultTags(`group:"tool_middlewares"`)),
		NewRegistry,
	),
)

func newRecoveryMiddlewareRegistration() MiddlewareRegistration {
	return MiddlewareRegistration{Name: "recovery", Order: 10, Middleware: recoveryMiddleware()}
}

func newContextMiddlewareRegistration() MiddlewareRegistration {
	return MiddlewareRegistration{Name: "context", Order: 20, Middleware: contextMiddleware()}
}

func newSchemaValidationMiddlewareRegistration() MiddlewareRegistration {
	return MiddlewareRegistration{Name: "schema_validation", Order: 30, Middleware: schemaValidationMiddleware()}
}

func newLoggingMiddlewareRegistration() MiddlewareRegistration {
	return MiddlewareRegistration{Name: "logging", Order: 40, Middleware: loggingMiddleware()}
}

func newOutputLimitMiddlewareRegistration() MiddlewareRegistration {
	return MiddlewareRegistration{Name: "output_limit", Order: 50, Middleware: outputLimitMiddleware()}
}

func newEventForwardingMiddlewareRegistration() MiddlewareRegistration {
	return MiddlewareRegistration{Name: "event_forwarding", Order: 60, Middleware: eventForwardingMiddleware()}
}
