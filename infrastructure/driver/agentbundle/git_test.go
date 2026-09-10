package agentbundle

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateInitialCommitsCompleteValidatedTree(t *testing.T) {
	store := mustStore(t)
	source := validSource(t)
	if err := os.WriteFile(filepath.Join(source, ".gitignore"), []byte("ignored.txt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "ignored.txt"), []byte("still included\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "script.sh"), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil { t.Fatal(err) }
	if err := os.Symlink("AGENTS.md", filepath.Join(source, "instructions")); err != nil { t.Fatal(err) }
	ref, err := store.CreateInitial(context.Background(), "tenant", "agent", "v1", source)
	if err != nil {
		t.Fatal(err)
	}
	if ref.Commit == "" || ref.Tag == "" || ref.Digest == "" {
		t.Fatalf("ref = %#v", ref)
	}
	if err := store.Verify(context.Background(), "tenant", "agent", ref); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "AGENTS.md"), []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.Verify(context.Background(), "tenant", "agent", ref); err != nil {
		t.Fatalf("source mutation affected commit: %v", err)
	}
}

func TestCreateInitialRejectsUnsafeIDsAndTrees(t *testing.T) {
	store := mustStore(t)
	for _, id := range []string{"", ".", "..", "a/b", ".git", "scratch", ".tmp"} {
		if _, err := store.CreateInitial(context.Background(), id, "agent", "v1", validSource(t)); err == nil {
			t.Fatalf("unsafe ID %q accepted", id)
		}
	}
	source := validSource(t)
	if err := os.Symlink("../escape", filepath.Join(source, "escape")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateInitial(context.Background(), "tenant", "agent", "v1", source); err == nil {
		t.Fatal("escaping symlink accepted")
	}
}

func TestCreateInitialRejectsDirtyGitSource(t *testing.T) {
	store := mustStore(t)
	source := validSource(t)
	runGitTest(t, source, "init")
	runGitTest(t, source, "add", "AGENTS.md")
	runGitTest(t, source, "-c", "user.name=test", "-c", "user.email=test@example.invalid", "commit", "-m", "initial")
	if err := os.WriteFile(filepath.Join(source, "untracked"), []byte("not committed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateInitial(context.Background(), "tenant", "agent", "v1", source); err == nil {
		t.Fatal("dirty Git source accepted")
	}
}

func TestVerifyRejectsMissingBlob(t *testing.T) {
	store := mustStore(t)
	ref, err := store.CreateInitial(context.Background(), "tenant", "agent", "v1", validSource(t))
	if err != nil {
		t.Fatal(err)
	}
	oid, err := store.run(context.Background(), "", nil, "--git-dir", store.repoPath("tenant", "agent"), "rev-parse", ref.Commit+":AGENTS.md")
	if err != nil {
		t.Fatal(err)
	}
	oid = strings.TrimSpace(oid)
	if err := os.Remove(filepath.Join(store.repoPath("tenant", "agent"), "objects", oid[:2], oid[2:])); err != nil {
		t.Fatal(err)
	}
	if err := store.Verify(context.Background(), "tenant", "agent", ref); err == nil {
		t.Fatal("missing blob accepted")
	}
}

func runGitTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func mustStore(t *testing.T) *Store {
	t.Helper()
	root := t.TempDir()
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err == nil && d.IsDir() {
				_ = os.Chmod(path, 0o700)
			}
			return nil
		})
	})
	return s
}

func validSource(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("You are useful.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}
