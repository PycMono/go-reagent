package utils

import (
	"os"
	"runtime"
	"strings"
)

// ShellInvocation returns the platform shell and arguments used to execute command.
func ShellInvocation(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd.exe", []string{"/d", "/s", "/c", command}
	}
	shell := strings.TrimSpace(os.Getenv("SHELL"))
	if shell == "" {
		shell = "/bin/sh"
	}
	return shell, []string{"-lc", command}
}
