package skills

// Loader loads standard Agent Skills from one workspace.
type Loader struct {
	workDir string
}

// NewLoader creates a Skill loader confined to workDir.
func NewLoader(workDir string) *Loader {
	return &Loader{workDir: workDir}
}
