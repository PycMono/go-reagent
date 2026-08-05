//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package utils

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// ConfigureProcessGroup isolates command so its complete process tree can be terminated.
func ConfigureProcessGroup(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// KillProcessGroup terminates the complete process group owned by process.
func KillProcessGroup(process *os.Process) error {
	if process == nil {
		return nil
	}
	err := syscall.Kill(-process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}
