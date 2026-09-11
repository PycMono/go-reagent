package agentbundle

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func candidateFixture(t *testing.T) (*Store, Candidate, string) {
	t.Helper()
	s := mustStore(t)
	ref, err := s.CreateInitial(context.Background(), "tenant", "agent", "v1", validSource(t))
	if err != nil {
		t.Fatal(err)
	}
	root, err := s.CreateCandidate(context.Background(), "tenant", "agent", "session", ref)
	if err != nil {
		t.Fatal(err)
	}
	return s, Candidate{"tenant", "agent", "session", ref.Commit}, root
}
func TestCandidatePartialHistoryAndRestore(t *testing.T) {
	ctx := context.Background()
	s, c, root := candidateFixture(t)
	if err := os.MkdirAll(filepath.Join(root, "skills", "broken"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "skills", "broken", "SKILL.md"), []byte("invalid frontmatter but safe draft"), 0600); err != nil {
		t.Fatal(err)
	}
	cp, err := s.Checkpoint(ctx, c, CheckpointMetadata{RunID: "run1", ActorID: "admin", At: time.Now(), Partial: true})
	if err != nil {
		t.Fatal(err)
	}
	c.Head = cp.Head
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("revised"), 0600); err != nil {
		t.Fatal(err)
	}
	diff, err := s.Diff(ctx, c, c.Head, 5)
	if err != nil || !diff.Truncated || diff.ChangedPaths != 1 {
		t.Fatalf("diff %#v %v", diff, err)
	}
	later, err := s.Checkpoint(ctx, c, CheckpointMetadata{RunID: "run2", ActorID: "admin", At: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	c.Head = later.Head
	restored, err := s.Restore(ctx, c, cp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !restored.Metadata.Partial {
		t.Fatal("partial lost")
	}
	c.Head = restored.Head
	list, _, err := s.ListCheckpoints(ctx, c, "", 100)
	if err != nil || len(list) != 2 {
		t.Fatalf("history: %#v %v", list, err)
	}
	other := c
	other.TrainingID = "other"
	if _, err := s.checkpoint(ctx, other, cp.ID); err == nil {
		t.Fatal("cross session checkpoint accepted")
	}
}

func TestDiffPreservesCheckpointedChangesAgainstProduction(t *testing.T) {
	ctx := context.Background()
	s, c, root := candidateFixture(t)
	base := c.Head
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("new behavior"), 0600); err != nil {
		t.Fatal(err)
	}
	cp, err := s.Checkpoint(ctx, c, CheckpointMetadata{RunID: "run", ActorID: "admin", At: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	c.Head = cp.Head
	diff, err := s.Diff(ctx, c, base, 1024)
	if err != nil || diff.ChangedPaths != 1 || diff.Text == "" {
		t.Fatalf("lost published-baseline diff: %+v %v", diff, err)
	}
}

func TestRecoveryUsesDatabaseHeadAndPreservesCheckpointHistory(t *testing.T) {
	ctx := context.Background()
	s, c, root := candidateFixture(t)
	before, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("uncommitted database result"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = s.Checkpoint(ctx, c, CheckpointMetadata{RunID: "lost", ActorID: "admin", At: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RecoverCandidate(ctx, c); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil || string(after) != string(before) {
		t.Fatalf("recovery did not restore DB head: %s %v", after, err)
	}
	items, _, err := s.ListCheckpoints(ctx, c, "", 100)
	if err != nil || len(items) != 1 {
		t.Fatalf("lost history: %v %v", items, err)
	}
}

func TestRestoreRollsBackDirectoryWhenHeadCASFails(t *testing.T) {
	ctx := context.Background()
	s, c, root := candidateFixture(t)
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	first, err := s.Checkpoint(ctx, c, CheckpointMetadata{RunID: "first", ActorID: "admin", At: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	c.Head = first.Head
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("second"), 0600); err != nil {
		t.Fatal(err)
	}
	second, err := s.Checkpoint(ctx, c, CheckpointMetadata{RunID: "second", ActorID: "admin", At: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	c.Head = second.Head
	if err := os.WriteFile(filepath.Join(s.repoPath(c.TenantID, c.AgentID), "refs", "training", c.TrainingID, "head.lock"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Restore(ctx, c, first.ID); err == nil {
		t.Fatal("locked HEAD accepted")
	}
	text, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil || string(text) != "second" {
		t.Fatalf("failed restore changed candidate: %s %v", text, err)
	}
}
func TestCandidateRejectsUnsafeActualTree(t *testing.T) {
	for _, kind := range []string{"secret", "binary", "ignored", "tmp", "escape", "git"} {
		t.Run(kind, func(t *testing.T) {
			s, c, root := candidateFixture(t)
			var err error
			switch kind {
			case "secret":
				err = os.WriteFile(filepath.Join(root, "documents", "key"), []byte("api_key=supersecretvalue"), 0600)
			case "binary":
				err = os.WriteFile(filepath.Join(root, "assets", "native"), []byte("\x7fELFxxxx"), 0600)
			case "ignored":
				err = os.WriteFile(filepath.Join(root, "documents", ".gitignore"), []byte("*"), 0600)
				if err == nil {
					err = os.WriteFile(filepath.Join(root, "documents", ".env"), []byte("hidden"), 0600)
				}
			case "tmp":
				err = os.Remove(filepath.Join(root, ".tmp"))
				if err == nil {
					err = os.Symlink(t.TempDir(), filepath.Join(root, ".tmp"))
				}
			case "escape":
				err = os.Symlink(t.TempDir(), filepath.Join(root, "assets", "escape"))
			case "git":
				err = os.Chmod(filepath.Join(root, ".git"), 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.Checkpoint(context.Background(), c, CheckpointMetadata{RunID: "run", ActorID: "admin", At: time.Now()}); err == nil {
				t.Fatal("unsafe candidate accepted")
			}
		})
	}
}
