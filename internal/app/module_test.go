package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/PycMono/go-reagent/internal/engine"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
	"go.uber.org/fx/fxtest"
)

func TestModuleDependencyGraphIsValid(t *testing.T) {
	if err := fx.ValidateApp(
		Module,
		fx.WithLogger(func() fxevent.Logger { return fxevent.NopLogger }),
	); err != nil {
		t.Fatalf("ValidateApp() error = %v", err)
	}
}

func TestAgentLifecycleShutsDownWithZeroExitCodeAfterSuccess(t *testing.T) {
	app := newLifecycleTestApp(t, nil)
	app.RequireStart()
	defer app.RequireStop()

	select {
	case signal := <-app.Wait():
		if signal.ExitCode != 0 {
			t.Fatalf("exit code = %d, want 0", signal.ExitCode)
		}
	case <-time.After(time.Second):
		t.Fatal("Fx app did not shut down after Agent success")
	}
}

func TestAgentLifecycleShutsDownWithExitCodeOneAfterFailure(t *testing.T) {
	app := newLifecycleTestApp(t, errors.New("agent failed"))
	app.RequireStart()
	defer app.RequireStop()

	select {
	case signal := <-app.Wait():
		if signal.ExitCode != 1 {
			t.Fatalf("exit code = %d, want 1", signal.ExitCode)
		}
	case <-time.After(time.Second):
		t.Fatal("Fx app did not shut down after Agent failure")
	}
}

func newLifecycleTestApp(t *testing.T, runError error) *fxtest.App {
	t.Helper()
	return fxtest.New(t,
		fx.WithLogger(func() fxevent.Logger { return fxevent.NopLogger }),
		fx.Provide(func() Agent {
			return agentFunc(func(context.Context, string, engine.Reporter) error {
				return runError
			})
		}),
		fx.Provide(func() engine.Reporter { return engine.NewTerminalReporter() }),
		fx.Supply(Prompt("test")),
		fx.Provide(NewAgentRunner),
		fx.Invoke(RegisterAgentLifecycle),
	)
}
