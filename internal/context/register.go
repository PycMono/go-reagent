package context

import (
	"github.com/PycMono/go-reagent/internal/config"
	"go.uber.org/fx"
)

// Register provides workspace-bound prompt and Skill components.
var Register = fx.Options(
	fx.Provide(
		newPromptComposer,
		newSkillLoader,
		NewRunContextFactory,
	),
)

func newPromptComposer(workDir config.WorkDir) *PromptComposer {
	return NewPromptComposer(string(workDir))
}

func newSkillLoader(workDir config.WorkDir) *SkillLoader {
	return NewSkillLoader(string(workDir))
}
