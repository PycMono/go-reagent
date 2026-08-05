package pi

import (
	"testing"

	"github.com/PycMono/go-reagent/pi/agent"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"
)

func TestModuleProvidesWorkspaceContextComponents(t *testing.T) {
	workDir := t.TempDir()
	var (
		composer *PromptComposer
		loader   *SkillLoader
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
	if loader == nil || loader.workDir != workDir {
		t.Fatalf("SkillLoader = %#v, want workDir %q", loader, workDir)
	}
	if factory == nil {
		t.Fatal("agent.ContextFactory was not provided")
	}
}
