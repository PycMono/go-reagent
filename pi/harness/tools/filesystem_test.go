package tools

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"

	"go.uber.org/fx/fxtest"
)

func TestWorkspaceRejectsNonRelativePathsForEveryFileOperation(t *testing.T) {
	workspace := newWorkspaceForTest(t, t.TempDir())
	for _, path := range []string{"/tmp/x", "../x", `C:\\x`, `\\\\server\\share`} {
		t.Run(path, func(t *testing.T) {
			if _, err := workspace.Open(path); err == nil {
				t.Fatalf("Open(%q) error = nil", path)
			}
			if _, err := workspace.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
				t.Fatalf("OpenFile(%q) error = nil", path)
			}
			if _, err := workspace.ReadFile(path); err == nil {
				t.Fatalf("ReadFile(%q) error = nil", path)
			}
			if err := workspace.MkdirAll(path, 0o700); err == nil {
				t.Fatalf("MkdirAll(%q) error = nil", path)
			}
			if err := workspace.Remove(path); err == nil {
				t.Fatalf("Remove(%q) error = nil", path)
			}
			if err := workspace.Rename("inside.txt", path); err == nil {
				t.Fatalf("Rename(_, %q) error = nil", path)
			}
			if _, err := workspace.ResolveDir(path); err == nil {
				t.Fatalf("ResolveDir(%q) error = nil", path)
			}
		})
	}
}

func TestNewWorkspaceClassifiesInvalidWorkDir(t *testing.T) {
	_, err := NewWorkspace(fxtest.NewLifecycle(t), Root(""))
	if !errors.Is(err, pierrors.ErrWorkspaceInvalid) {
		t.Fatalf("NewWorkspace() error = %v, want pierrors.ErrWorkspaceInvalid", err)
	}
}

func TestWorkspaceRejectsOutsideSymlinkTargets(t *testing.T) {
	workDir := t.TempDir()
	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "secret.txt")
	writeTestFile(t, outsideFile, []byte("secret"))
	if err := os.Symlink(outsideFile, filepath.Join(workDir, "outside-file")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(outsideDir, filepath.Join(workDir, "outside-dir")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	workspace := newWorkspaceForTest(t, workDir)

	for _, operation := range []struct {
		name string
		run  func() error
	}{
		{"Open", func() error {
			file, err := workspace.Open("outside-file")
			if file != nil {
				_ = file.Close()
			}
			return err
		}},
		{"OpenFile", func() error {
			file, err := workspace.OpenFile("outside-file", os.O_WRONLY, 0)
			if file != nil {
				_ = file.Close()
			}
			return err
		}},
		{"ReadFile", func() error { _, err := workspace.ReadFile("outside-file"); return err }},
		{"MkdirAll", func() error { return workspace.MkdirAll("outside-dir/new", 0o700) }},
		{"Remove", func() error { return workspace.Remove("outside-file") }},
		{"Rename", func() error { return workspace.Rename("outside-file", "renamed") }},
		{"ResolveDir", func() error { _, err := workspace.ResolveDir("outside-dir"); return err }},
	} {
		t.Run(operation.name, func(t *testing.T) {
			if err := operation.run(); err == nil {
				t.Fatal("outside symlink error = nil")
			}
		})
	}
	if _, err := os.Stat(filepath.Join(outsideDir, "new")); !os.IsNotExist(err) {
		t.Fatalf("outside directory was modified: %v", err)
	}
}

func TestWorkspaceUsesLifecycleAndResolvesExistingDirectories(t *testing.T) {
	workDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(workDir, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	workspace := newWorkspaceForTest(t, workDir)

	resolved, err := workspace.ResolveDir("nested")
	want, evalErr := filepath.EvalSymlinks(filepath.Join(workDir, "nested"))
	if evalErr != nil {
		t.Fatal(evalErr)
	}
	if err != nil || resolved != want {
		t.Fatalf("ResolveDir() = %q, %v", resolved, err)
	}
	if _, err := workspace.ResolveDir("missing"); err == nil {
		t.Fatal("ResolveDir(missing) error = nil")
	}
	if _, err := workspace.ResolveDir("inside.txt"); err == nil {
		t.Fatal("ResolveDir(file) error = nil")
	}
}

func newWorkspaceForTest(t *testing.T, workDir string) *Workspace {
	t.Helper()
	lifecycle := fxtest.NewLifecycle(t)
	workspace, err := NewWorkspace(lifecycle, Root(workDir))
	if err != nil {
		t.Fatalf("NewWorkspace() error = %v", err)
	}
	lifecycle.RequireStart()
	t.Cleanup(lifecycle.RequireStop)
	return workspace
}
