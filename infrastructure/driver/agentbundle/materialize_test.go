package agentbundle

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/infrastructure/driver/agentprofile"
	"github.com/PycMono/go-reagent/infrastructure/driver/agenttemplate"
	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/harness/sandbox"
)

func TestMaterializeRealGeneralTemplate(t *testing.T) {
	work, err := filepath.Abs("../../../workspaces/chat")
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := agentprofile.NewCatalog(pi.WorkDir(work))
	if err != nil {
		t.Fatal(err)
	}
	assembler, err := agenttemplate.New(work, catalog)
	if err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	if err := assembler.Assemble(context.Background(), "general", source); err != nil {
		t.Fatal(err)
	}
	s := mustStore(t)
	ref, err := s.CreateInitial(context.Background(), "tenant", "agent", "version", source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.MaterializeVersion(context.Background(), "tenant", "agent", "version", ref); err != nil {
		t.Fatal(err)
	}
}

func TestMaterializeVersionAndChatAreImmutableAndIsolated(t *testing.T) {
	store := mustStore(t)
	ref, err := store.CreateInitial(context.Background(), "tenant", "agent", "v1", validSource(t))
	if err != nil {
		t.Fatal(err)
	}
	version, err := store.MaterializeVersion(context.Background(), "tenant", "agent", "v1", ref)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(version, "AGENTS.md"), []byte("changed"), 0o600); err == nil {
		t.Fatal("version behavior file is writable")
	}
	chat1, err := store.MaterializeChat(context.Background(), "tenant", "agent", "conversation-1", "v1", ref)
	if err != nil {
		t.Fatal(err)
	}
	chat2, err := store.MaterializeChat(context.Background(), "tenant", "agent", "conversation-2", "v1", ref)
	if err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{chat1, chat2} {
		for _, name := range []string{"scratch", ".tmp"} {
			info, err := os.Stat(filepath.Join(root, name))
			if err != nil || info.Mode().Perm()&0o200 == 0 {
				t.Fatalf("%s/%s not writable: %v", root, name, err)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(chat1, "scratch", "private"), []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(chat2, "scratch", "private")); !os.IsNotExist(err) {
		t.Fatal("chat copies share scratch")
	}
}

func TestMaterializeValidationUsesIsolatedRuntimePath(t *testing.T) {
	store := mustStore(t)
	ref, err := store.CreateInitial(context.Background(), "tenant", "agent", "v1", validSource(t))
	if err != nil {
		t.Fatal(err)
	}
	root, err := store.MaterializeValidation(context.Background(), "tenant", "agent", "operation-1", ref)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(store.agentRoot("tenant", "agent"), "runtime-cache", "validation", "operation-1")
	if root != want {
		t.Fatalf("path=%q want %q", root, want)
	}
	for _, name := range []string{"scratch", ".tmp"} {
		info, err := os.Stat(filepath.Join(root, name))
		if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
			t.Fatalf("%s: %v %#v", name, err, info)
		}
	}
	if info, err := os.Stat(filepath.Join(root, "AGENTS.md")); err != nil || info.Mode().Perm() != 0o444 {
		t.Fatalf("AGENTS.md: %v %#v", err, info)
	}
}

func TestCleanupPreparationDeletesOnlyExactRefAndArtifacts(t *testing.T) {
	store := mustStore(t)
	source := validSource(t)
	v1, err := store.CreateInitial(context.Background(), "tenant", "agent", "v1", source)
	if err != nil {
		t.Fatal(err)
	}
	v2, err := store.CreateInitial(context.Background(), "tenant", "agent", "v2", source)
	if err != nil {
		t.Fatal(err)
	}
	version, err := store.MaterializeVersion(context.Background(), "tenant", "agent", "v1", v1)
	if err != nil {
		t.Fatal(err)
	}
	validation, err := store.MaterializeValidation(context.Background(), "tenant", "agent", "v1", v1)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CleanupPreparation(context.Background(), PreparationArtifacts{TenantID: "tenant", AgentID: "agent", VersionID: "v1", Ref: v1}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{version, validation} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("artifact retained: %s %v", path, err)
		}
	}
	if err := store.Verify(context.Background(), "tenant", "agent", v2); err != nil {
		t.Fatalf("other ref or shared commit removed: %v", err)
	}
}

func TestCleanupPreparationFailsClosedOnMismatchOrSymlink(t *testing.T) {
	t.Run("commit mismatch", func(t *testing.T) {
		store := mustStore(t)
		ref, err := store.CreateInitial(context.Background(), "tenant", "agent", "v1", validSource(t))
		if err != nil {
			t.Fatal(err)
		}
		bad := ref
		bad.Commit = strings.Repeat("b", 40)
		if err := store.CleanupPreparation(context.Background(), PreparationArtifacts{TenantID: "tenant", AgentID: "agent", VersionID: "v1", Ref: bad}); err == nil {
			t.Fatal("commit mismatch accepted")
		}
		if err := store.Verify(context.Background(), "tenant", "agent", ref); err != nil {
			t.Fatalf("tag changed: %v", err)
		}
	})
	t.Run("symlink artifact", func(t *testing.T) {
		store := mustStore(t)
		outside := t.TempDir()
		target := filepath.Join(store.agentRoot("tenant", "agent"), "versions", "v1")
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, target); err != nil {
			t.Fatal(err)
		}
		if err := store.CleanupPreparation(context.Background(), PreparationArtifacts{TenantID: "tenant", AgentID: "agent", VersionID: "v1"}); err == nil {
			t.Fatal("symlink artifact accepted")
		}
		if _, err := os.Stat(outside); err != nil {
			t.Fatalf("outside target changed: %v", err)
		}
	})
}

func TestNativeChatCannotReadSiblingScratch(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("requires OS sandbox")
	}
	if os.Getenv("RUN_WORKSPACE_SANDBOX_INTEGRATION") != "1" {
		t.Skip("native suite not requested")
	}
	store := mustStore(t)
	ref, err := store.CreateInitial(context.Background(), "tenant", "agent", "v1", validSource(t))
	if err != nil {
		t.Fatal(err)
	}
	chat1, err := store.MaterializeChat(context.Background(), "tenant", "agent", "conversation-1", "v1", ref)
	if err != nil {
		t.Fatal(err)
	}
	chat2, err := store.MaterializeChat(context.Background(), "tenant", "agent", "conversation-2", "v1", ref)
	if err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(chat2, "scratch", "secret")
	if err := os.WriteFile(secret, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	chat1, err = filepath.EvalSymlinks(chat1)
	if err != nil {
		t.Fatal(err)
	}
	runner, err := sandbox.NewRunner(chat1)
	if err != nil {
		t.Fatal(err)
	}
	policy := runner.Policy()
	tmpDir, err := sandbox.PayloadTmpDir(policy, chat1)
	if err != nil {
		t.Fatal(err)
	}
	env, err := sandbox.BuildSandboxPayloadEnv(chat1, tmpDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	cmd, err := runner.BuildArgv([]string{"/bin/cat", secret}, sandbox.CommandSpec{WorkDir: chat1, PayloadEnv: env})
	if err != nil {
		t.Fatal(err)
	}
	if out, err := cmd.CombinedOutput(); err == nil || strings.Contains(string(out), "private") {
		t.Fatalf("sibling scratch was readable: %q %v", out, err)
	}
}

func TestFailedMaterializationKeepsExistingDestination(t *testing.T) {
	store := mustStore(t)
	destination := filepath.Join(store.agentRoot("tenant", "agent"), "versions", "v1")
	if err := os.MkdirAll(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "marker"), []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := store.MaterializeVersion(context.Background(), "tenant", "agent", "v1", BundleRef{Commit: "missing", Tag: "versions/v1", Digest: "sha256:bad"})
	if err == nil {
		t.Fatal("invalid ref materialized")
	}
	if b, readErr := os.ReadFile(filepath.Join(destination, "marker")); readErr != nil || string(b) != "existing" {
		t.Fatalf("existing destination changed: %q %v", b, readErr)
	}
}

func TestExistingMaterializationRejectsOversizedAndHardlinkedFiles(t *testing.T) {
	for _, name := range []string{"oversized", "hardlink"} {
		t.Run(name, func(t *testing.T) {
			store := mustStore(t)
			ref, err := store.CreateInitial(context.Background(), "tenant", "agent", "v1", validSource(t))
			if err != nil {
				t.Fatal(err)
			}
			root, err := store.MaterializeVersion(context.Background(), "tenant", "agent", "v1", ref)
			if err != nil {
				t.Fatal(err)
			}
			agents := filepath.Join(root, "AGENTS.md")
			if err := os.Chmod(root, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(agents); err != nil {
				t.Fatal(err)
			}
			if name == "oversized" {
				if err := os.WriteFile(agents, make([]byte, maxBundleFile+1), 0o444); err != nil {
					t.Fatal(err)
				}
			} else {
				external := filepath.Join(t.TempDir(), "linked")
				if err := os.WriteFile(external, []byte("You are useful.\n"), 0o444); err != nil {
					t.Fatal(err)
				}
				if err := os.Link(external, agents); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := store.MaterializeVersion(context.Background(), "tenant", "agent", "v1", ref); err == nil {
				t.Fatalf("%s materialized file accepted", name)
			}
		})
	}
}

func TestExistingMaterializationRejectsDirectoryModeChanges(t *testing.T) {
	for _, target := range []string{"root", "behavior directory", "scratch not writable"} {
		t.Run(target, func(t *testing.T) {
			store := mustStore(t)
			source := validSource(t)
			if err := os.MkdirAll(filepath.Join(source, "documents"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(source, "documents", "note.txt"), []byte("note"), 0o600); err != nil {
				t.Fatal(err)
			}
			ref, err := store.CreateInitial(context.Background(), "tenant", "agent", "v1", source)
			if err != nil {
				t.Fatal(err)
			}
			chat := target == "scratch not writable"
			var root string
			if chat {
				root, err = store.MaterializeChat(context.Background(), "tenant", "agent", "conversation", "v1", ref)
			} else {
				root, err = store.MaterializeVersion(context.Background(), "tenant", "agent", "v1", ref)
			}
			if err != nil {
				t.Fatal(err)
			}
			path := root
			mode := os.FileMode(0o755)
			if target == "behavior directory" {
				path = filepath.Join(root, "documents")
			}
			if target == "scratch not writable" {
				path = filepath.Join(root, "scratch")
				mode = 0o500
			}
			if err := os.Chmod(path, mode); err != nil {
				t.Fatal(err)
			}
			if chat {
				_, err = store.MaterializeChat(context.Background(), "tenant", "agent", "conversation", "v1", ref)
			} else {
				_, err = store.MaterializeVersion(context.Background(), "tenant", "agent", "v1", ref)
			}
			if err == nil {
				t.Fatalf("%s mode change accepted", target)
			}
		})
	}
}
