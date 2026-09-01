package sandbox

import (
	"errors"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestHostPayloadEnvRejectsPathOverride(t *testing.T) {
	_, err := HostPayloadEnv(map[string]string{"PATH": "/tmp"})
	if err == nil || !strings.Contains(err.Error(), "PATH") {
		t.Fatalf("HostPayloadEnv() error = %v", err)
	}
}

func TestHostPayloadEnvOverridesWithoutDuplicates(t *testing.T) {
	t.Setenv("PI_TEST_VALUE", "host")
	env, err := HostPayloadEnv(map[string]string{"PI_TEST_VALUE": "override"})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range env {
		if strings.HasPrefix(entry, "PI_TEST_VALUE=") {
			count++
			if entry != "PI_TEST_VALUE=override" {
				t.Fatalf("override 未生效: %q", entry)
			}
		}
	}
	if count != 1 {
		t.Fatalf("PI_TEST_VALUE 出现 %d 次", count)
	}
}

func TestHostRunnerRejectsMalformedOrDuplicatePayloadEnv(t *testing.T) {
	runner := NewHostRunner()
	for _, env := range [][]string{{"NO_EQUALS"}, {"A=1", "A=2"}} {
		_, err := runner.BuildArgv([]string{"true"}, CommandSpec{PayloadEnv: env})
		if !errors.Is(err, ErrEnvContractRejected) {
			t.Fatalf("env %v: error = %v", env, err)
		}
	}
}

func TestHostRunnerBuildShellSetsWorkingDirectory(t *testing.T) {
	workDir := t.TempDir()
	env, err := HostPayloadEnv(map[string]string{"PI_TEST_VALUE": "ok"})
	if err != nil {
		t.Fatalf("HostPayloadEnv() error = %v", err)
	}
	runner := NewHostRunner()
	child, err := runner.BuildShell("exit 0", CommandSpec{WorkDir: workDir, PayloadEnv: env})
	if err != nil {
		t.Fatalf("BuildShell() error = %v", err)
	}
	if child.Dir != workDir {
		t.Fatalf("child.Dir = %q, want %q", child.Dir, workDir)
	}
}

func TestShellInvocationIncludesCommand(t *testing.T) {
	shell, arguments := ShellInvocation("printf hello")
	if shell == "" || len(arguments) == 0 {
		t.Fatalf("ShellInvocation() = %q, %#v", shell, arguments)
	}
	if got := arguments[len(arguments)-1]; got != "printf hello" {
		t.Fatalf("last argument = %q", got)
	}
	switch runtime.GOOS {
	case "windows":
		if shell != "cmd.exe" {
			t.Fatalf("Windows shell = %q", shell)
		}
		for _, flag := range []string{"/d", "/s", "/c"} {
			if !slices.Contains(arguments, flag) {
				t.Fatalf("Windows 契约缺少 %q: %#v", flag, arguments)
			}
		}
	default:
		if !slices.Contains(arguments, "-lc") {
			t.Fatalf("Unix 契约缺少 -lc: %#v", arguments)
		}
	}
}
