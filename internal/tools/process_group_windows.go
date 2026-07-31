//go:build windows

package tools

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
)

func configureProcessGroup(_ *exec.Cmd) {}

func killProcessGroup(process *os.Process) error {
	if process == nil {
		return nil
	}
	if err := exec.Command("taskkill", "/PID", strconv.Itoa(process.Pid), "/T", "/F").Run(); err == nil {
		return nil
	}
	err := process.Kill()
	if errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	return err
}
