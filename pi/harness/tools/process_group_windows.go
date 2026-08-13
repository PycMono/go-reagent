//go:build windows

package tools

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
)

// ConfigureProcessGroup is a no-op because Windows process trees are terminated with taskkill.
func ConfigureProcessGroup(_ *exec.Cmd) {}

// KillProcessGroup terminates the process tree owned by process.
func KillProcessGroup(process *os.Process) error {
	if err := exec.Command("taskkill", "/PID", strconv.Itoa(process.Pid), "/T", "/F").Run(); err == nil {
		return nil
	}
	err := process.Kill()
	if errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	return err
}
