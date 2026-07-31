package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/PycMono/go-reagent/internal/config"
	"github.com/PycMono/go-reagent/internal/engine"
	"github.com/PycMono/go-reagent/internal/schema"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
	"go.uber.org/fx/fxtest"
)

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
		fx.Provide(func() engine.AgentRuntime {
			return runtimeFunc(func(context.Context, schema.RunRequest, engine.Reporter) (schema.RunResult, error) {
				return schema.RunResult{}, runError
			})
		}),
		fx.Supply(config.Prompt("test")),
		fx.Provide(func() engine.Reporter { return nil }),
		Register,
	)
}
