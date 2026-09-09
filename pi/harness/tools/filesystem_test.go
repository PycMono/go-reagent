package tools

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"

	pierrors "github.com/PycMono/go-reagent/pi/errors"
	"github.com/PycMono/go-reagent/pi/internal/workspacepolicy"
)

func TestWorkspaceRestrictedCannotTruncateBundle(t *testing.T) {
	root := t.TempDir()
	bundle := filepath.Join(root, "AGENTS.md")
	writeTestFile(t, bundle, []byte("original"))
	w := newRestrictedWorkspaceForTest(t, root, "scratch")

	f, err := w.OpenFile("AGENTS.md", os.O_WRONLY|os.O_TRUNC, 0o600)
	if f != nil {
		_ = f.Close()
	}
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("want permission error, got %v", err)
	}
	assertFileContents(t, bundle, "original")

	if err := w.MkdirAll("scratch/nested", 0o700); err != nil {
		t.Fatal(err)
	}
	f, err = w.OpenFile("scratch/nested/result", os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceRestrictedRejectsEveryMutatingOpenFlagOutsidePrefixes(t *testing.T) {
	flags := []struct {
		name string
		flag int
	}{
		{"write-only", os.O_WRONLY},
		{"read-write", os.O_RDWR},
		{"append", os.O_APPEND},
		{"create", os.O_CREATE},
		{"truncate", os.O_TRUNC},
	}
	for _, tc := range flags {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			bundle := filepath.Join(root, "bundle")
			writeTestFile(t, bundle, []byte("original"))
			w := newRestrictedWorkspaceForTest(t, root, "scratch")
			f, err := w.OpenFile("bundle", tc.flag, 0o600)
			if f != nil {
				_ = f.Close()
			}
			if !errors.Is(err, fs.ErrPermission) {
				t.Fatalf("OpenFile flag %d error = %v, want permission", tc.flag, err)
			}
			assertFileContents(t, bundle, "original")
		})
	}
}

func TestWorkspaceRestrictedConfinesDirectoryMutations(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "bundle"), []byte("original"))
	w := newRestrictedWorkspaceForTest(t, root, "scratch")

	if err := w.MkdirAll("scratch", 0o700); err != nil {
		t.Fatalf("MkdirAll prefix: %v", err)
	}
	for _, path := range []string{"readonly/new", "scratch-evil/new"} {
		if err := w.MkdirAll(path, 0o700); !errors.Is(err, fs.ErrPermission) {
			t.Fatalf("MkdirAll(%q) error = %v, want permission", path, err)
		}
	}
	if err := w.Remove("scratch"); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("Remove(prefix) error = %v, want permission", err)
	}
	if err := w.Remove("bundle"); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("Remove(bundle) error = %v, want permission", err)
	}
	assertFileContents(t, filepath.Join(root, "bundle"), "original")
	if _, err := os.Stat(filepath.Join(root, "scratch")); err != nil {
		t.Fatalf("writable prefix removed: %v", err)
	}
}

func TestWorkspaceRestrictedWriteRootRejectsLinkToReadonlySibling(t *testing.T) {
	root := t.TempDir()
	bundle := filepath.Join(root, "AGENTS.md")
	writeTestFile(t, bundle, []byte("original"))
	w := newRestrictedWorkspaceForTest(t, root, "scratch")
	if err := os.Symlink("../AGENTS.md", filepath.Join(root, "scratch", "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink("..", filepath.Join(root, "scratch", "dir-link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	f, err := w.OpenFile("scratch/link", os.O_WRONLY|os.O_TRUNC, 0o600)
	if f != nil {
		_ = f.Close()
	}
	if err == nil {
		t.Fatal("OpenFile through link error = nil")
	}
	if err := w.MkdirAll("scratch/dir-link/new", 0o700); err == nil {
		t.Fatal("MkdirAll through link error = nil")
	}
	if err := w.Remove("scratch/link"); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("Remove(link) error = %v, want permission", err)
	}
	assertFileContents(t, bundle, "original")
	if _, err := os.Stat(filepath.Join(root, "new")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("readonly directory changed: %v", err)
	}
}

func TestWorkspaceRestrictedKeepsIndependentRootWhenPrefixPathIsReplaced(t *testing.T) {
	root := t.TempDir()
	bundle := filepath.Join(root, "AGENTS.md")
	writeTestFile(t, bundle, []byte("original"))
	w := newRestrictedWorkspaceForTest(t, root, "scratch")

	oldScratch := filepath.Join(root, "detached-scratch")
	if err := os.Rename(filepath.Join(root, "scratch"), oldScratch); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(".", filepath.Join(root, "scratch")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	f, err := w.OpenFile("scratch/result", os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("OpenFile through retained root: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	assertFileContents(t, bundle, "original")
	if _, err := os.Stat(filepath.Join(oldScratch, "result")); err != nil {
		t.Fatalf("write did not use retained prefix root: %v", err)
	}
}

func TestNewWorkspaceWithPolicyValidatesPolicyAndLegacyAllowsAllWrites(t *testing.T) {
	root := t.TempDir()
	if _, err := NewWorkspaceWithPolicy(Root(root), nil); err == nil {
		t.Fatal("nil policy error = nil")
	}
	other := t.TempDir()
	n, err := workspacepolicy.Normalize(other, workspacepolicy.Policy{WriteMode: workspacepolicy.Restricted})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewWorkspaceWithPolicy(Root(root), n); err == nil {
		t.Fatal("mismatched policy root error = nil")
	}

	w := newWorkspaceForTest(t, root)
	f, err := w.OpenFile("legacy", os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("legacy OpenFile: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.MkdirAll("legacy-dir", 0o700); err != nil {
		t.Fatalf("legacy MkdirAll: %v", err)
	}
	if err := w.Remove("legacy"); err != nil {
		t.Fatalf("legacy Remove: %v", err)
	}
}

func TestNewWorkspaceWithPolicyRejectsPrefixLinksCreatedAfterNormalization(t *testing.T) {
	for _, tc := range []struct {
		name   string
		prefix string
		link   string
	}{
		{name: "final prefix", prefix: "scratch", link: "scratch"},
		{name: "prefix ancestor", prefix: "cache/work", link: "cache"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			readonly := filepath.Join(root, "readonly")
			if err := os.Mkdir(readonly, 0o700); err != nil {
				t.Fatal(err)
			}
			bundle := filepath.Join(readonly, "bundle")
			writeTestFile(t, bundle, []byte("original"))
			n, err := workspacepolicy.Normalize(root, workspacepolicy.Policy{
				WriteMode: workspacepolicy.Restricted, WritablePrefixes: []string{tc.prefix},
			})
			if err != nil {
				t.Fatal(err)
			}
			before := directoryNames(t, readonly)
			if err := os.Symlink("readonly", filepath.Join(root, tc.link)); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}

			w, err := NewWorkspaceWithPolicy(Root(root), n)
			if w != nil {
				_ = w.Close()
			}
			if err == nil {
				t.Fatal("NewWorkspaceWithPolicy accepted link introduced after normalization")
			}
			assertFileContents(t, bundle, "original")
			after := directoryNames(t, readonly)
			if !slices.Equal(after, before) {
				t.Fatalf("readonly directory changed: before=%v after=%v", before, after)
			}
		})
	}
}

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
			if _, err := workspace.ResolveDir(path); err == nil {
				t.Fatalf("ResolveDir(%q) error = nil", path)
			}
		})
	}
}

func TestNewWorkspaceClassifiesInvalidWorkDir(t *testing.T) {
	_, err := NewWorkspace(Root(""))
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
	workspace, err := NewWorkspace(Root(workDir))
	if err != nil {
		t.Fatalf("NewWorkspace() error = %v", err)
	}
	t.Cleanup(func() { _ = workspace.Close() })
	return workspace
}

func newRestrictedWorkspaceForTest(t *testing.T, root string, prefixes ...string) *Workspace {
	t.Helper()
	n, err := workspacepolicy.Normalize(root, workspacepolicy.Policy{
		WriteMode: workspacepolicy.Restricted, WritablePrefixes: prefixes,
	})
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	w, err := NewWorkspaceWithPolicy(Root(root), n)
	if err != nil {
		t.Fatalf("NewWorkspaceWithPolicy() error = %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w
}

func assertFileContents(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("%s = %q, want %q", path, got, want)
	}
}

func directoryNames(t *testing.T, path string) []string {
	t.Helper()
	entries, err := os.ReadDir(path)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(entries))
	for i, entry := range entries {
		names[i] = entry.Name()
	}
	return names
}
