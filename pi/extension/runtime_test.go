package extension

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/toolexec"
	"go.uber.org/fx/fxtest"
)

// testTool 是扩展注册用的最小 ai.Tool 实现。
type testTool string

func (tool testTool) Definition() ai.ToolDefinition {
	return ai.ToolDefinition{
		Name:        string(tool),
		InputSchema: map[string]any{"type": "object"},
	}
}

func (testTool) Execute(context.Context, json.RawMessage, ai.UpdateEmitter) (ai.ToolOutput, error) {
	return ai.ToolOutput{Content: []ai.ContentBlock{ai.TextBlock("ok")}}, nil
}

type fakeExtension struct {
	name        string
	events      *[]string
	tool        string
	registerErr error
	closeErr    error
}

func (extension *fakeExtension) Name() string { return extension.name }

func (extension *fakeExtension) Register(_ context.Context, api API) error {
	*extension.events = append(*extension.events, "start:"+extension.name)
	if extension.tool != "" {
		if err := api.RegisterTool(testTool(extension.tool)); err != nil {
			return err
		}
	}
	return extension.registerErr
}

func (extension *fakeExtension) Close(context.Context) error {
	*extension.events = append(*extension.events, "stop:"+extension.name)
	return extension.closeErr
}

func TestExtensionRuntimeStartsSortedAndStopsReversed(t *testing.T) {
	var events []string
	registry, err := toolexec.NewRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle := fxtest.NewLifecycle(t)
	_, err = NewRuntime(Params{
		Lifecycle: lifecycle,
		Registry:  registry,
		Extensions: []Extension{
			&fakeExtension{name: "zeta", events: &events},
			&fakeExtension{name: "alpha", events: &events},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	lifecycle.RequireStart()
	lifecycle.RequireStop()
	want := []string{"start:alpha", "start:zeta", "stop:zeta", "stop:alpha"}
	if !slices.Equal(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
}

func TestExtensionRuntimeRollsBackAndClosesAfterStartFailure(t *testing.T) {
	var events []string
	registry, err := toolexec.NewRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle := fxtest.NewLifecycle(t)
	_, err = NewRuntime(Params{
		Lifecycle: lifecycle,
		Registry:  registry,
		Extensions: []Extension{
			&fakeExtension{name: "alpha", events: &events, tool: "alpha_tool"},
			&fakeExtension{name: "zeta", events: &events, tool: "secret_tool", registerErr: errors.New("discover failed")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	startErr := lifecycle.Start(context.Background())
	if startErr == nil || !strings.Contains(startErr.Error(), "zeta") {
		t.Fatalf("start error = %v", startErr)
	}
	wantEvents := []string{"start:alpha", "start:zeta", "stop:zeta", "stop:alpha"}
	if !slices.Equal(events, wantEvents) {
		t.Fatalf("events = %v, want %v", events, wantEvents)
	}
	if definitions := registry.Definitions(); len(definitions) != 0 {
		t.Fatalf("definitions after rollback = %#v", definitions)
	}
}

func TestExtensionRuntimeRejectsInvalidExtensions(t *testing.T) {
	var events []string
	var typedNil *fakeExtension
	tests := []struct {
		name       string
		extensions []Extension
	}{
		{name: "typed nil", extensions: []Extension{typedNil}},
		{name: "blank", extensions: []Extension{&fakeExtension{name: "  ", events: &events}}},
		{name: "duplicate", extensions: []Extension{
			&fakeExtension{name: "same", events: &events},
			&fakeExtension{name: "same", events: &events},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry, err := toolexec.NewRegistry(nil)
			if err != nil {
				t.Fatal(err)
			}
			_, err = NewRuntime(Params{
				Lifecycle:  fxtest.NewLifecycle(t),
				Registry:   registry,
				Extensions: test.extensions,
			})
			if err == nil {
				t.Fatal("invalid extensions accepted")
			}
		})
	}
}

func TestExtensionRuntimeJoinsCloseErrorsAndContinues(t *testing.T) {
	var events []string
	registry, err := toolexec.NewRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle := fxtest.NewLifecycle(t)
	_, err = NewRuntime(Params{
		Lifecycle: lifecycle,
		Registry:  registry,
		Extensions: []Extension{
			&fakeExtension{name: "alpha", events: &events, closeErr: errors.New("alpha close")},
			&fakeExtension{name: "zeta", events: &events, closeErr: errors.New("zeta close")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	stopErr := lifecycle.Stop(context.Background())
	if stopErr == nil || !strings.Contains(stopErr.Error(), "alpha close") || !strings.Contains(stopErr.Error(), "zeta close") {
		t.Fatalf("stop error = %v", stopErr)
	}
	if !slices.Equal(events, []string{"start:alpha", "start:zeta", "stop:zeta", "stop:alpha"}) {
		t.Fatalf("events = %v", events)
	}
}
