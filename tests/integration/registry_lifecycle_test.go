package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/PycMono/go-reagent/agent"
	"github.com/PycMono/go-reagent/ai"
	"github.com/PycMono/go-reagent/internal/bootstrap"
	"github.com/PycMono/go-reagent/internal/tools"
	workspacepkg "github.com/PycMono/go-reagent/internal/workspace"
	"go.uber.org/fx"
)

func TestRegistryResourcesCloseInFxLifecycleOrder(t *testing.T) {
	var (
		registry   agent.Registry
		workspace  *tools.Workspace
		supervisor *tools.ProcessSupervisor
	)
	app := fx.New(
		fx.NopLogger,
		fx.Supply(
			workspacepkg.WorkDir(t.TempDir()),
			ai.PlatformConfig{ID: "test", Protocol: ai.ProtocolOpenAI, BaseURL: "http://127.0.0.1/v1/", APIKey: "key", Model: "model"},
		),
		bootstrap.Module,
		fx.Populate(&registry, &workspace, &supervisor),
	)
	if err := app.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	want := []string{"apply_patch", "edit", "exec", "process", "read", "write"}
	if diff := slices.Compare(toolNames(registry.GetAvailableTools()), want); diff != 0 {
		t.Fatalf("tool names = %v, want %v", toolNames(registry.GetAvailableTools()), want)
	}
	for _, old := range []string{"read_file", "edit_file", "write_file"} {
		result, err := registry.Execute(context.Background(), ai.ToolCall{ID: "old", Name: old, Arguments: json.RawMessage(`{}`)}, nil)
		if err != nil || !result.IsError {
			t.Fatalf("old tool %q remained callable", old)
		}
	}

	marker := filepath.Join(t.TempDir(), "child-survived")
	command := lifecycleHelperCommand("spawn-child", marker)
	result, err := registry.Execute(context.Background(), ai.ToolCall{
		ID:        "background-exec",
		Name:      "exec",
		Arguments: json.RawMessage(fmt.Sprintf(`{"command":%q,"background":true}`, command)),
	}, nil)
	if err != nil || result.IsError {
		t.Fatalf("Registry.Execute(exec) = (%#v, %v)", result, err)
	}
	if len(supervisor.List()) != 1 {
		t.Fatalf("sessions before Stop = %#v, want one", supervisor.List())
	}
	waitForFile(t, marker+".ready", 3*time.Second)

	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := app.Stop(stopCtx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if sessions := supervisor.List(); len(sessions) != 0 {
		t.Fatalf("sessions after Stop = %#v, want none", sessions)
	}
	time.Sleep(700 * time.Millisecond)
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("background process group survived Fx Stop: %v", err)
	}
	if _, err := workspace.ReadFile("missing.txt"); err == nil {
		t.Fatal("Workspace.ReadFile() after Stop error = nil")
	}
	result, err = registry.Execute(context.Background(), schemaToolCall("read-after-stop", "read", `{"path":"missing.txt"}`), nil)
	if err != nil || !result.IsError {
		t.Fatalf("Registry.Execute() after Stop = (%#v, %v)", result, err)
	}
}

func TestRegistryLifecycleProcessHelper(t *testing.T) {
	separator := slices.Index(os.Args, "--")
	if separator < 0 || separator+1 >= len(os.Args) {
		return
	}
	arguments := os.Args[separator+1:]
	switch arguments[0] {
	case "spawn-child":
		if len(arguments) != 2 {
			os.Exit(2)
		}
		child := exec.Command(os.Args[0], "-test.run=^TestRegistryLifecycleProcessHelper$", "--", "delayed-write", arguments[1])
		if err := child.Start(); err != nil {
			os.Exit(2)
		}
		if err := os.WriteFile(arguments[1]+".ready", []byte("ready"), 0o600); err != nil {
			_ = child.Process.Kill()
			os.Exit(2)
		}
		if err := child.Wait(); err != nil {
			os.Exit(1)
		}
	case "delayed-write":
		if len(arguments) != 2 {
			os.Exit(2)
		}
		time.Sleep(500 * time.Millisecond)
		if err := os.WriteFile(arguments[1], []byte("survived"), 0o600); err != nil {
			os.Exit(2)
		}
	default:
		os.Exit(2)
	}
	os.Exit(0)
}

func lifecycleHelperCommand(arguments ...string) string {
	command := []string{quoteLifecycleArgument(os.Args[0]), "-test.run=^TestRegistryLifecycleProcessHelper$", "--"}
	for _, argument := range arguments {
		command = append(command, quoteLifecycleArgument(argument))
	}
	return strings.Join(command, " ")
}

func quoteLifecycleArgument(value string) string {
	if runtime.GOOS == "windows" {
		return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func waitForFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}

func toolNames(definitions []ai.ToolDefinition) []string {
	names := make([]string, len(definitions))
	for index := range definitions {
		names[index] = definitions[index].Name
	}
	return names
}

func schemaToolCall(id, name, arguments string) ai.ToolCall {
	return ai.ToolCall{ID: id, Name: name, Arguments: []byte(arguments)}
}
