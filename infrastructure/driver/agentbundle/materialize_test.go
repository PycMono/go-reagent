package agentbundle

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestMaterializeVersionAndChatAreImmutableAndIsolated(t *testing.T) {
	store := mustStore(t)
	ref, err := store.CreateInitial(context.Background(), "tenant", "agent", "v1", validSource(t))
	if err != nil { t.Fatal(err) }
	version, err := store.MaterializeVersion(context.Background(), "tenant", "agent", "v1", ref)
	if err != nil { t.Fatal(err) }
	if err := os.WriteFile(filepath.Join(version, "AGENTS.md"), []byte("changed"), 0o600); err == nil { t.Fatal("version behavior file is writable") }
	chat1, err := store.MaterializeChat(context.Background(), "tenant", "agent", "conversation-1", "v1", ref)
	if err != nil { t.Fatal(err) }
	chat2, err := store.MaterializeChat(context.Background(), "tenant", "agent", "conversation-2", "v1", ref)
	if err != nil { t.Fatal(err) }
	for _, root := range []string{chat1, chat2} {
		for _, name := range []string{"scratch", ".tmp"} {
			info, err := os.Stat(filepath.Join(root, name)); if err != nil || info.Mode().Perm()&0o200 == 0 { t.Fatalf("%s/%s not writable: %v", root, name, err) }
		}
	}
	if err := os.WriteFile(filepath.Join(chat1, "scratch", "private"), []byte("one"), 0o600); err != nil { t.Fatal(err) }
	if _, err := os.Stat(filepath.Join(chat2, "scratch", "private")); !os.IsNotExist(err) { t.Fatal("chat copies share scratch") }
}

func TestFailedMaterializationKeepsExistingDestination(t *testing.T) {
	store := mustStore(t)
	destination := filepath.Join(store.root, "versions", "tenant", "agent", "v1")
	if err := os.MkdirAll(destination, 0o700); err != nil { t.Fatal(err) }
	if err := os.WriteFile(filepath.Join(destination, "marker"), []byte("existing"), 0o600); err != nil { t.Fatal(err) }
	_, err := store.MaterializeVersion(context.Background(), "tenant", "agent", "v1", BundleRef{Commit: "missing", Tag: "versions/v1", Digest: "sha256:bad"})
	if err == nil { t.Fatal("invalid ref materialized") }
	if b, readErr := os.ReadFile(filepath.Join(destination, "marker")); readErr != nil || string(b) != "existing" { t.Fatalf("existing destination changed: %q %v", b, readErr) }
}
