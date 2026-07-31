package integration_test

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/PycMono/go-reagent/internal/config"
	"github.com/PycMono/go-reagent/internal/schema"
	"github.com/PycMono/go-reagent/internal/tools"
	"go.uber.org/fx"
)

func TestRegistryResourcesCloseInFxLifecycleOrder(t *testing.T) {
	var (
		registry   tools.Registry
		workspace  *tools.Workspace
		supervisor *tools.ProcessSupervisor
	)
	app := fx.New(
		fx.NopLogger,
		fx.Supply(config.WorkDir(t.TempDir())),
		tools.Register,
		fx.Populate(&registry, &workspace, &supervisor),
	)
	if err := app.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	command := "while :; do sleep 1; done"
	if runtime.GOOS == "windows" {
		command = "ping -t 127.0.0.1 >NUL"
	}
	if _, err := supervisor.Start(context.Background(), tools.ProcessStart{Command: command}); err != nil {
		t.Fatalf("ProcessSupervisor.Start() error = %v", err)
	}
	if len(supervisor.List()) != 1 {
		t.Fatalf("sessions before Stop = %#v, want one", supervisor.List())
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := app.Stop(stopCtx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if sessions := supervisor.List(); len(sessions) != 0 {
		t.Fatalf("sessions after Stop = %#v, want none", sessions)
	}
	if _, err := workspace.ReadFile("missing.txt"); err == nil {
		t.Fatal("Workspace.ReadFile() after Stop error = nil")
	}
	result, err := registry.Execute(context.Background(), schemaToolCall("read-after-stop", "read", `{"path":"missing.txt"}`), nil)
	if err != nil || !result.IsError {
		t.Fatalf("Registry.Execute() after Stop = (%#v, %v)", result, err)
	}
}

func schemaToolCall(id, name, arguments string) schema.ToolCall {
	return schema.ToolCall{ID: id, Name: name, Arguments: []byte(arguments)}
}
