package pi

import "github.com/PycMono/go-reagent/pi/skills"

// WorkDir is the Agent workspace path injected through the private Fx graph.
type WorkDir string

func newPromptComposer(workDir WorkDir) *PromptComposer {
	return NewPromptComposer(string(workDir))
}

func newSkillLoader(workDir WorkDir) *skills.Loader {
	return skills.NewLoader(string(workDir))
}
