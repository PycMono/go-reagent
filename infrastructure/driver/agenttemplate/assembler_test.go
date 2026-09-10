package agenttemplate

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	profileentity "github.com/PycMono/go-reagent/domain/entity/agentprofile"
	profiledriver "github.com/PycMono/go-reagent/infrastructure/driver/agentprofile"
	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/harness"
)

func TestAssembleRealProfilesAsStandaloneWorkspaces(t *testing.T) {
	workspace := filepath.Clean(filepath.Join("..", "..", "..", "workspaces", "chat"))
	catalog, err := profiledriver.NewCatalog(pi.WorkDir(workspace))
	if err != nil {
		t.Fatal(err)
	}
	assembler, err := New(workspace, catalog)
	if err != nil {
		t.Fatal(err)
	}
	rootBefore := fileDigest(t, filepath.Join(workspace, "AGENTS.md"))
	profiles := catalog.List()
	if len(profiles) != 8 {
		t.Fatalf("profiles = %d", len(profiles))
	}
	for _, profile := range profiles {
		t.Run(profile.Code, func(t *testing.T) {
			destination := t.TempDir()
			if err := assembler.Assemble(context.Background(), profile.Code, destination); err != nil {
				t.Fatal(err)
			}
			content, err := os.ReadFile(filepath.Join(destination, "AGENTS.md"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(content), "# 通用聊天助手") || !strings.Contains(string(content), profile.Instructions) || strings.Contains(string(content), "profiles/") {
				t.Fatal("combined AGENTS.md lost root/profile instructions or retained profile paths")
			}
			report, err := harness.InspectWorkspace(context.Background(), destination)
			if err != nil || len(report.Diagnostics) != 0 {
				t.Fatalf("InspectWorkspace diagnostics=%v err=%v", report.Diagnostics, err)
			}
			wantSkills := append(skillDirectories(t, filepath.Join(workspace, "skills")), skillDirectories(t, filepath.Join(workspace, "profiles", profile.Code, "skills"))...)
			sort.Strings(wantSkills)
			gotSkills := skillDirectories(t, filepath.Join(destination, "skills"))
			sort.Strings(gotSkills)
			if fmt.Sprint(gotSkills) != fmt.Sprint(wantSkills) {
				t.Fatalf("skills = %v, want %v", gotSkills, wantSkills)
			}
			assertRelocatedFiles(t, filepath.Join(workspace, "profiles", profile.Code, "references"), filepath.Join(destination, "documents"), "")
			for _, skill := range skillDirectories(t, filepath.Join(workspace, "profiles", profile.Code, "skills")) {
				assertRelocatedFiles(t, filepath.Join(workspace, "profiles", profile.Code, "skills", skill, "templates"), filepath.Join(destination, "assets", skill), "")
			}
			if matches, _ := filepath.Glob(filepath.Join(destination, "profiles", "*")); len(matches) != 0 {
				t.Fatalf("copied profiles: %v", matches)
			}
			assertNoProfileReferences(t, destination, profile.Code)
		})
	}
	if got := fileDigest(t, filepath.Join(workspace, "AGENTS.md")); got != rootBefore {
		t.Fatal("shared template was mutated")
	}
}

func TestAssembliesAreIndependent(t *testing.T) {
	workspace := filepath.Clean(filepath.Join("..", "..", "..", "workspaces", "chat"))
	catalog, _ := profiledriver.NewCatalog(pi.WorkDir(workspace))
	assembler, _ := New(workspace, catalog)
	first, second := t.TempDir(), t.TempDir()
	if err := assembler.Assemble(context.Background(), "legal", first); err != nil {
		t.Fatal(err)
	}
	if err := assembler.Assemble(context.Background(), "legal", second); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(first, "documents", "legal-review-framework.md")
	if err := os.WriteFile(path, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	other, _ := os.ReadFile(filepath.Join(second, "documents", "legal-review-framework.md"))
	source, _ := os.ReadFile(filepath.Join(workspace, "profiles", "legal", "references", "legal-review-framework.md"))
	if string(other) != string(source) {
		t.Fatal("assembly mutation leaked to another assembly or source")
	}
}

func TestAssembleRejectsUnsafeInputsBeforeWriting(t *testing.T) {
	workspace := filepath.Clean(filepath.Join("..", "..", "..", "workspaces", "chat"))
	catalog, _ := profiledriver.NewCatalog(pi.WorkDir(workspace))
	assembler, _ := New(workspace, catalog)
	for _, code := range []string{"missing", ""} {
		if err := assembler.Assemble(context.Background(), code, t.TempDir()); err == nil {
			t.Fatalf("template %q accepted", code)
		}
	}
	nonempty := t.TempDir()
	os.WriteFile(filepath.Join(nonempty, "keep"), []byte("x"), 0o600)
	if err := assembler.Assemble(context.Background(), "general", nonempty); err == nil {
		t.Fatal("non-empty destination accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := assembler.Assemble(ctx, "general", t.TempDir()); err == nil {
		t.Fatal("canceled assembly accepted")
	}
}

type fakeCatalog struct{ profile profileentity.Profile }

func (catalog fakeCatalog) List() []profileentity.Profile {
	return []profileentity.Profile{catalog.profile}
}
func (catalog fakeCatalog) Find(code string) (profileentity.Profile, bool) {
	return catalog.profile, code == catalog.profile.Code
}
func (catalog fakeCatalog) DefaultCode() string { return catalog.profile.Code }

func TestAssembleRejectsSkillCollisionAndSourceSymlink(t *testing.T) {
	for _, test := range []struct {
		name string
		set  func(*testing.T, string)
	}{
		{name: "collision", set: func(t *testing.T, root string) {
			writeFixture(t, filepath.Join(root, "profiles", "test", "skills", "same", "SKILL.md"), skillFixture("same"))
		}},
		{name: "symlink", set: func(t *testing.T, root string) {
			target := filepath.Join(t.TempDir(), "outside.md")
			writeFixture(t, target, skillFixture("outside"))
			path := filepath.Join(root, "skills", "linked.md")
			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeFixture(t, filepath.Join(root, "AGENTS.md"), "root")
			writeFixture(t, filepath.Join(root, "profiles", "test", "AGENTS.md"), "profile")
			writeFixture(t, filepath.Join(root, "skills", "same", "SKILL.md"), skillFixture("same"))
			test.set(t, root)
			assembler, err := New(root, fakeCatalog{profileentity.Profile{Code: "test", Selectable: true}})
			if err != nil {
				t.Fatal(err)
			}
			destination := t.TempDir()
			if err := assembler.Assemble(context.Background(), "test", destination); err == nil {
				t.Fatal("unsafe source accepted")
			}
			entries, _ := os.ReadDir(destination)
			if len(entries) != 0 {
				t.Fatal("failed preflight wrote destination")
			}
		})
	}
}

func TestAssembleRejectsUnselectableTemplate(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, filepath.Join(root, "AGENTS.md"), "root")
	assembler, err := New(root, fakeCatalog{profileentity.Profile{Code: "disabled", Selectable: false}})
	if err != nil {
		t.Fatal(err)
	}
	if err := assembler.Assemble(context.Background(), "disabled", t.TempDir()); err == nil {
		t.Fatal("unselectable template accepted")
	}
}

func writeFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func skillFixture(name string) string {
	return "---\nname: " + name + "\ndescription: test skill\n---\n\n# Test\n"
}

func fileDigest(t *testing.T, path string) [32]byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(content)
}

func skillDirectories(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			if _, err := os.Stat(filepath.Join(root, entry.Name(), "SKILL.md")); err != nil {
				continue
			}
			names = append(names, entry.Name())
		}
	}
	return names
}

func assertRelocatedFiles(t *testing.T, source, destination, _ string) {
	t.Helper()
	entries, err := os.ReadDir(source)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(destination, entry.Name())); err != nil {
			t.Fatalf("missing relocated %s: %v", entry.Name(), err)
		}
	}
}

func assertNoProfileReferences(t *testing.T, root, code string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Ext(path) != ".md" {
			return err
		}
		content, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(content), "profiles/"+code+"/") {
			t.Errorf("legacy profile reference remains in %s", path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}
