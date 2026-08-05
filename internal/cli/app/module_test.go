package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/PycMono/go-reagent"
	"github.com/PycMono/go-reagent/agent"
	"github.com/PycMono/go-reagent/internal/cli/conversation"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
	"go.uber.org/fx/fxtest"
)

func TestAgentLifecycleShutsDownWithZeroExitCodeAfterSuccess(t *testing.T) {
	app := newLifecycleTestApp(t, false, nil)
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
	app := newLifecycleTestApp(t, false, errors.New("agent failed"))
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

func TestAgentLifecycleUsesPersistedRunnerExitCodes(t *testing.T) {
	tests := []struct {
		name     string
		runError error
		exitCode int
	}{
		{name: "success", exitCode: 0},
		{name: "failure", runError: errors.New("conversation failed"), exitCode: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := newLifecycleTestApp(t, true, tt.runError)
			app.RequireStart()
			defer app.RequireStop()
			select {
			case signal := <-app.Wait():
				if signal.ExitCode != tt.exitCode {
					t.Fatalf("exit code = %d, want %d", signal.ExitCode, tt.exitCode)
				}
			case <-time.After(time.Second):
				t.Fatal("Fx app did not shut down after persisted Agent run")
			}
		})
	}
}

func newLifecycleTestApp(t *testing.T, persistenceEnabled bool, runError error) *fxtest.App {
	t.Helper()
	if persistenceEnabled {
		t.Setenv("AGENT_USER_ID", "user")
		t.Setenv("AGENT_CONVERSATION_ID", "conversation")
	}
	return fxtest.New(t,
		fx.WithLogger(func() fxevent.Logger { return fxevent.NopLogger }),
		fx.Provide(func() agent.Runner {
			return runtimeFunc(func(context.Context, agent.RunRequest, agent.Reporter) (agent.RunResult, error) {
				return agent.RunResult{}, runError
			})
		}),
		fx.Provide(func() conversation.Runner {
			return conversationFunc(func(context.Context, conversation.RunRequest, agent.Reporter) (agent.RunResult, error) {
				return agent.RunResult{}, runError
			})
		}),
		fx.Supply(&reagent.Config{Conversation: reagent.ConversationConfig{Enabled: persistenceEnabled}}),
		fx.Supply(Prompt("test")),
		fx.Provide(func() agent.Reporter { return nil }),
		Module,
	)
}
