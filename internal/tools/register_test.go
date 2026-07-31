package tools

import (
	"testing"

	"github.com/PycMono/go-reagent/internal/config"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"
)

func TestRegisterProvidesRuntimeRegistry(t *testing.T) {
	var registry Registry
	app := fxtest.New(t,
		fx.Supply(config.WorkDir(t.TempDir())),
		Register,
		fx.Populate(&registry),
	)
	app.RequireStart()
	defer app.RequireStop()

	if registry == nil {
		t.Fatal("Register did not provide Registry")
	}
	definitions := registry.GetAvailableTools()
	if len(definitions) != 6 {
		t.Fatalf("tool definitions = %#v, want 6 tools", definitions)
	}
	wantNames := []string{"apply_patch", "edit", "exec", "process", "read", "write"}
	for index, want := range wantNames {
		if definitions[index].Name != want {
			t.Fatalf("definitions[%d].Name = %q, want %q", index, definitions[index].Name, want)
		}
	}
}
