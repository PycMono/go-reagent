package agentbundle

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFreezeCandidateRejectsUncheckpointedChanges(t *testing.T) {
	ctx := context.Background()
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "AGENTS.md"), []byte("Be helpful.\n"), 0644); err != nil {
		t.Fatal(err)
	}
	ref, err := store.CreateInitial(ctx, "tenant", "agent", "v1", source)
	if err != nil {
		t.Fatal(err)
	}
	root, err := store.CreateCandidate(ctx, "tenant", "agent", "training", ref)
	if err != nil {
		t.Fatal(err)
	}
	c := Candidate{TenantID: "tenant", AgentID: "agent", TrainingID: "training", Head: ref.Commit}
	frozen, err := store.FreezeCandidate(ctx, c, "validation-1")
	if err != nil {
		t.Fatal(err)
	}
	if frozen.Commit != c.Head {
		t.Fatal("freeze changed checkpoint")
	}
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("dirty\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FreezeCandidate(ctx, c, "validation-2"); err == nil {
		t.Fatal("dirty candidate frozen")
	}
}
