package tools

import (
	"github.com/PycMono/go-reagent/agent"
	"go.uber.org/fx"
)

// Register provides shared tool resources, grouped tools/middleware, and the Registry.
var Register = fx.Options(
	fx.Provide(
		NewWorkspace,
		NewProcessSupervisor,
		fx.Annotate(NewReadTool, fx.As(new(agent.Tool)), fx.ResultTags(`group:"agent_tools"`)),
		fx.Annotate(NewEditTool, fx.As(new(agent.Tool)), fx.ResultTags(`group:"agent_tools"`)),
		fx.Annotate(NewWriteTool, fx.As(new(agent.Tool)), fx.ResultTags(`group:"agent_tools"`)),
		fx.Annotate(NewApplyPatchTool, fx.As(new(agent.Tool)), fx.ResultTags(`group:"agent_tools"`)),
		fx.Annotate(NewExecTool, fx.As(new(agent.Tool)), fx.ResultTags(`group:"agent_tools"`)),
		fx.Annotate(NewProcessTool, fx.As(new(agent.Tool)), fx.ResultTags(`group:"agent_tools"`)),
		newRegistry,
	),
)

type registryParams struct {
	fx.In

	Tools []agent.Tool `group:"agent_tools"`
}

func newRegistry(params registryParams) (agent.Registry, error) {
	return agent.NewRegistry(agent.RegistryOptions{
		Tools:       params.Tools,
		Middlewares: agent.DefaultMiddlewareRegistrations(),
	})
}
