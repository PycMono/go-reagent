package agentbundle

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCreateInitialCommitsCompleteValidatedTree(t *testing.T) {
	store := mustStore(t)
	source := validSource(t)
	if err := os.WriteFile(filepath.Join(source, ".gitignore"), []byte("ignored.txt\n"), 0o600); err != nil { t.Fatal(err) }
	if err := os.WriteFile(filepath.Join(source, "ignored.txt"), []byte("still included\n"), 0o600); err != nil { t.Fatal(err) }
	ref, err := store.CreateInitial(context.Background(), "tenant", "agent", "v1", source)
	if err != nil { t.Fatal(err) }
	if ref.Commit == "" || ref.Tag == "" || ref.Digest == "" { t.Fatalf("ref = %#v", ref) }
	if err := store.Verify(context.Background(), "tenant", "agent", ref); err != nil { t.Fatal(err) }
	if err := os.WriteFile(filepath.Join(source, "AGENTS.md"), []byte("changed\n"), 0o600); err != nil { t.Fatal(err) }
	if err := store.Verify(context.Background(), "tenant", "agent", ref); err != nil { t.Fatalf("source mutation affected commit: %v", err) }
}

func TestCreateInitialRejectsUnsafeIDsAndTrees(t *testing.T) {
	store := mustStore(t)
	for _, id := range []string{"", ".", "..", "a/b", ".git", "scratch", ".tmp"} {
		if _, err := store.CreateInitial(context.Background(), id, "agent", "v1", validSource(t)); err == nil { t.Fatalf("unsafe ID %q accepted", id) }
	}
	source := validSource(t)
	if err := os.Symlink("../escape", filepath.Join(source, "escape")); err != nil { t.Fatal(err) }
	if _, err := store.CreateInitial(context.Background(), "tenant", "agent", "v1", source); err == nil { t.Fatal("escaping symlink accepted") }
}

func mustStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(t.TempDir())
	if err != nil { t.Fatal(err) }
	return s
}

func validSource(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("You are useful.\n"), 0o600); err != nil { t.Fatal(err) }
	return dir
}
