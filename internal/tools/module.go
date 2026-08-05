package tools

import (
	"github.com/PycMono/go-reagent/pi/agent"
	"go.uber.org/fx"
)

// Module provides shared tool resources and the six default tool implementations.
var Module = fx.Options(
	fx.Provide(
		NewWorkspace,
		NewProcessSupervisor,
		fx.Annotate(NewReadTool, fx.As(new(agent.Tool)), fx.ResultTags(`group:"agent_tools"`)),
		fx.Annotate(NewEditTool, fx.As(new(agent.Tool)), fx.ResultTags(`group:"agent_tools"`)),
		fx.Annotate(NewWriteTool, fx.As(new(agent.Tool)), fx.ResultTags(`group:"agent_tools"`)),
		fx.Annotate(NewApplyPatchTool, fx.As(new(agent.Tool)), fx.ResultTags(`group:"agent_tools"`)),
		fx.Annotate(NewExecTool, fx.As(new(agent.Tool)), fx.ResultTags(`group:"agent_tools"`)),
		fx.Annotate(NewProcessTool, fx.As(new(agent.Tool)), fx.ResultTags(`group:"agent_tools"`)),
	),
)
