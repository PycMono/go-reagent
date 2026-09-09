package mcp

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/pi/harness/sandbox"
	"github.com/PycMono/go-reagent/pi/internal/workspacepolicy"
)

type recordingRunner struct {
	policy sandbox.Policy
	spec   sandbox.CommandSpec
}

func (r *recordingRunner) Policy() sandbox.Policy { return r.policy }
func (r *recordingRunner) BuildShell(string, sandbox.CommandSpec) (*exec.Cmd, error) {
	return nil, errors.New("not used")
}
func (r *recordingRunner) BuildArgv(_ []string, spec sandbox.CommandSpec) (*exec.Cmd, error) {
	r.spec = spec
	return exec.Command("true"), nil
}

func TestSandboxBuildCommandUsesDeclaredTmpDir(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, backend := range []string{"seatbelt", "bubblewrap"} {
		r := &recordingRunner{policy: sandbox.Policy{
			Backend: backend, WriteMode: "restricted", TmpDir: filepath.Join(root, ".tmp"),
		}}
		build, err := sandboxBuildCommand(r, root, ServerOptions{Name: "probe", Command: "true"})
		if err != nil {
			t.Fatalf("%s sandboxBuildCommand() error = %v", backend, err)
		}
		if _, err := build(); err != nil {
			t.Fatalf("%s build() error = %v", backend, err)
		}
		if !strings.Contains(strings.Join(r.spec.PayloadEnv, "\n"), "TMPDIR="+filepath.Join(root, ".tmp")) {
			t.Fatalf("%s payload env = %v", backend, r.spec.PayloadEnv)
		}
	}
}

func TestSandboxBuildCommandRejectsInvalidInputs(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	base := sandbox.Policy{Backend: "seatbelt", WriteMode: "restricted", TmpDir: filepath.Join(root, ".tmp")}
	for _, tt := range []struct {
		name   string
		policy sandbox.Policy
		option ServerOptions
	}{
		{name: "HOME override", policy: base, option: ServerOptions{Name: "probe", Command: "true", Env: map[string]string{"HOME": "/bad"}}},
		{name: "PATH override", policy: base, option: ServerOptions{Name: "probe", Command: "true", Env: map[string]string{"PATH": "/bad"}}},
		{name: "TMPDIR override", policy: base, option: ServerOptions{Name: "probe", Command: "true", Env: map[string]string{"TMPDIR": "/bad"}}},
		{name: "outside cwd", policy: base, option: ServerOptions{Name: "probe", Command: "true", CWD: outside}},
		{name: "unknown backend", policy: sandbox.Policy{Backend: "unknown", WriteMode: "all"}, option: ServerOptions{Name: "probe", Command: "true"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := sandboxBuildCommand(&recordingRunner{policy: tt.policy}, root, tt.option); err == nil {
				t.Fatal("sandboxBuildCommand() accepted invalid input")
			}
		})
	}
}

func TestNativeMCPCommandCannotModifyBundle(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("requires OS sandbox")
	}
	if os.Getenv("RUN_WORKSPACE_SANDBOX_INTEGRATION") != "1" {
		t.Skip("native suite not requested")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	n, err := workspacepolicy.Normalize(root, workspacepolicy.Policy{
		WriteMode: workspacepolicy.Restricted, WritablePrefixes: []string{".tmp", "scratch"},
	})
	if err != nil {
		t.Fatal(err)
	}
	r, err := sandbox.NewRunnerWithPolicy(root, n, true)
	if err != nil {
		t.Fatal(err)
	}
	build, err := sandboxBuildCommand(r, root, ServerOptions{
		Name: "write-probe", Transport: "stdio", Command: "/bin/sh",
		Args: []string{"-c", "printf changed > AGENTS.md"},
	})
	if err != nil {
		t.Fatal(err)
	}
	cmd, err := build()
	if err != nil {
		t.Fatal(err)
	}
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("MCP wrote Bundle: %s", out)
	}
	b, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil || string(b) != "original" {
		t.Fatalf("changed: %q %v", b, err)
	}
}
