package pi

// WorkDir is the Agent workspace path injected through the private Fx graph.
type WorkDir string

func newPromptComposer(workDir WorkDir) *PromptComposer {
	return NewPromptComposer(string(workDir))
}

func newSkillLoader(workDir WorkDir) *SkillLoader {
	return NewSkillLoader(string(workDir))
}
