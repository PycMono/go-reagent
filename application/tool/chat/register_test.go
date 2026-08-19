package chat

import (
	"slices"
	"testing"

	"github.com/PycMono/go-reagent/pi/ai"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"
)

type registeredTools struct {
	fx.In
	Tools []ai.Tool `group:"agent_tools"`
}

func TestRegisterProvidesLocalChatTools(t *testing.T) {
	var tools []ai.Tool
	app := fxtest.New(t,
		Register,
		fx.Invoke(func(params registeredTools) { tools = params.Tools }),
	)
	app.RequireStart()
	t.Cleanup(app.RequireStop)

	names := make([]string, len(tools))
	for i, tool := range tools {
		names[i] = tool.Definition().Name
		if !tool.Definition().ParallelSafe {
			t.Fatalf("tool %q is not parallel safe", names[i])
		}
	}
	slices.Sort(names)
	want := []string{"calculate", "get_current_time"}
	if !slices.Equal(names, want) {
		t.Fatalf("tools = %v, want %v", names, want)
	}
}
