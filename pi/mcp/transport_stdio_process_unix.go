//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package mcp

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// configureProcessGroup 为 stdio 子进程设置独立进程组，使强制关闭可以
// 终止完整进程树。
func configureProcessGroup(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessTree 向负 PID 发送 SIGKILL，终止完整进程组。
func killProcessTree(process *os.Process) error {
	err := syscall.Kill(-process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}