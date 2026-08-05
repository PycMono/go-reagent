package workspace

import (
	"github.com/PycMono/go-reagent/pi/agent"
	"go.uber.org/fx"
)

// Module provides workspace-bound prompt and Skill components.
var Module = fx.Options(
	fx.Provide(
		newPromptComposer,
		newSkillLoader,
		fx.Annotate(NewRunContextFactory, fx.As(new(agent.ContextFactory))),
	),
)

func newPromptComposer(workDir WorkDir) *PromptComposer {
	return NewPromptComposer(string(workDir))
}

func newSkillLoader(workDir WorkDir) *SkillLoader {
	return NewSkillLoader(string(workDir))
}
