package tools

import (
	"runtime"
	"testing"
)

func TestShellInvocationIncludesCommand(t *testing.T) {
	shell, arguments := ShellInvocation("printf hello")
	if shell == "" || len(arguments) == 0 {
		t.Fatalf("ShellInvocation() = %q, %#v", shell, arguments)
	}
	if got := arguments[len(arguments)-1]; got != "printf hello" {
		t.Fatalf("last argument = %q", got)
	}
	if runtime.GOOS == "windows" && shell != "cmd.exe" {
		t.Fatalf("Windows shell = %q", shell)
	}
}
