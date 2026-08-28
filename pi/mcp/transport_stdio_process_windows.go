//go:build windows

package mcp

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
)

// configureProcessGroup is a no-op because Windows process trees are terminated with taskkill.
func configureProcessGroup(_ *exec.Cmd) {}

// killProcessTree terminates the process tree owned by process with
// taskkill, falling back to a direct Kill.
func killProcessTree(process *os.Process) error {
	if err := exec.Command("taskkill", "/PID", strconv.Itoa(process.Pid), "/T", "/F").Run(); err == nil {
		return nil
	}
	err := process.Kill()
	if errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	return err
}