package pi

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/PycMono/go-reagent/pi/agent"
	"github.com/PycMono/go-reagent/pi/skills"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"
)

func TestModuleProvidesWorkspaceContextComponents(t *testing.T) {
	workDir := t.TempDir()
	skillDir := filepath.Join(workDir, "skills", "fx-skill")
	if err := os.MkdirAll(skillDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: fx-skill\ndescription: Verify Fx binding\n---\nBody"), 0o600); err != nil {
		t.Fatal(err)
	}
	var (
		composer *PromptComposer
		loader   *skills.Loader
		factory  agent.ContextFactory
	)
	app := fxtest.New(t,
		fx.Supply(WorkDir(workDir)),
		Module,
		fx.Populate(&composer, &loader, &factory),
	)
	app.RequireStart()
	defer app.RequireStop()

	if composer == nil || composer.workDir != workDir {
		t.Fatalf("PromptComposer = %#v, want workDir %q", composer, workDir)
	}
	snapshot, err := loader.Discover(skills.DefaultEnvironment())
	if err != nil || len(snapshot.Skills()) != 1 || snapshot.Skills()[0].Name != "fx-skill" {
		t.Fatalf("Loader.Discover() = %#v, %v", snapshot, err)
	}
	if factory == nil {
		t.Fatal("agent.ContextFactory was not provided")
	}
}
