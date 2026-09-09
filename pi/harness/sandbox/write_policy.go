package sandbox

import "os/exec"

type disabledRunner struct {
	policy Policy
}

func (r *disabledRunner) Policy() Policy {
	return clonePolicy(r.policy)
}

func (r *disabledRunner) BuildShell(string, CommandSpec) (*exec.Cmd, error) {
	return nil, ErrProcessDisabled
}

func (r *disabledRunner) BuildArgv([]string, CommandSpec) (*exec.Cmd, error) {
	return nil, ErrProcessDisabled
}
