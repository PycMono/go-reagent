package pi

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"go.uber.org/fx/fxtest"
)

type extensionFake struct {
	name        string
	events      *[]string
	tool        string
	registerErr error
	closeErr    error
}

func (extension *extensionFake) Name() string { return extension.name }

func (extension *extensionFake) Register(_ context.Context, api ExtensionAPI) error {
	*extension.events = append(*extension.events, "start:"+extension.name)
	if extension.tool != "" {
		if err := api.RegisterTool(registryTestTool(extension.tool)); err != nil {
			return err
		}
	}
	return extension.registerErr
}

func (extension *extensionFake) Close(context.Context) error {
	*extension.events = append(*extension.events, "stop:"+extension.name)
	return extension.closeErr
}

func TestExtensionRuntimeStartsSortedAndStopsReversed(t *testing.T) {
	var events []string
	registry, err := newToolRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle := fxtest.NewLifecycle(t)
	_, err = newExtensionRuntime(extensionRuntimeParams{
		Lifecycle: lifecycle,
		Registry:  registry,
		Extensions: []Extension{
			&extensionFake{name: "zeta", events: &events},
			&extensionFake{name: "alpha", events: &events},
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
	registry, err := newToolRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle := fxtest.NewLifecycle(t)
	_, err = newExtensionRuntime(extensionRuntimeParams{
		Lifecycle: lifecycle,
		Registry:  registry,
		Extensions: []Extension{
			&extensionFake{name: "alpha", events: &events, tool: "alpha_tool"},
			&extensionFake{name: "zeta", events: &events, tool: "secret_tool", registerErr: errors.New("discover failed")},
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
	if definitions := registry.definitions(); len(definitions) != 0 {
		t.Fatalf("definitions after rollback = %#v", definitions)
	}
}

func TestExtensionRuntimeRejectsInvalidExtensions(t *testing.T) {
	var events []string
	var typedNil *extensionFake
	tests := []struct {
		name       string
		extensions []Extension
	}{
		{name: "typed nil", extensions: []Extension{typedNil}},
		{name: "blank", extensions: []Extension{&extensionFake{name: "  ", events: &events}}},
		{name: "duplicate", extensions: []Extension{
			&extensionFake{name: "same", events: &events},
			&extensionFake{name: "same", events: &events},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry, err := newToolRegistry(nil)
			if err != nil {
				t.Fatal(err)
			}
			_, err = newExtensionRuntime(extensionRuntimeParams{
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
	registry, err := newToolRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle := fxtest.NewLifecycle(t)
	_, err = newExtensionRuntime(extensionRuntimeParams{
		Lifecycle: lifecycle,
		Registry:  registry,
		Extensions: []Extension{
			&extensionFake{name: "alpha", events: &events, closeErr: errors.New("alpha close")},
			&extensionFake{name: "zeta", events: &events, closeErr: errors.New("zeta close")},
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
