package agentbundle

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateInitialCommitsCompleteValidatedTree(t *testing.T) {
	store := mustStore(t)
	source := validSource(t)
	if err := os.MkdirAll(filepath.Join(source, "documents"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "documents", ".gitignore"), []byte("ignored.txt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "documents", "ignored.txt"), []byte("still included\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeSkillScript(t, source)
	if err := os.Symlink("../AGENTS.md", filepath.Join(source, "documents", "instructions")); err != nil {
		t.Fatal(err)
	}
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

func TestCreateInitialRejectsFilesOutsideFixedBundleSchema(t *testing.T) {
	cases := map[string]func(*testing.T, string){
		"arbitrary root file": func(t *testing.T, root string) {
			_ = os.WriteFile(filepath.Join(root, "README.md"), []byte("x"), 0o600)
		},
		"nested git": func(t *testing.T, root string) {
			_ = os.MkdirAll(filepath.Join(root, "documents", ".git"), 0o700)
			_ = os.WriteFile(filepath.Join(root, "documents", ".git", "config"), []byte("x"), 0o600)
		},
		"gitmodules": func(t *testing.T, root string) {
			_ = os.WriteFile(filepath.Join(root, ".gitmodules"), []byte("x"), 0o600)
		},
		"gitattributes": func(t *testing.T, root string) {
			_ = os.WriteFile(filepath.Join(root, ".gitattributes"), []byte("x"), 0o600)
		},
		"platform config": func(t *testing.T, root string) {
			_ = os.WriteFile(filepath.Join(root, "agent.yaml"), []byte("x"), 0o600)
		},
		"native ELF": func(t *testing.T, root string) {
			_ = os.MkdirAll(filepath.Join(root, "assets"), 0o700)
			_ = os.WriteFile(filepath.Join(root, "assets", "tool"), []byte{0x7f, 'E', 'L', 'F', 1}, 0o600)
		},
		"native PE": func(t *testing.T, root string) {
			_ = os.MkdirAll(filepath.Join(root, "assets"), 0o700)
			_ = os.WriteFile(filepath.Join(root, "assets", "tool.exe"), []byte{'M', 'Z', 0, 0}, 0o600)
		},
		"native Mach-O": func(t *testing.T, root string) {
			_ = os.MkdirAll(filepath.Join(root, "assets"), 0o700)
			_ = os.WriteFile(filepath.Join(root, "assets", "tool"), []byte{0xfe, 0xed, 0xfa, 0xcf}, 0o600)
		},
		"misplaced script": func(t *testing.T, root string) {
			_ = os.MkdirAll(filepath.Join(root, "documents"), 0o700)
			_ = os.WriteFile(filepath.Join(root, "documents", "run.sh"), []byte("#!/bin/sh\n"), 0o600)
		},
		"misplaced executable": func(t *testing.T, root string) {
			_ = os.MkdirAll(filepath.Join(root, "assets"), 0o700)
			_ = os.WriteFile(filepath.Join(root, "assets", "run"), []byte("text"), 0o700)
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			source := validSource(t)
			setup(t, source)
			if _, err := mustStore(t).CreateInitial(context.Background(), "tenant", "agent", "v1", source); err == nil {
				t.Fatal("unsafe asset accepted")
			}
		})
	}
}

func writeSkillScript(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, "skills", "example", "scripts")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(dir), "SKILL.md"), []byte("---\nname: example\ndescription: Example\n---\nRun script.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "check.sh"), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
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

func TestVerifyRejectsOversizedBlob(t *testing.T) {
	store := mustStore(t)
	ref, err := store.CreateInitial(context.Background(), "tenant", "agent", "v1", validSource(t))
	if err != nil {
		t.Fatal(err)
	}
	large := filepath.Join(t.TempDir(), "large")
	if err := os.WriteFile(large, make([]byte, maxBundleFile+1), 0o600); err != nil {
		t.Fatal(err)
	}
	oid := runGitOutput(t, "", nil, "--git-dir", store.repoPath("tenant", "agent"), "hash-object", "-w", large)
	tree := runGitOutput(t, "", strings.NewReader("100644 blob "+oid+"\tAGENTS.md\n"), "--git-dir", store.repoPath("tenant", "agent"), "mktree")
	commit := runGitOutput(t, "", strings.NewReader("oversized\n"), "--git-dir", store.repoPath("tenant", "agent"), "commit-tree", tree)
	runGitOutput(t, "", nil, "--git-dir", store.repoPath("tenant", "agent"), "update-ref", "refs/tags/versions/v2", commit)
	ref = BundleRef{Commit: commit, Tag: "versions/v2", Digest: "sha256:" + strings.Repeat("a", 64)}
	if err := store.Verify(context.Background(), "tenant", "agent", ref); err == nil {
		t.Fatal("oversized Git blob accepted")
	}
}

func runGitOutput(t *testing.T, dir string, input io.Reader, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Stdin = input
	cmd.Env = gitEnv(t.TempDir(), nil)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
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
