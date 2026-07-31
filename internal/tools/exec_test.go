package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type processObservation struct {
	SessionID string `json:"session_id"`
	Status    string `json:"status"`
	Command   string `json:"command"`
	WorkDir   string `json:"workdir"`
	Output    string `json:"output"`
	ExitCode  *int   `json:"exit_code,omitempty"`
	Truncated bool   `json:"truncated"`
}

func TestExecToolDefinitionIsExclusive(t *testing.T) {
	manager := newProcessSupervisorForTest(t, t.TempDir())
	definition := NewExecTool(manager).Definition()
	if definition.Name != "exec" || definition.Description == "" || definition.ParallelSafe {
		t.Fatalf("definition = %#v", definition)
	}
}

func TestExecToolReturnsOutputAndNonZeroExitCode(t *testing.T) {
	manager := newProcessSupervisorForTest(t, t.TempDir())
	tool := NewExecTool(manager)
	result := executeAndDecodeProcessObservation(t, tool, map[string]any{
		"command":  toolHelperCommand("output-exit"),
		"yield_ms": 30000,
	})
	if result.Status != "completed" || result.ExitCode == nil || *result.ExitCode != 7 {
		t.Fatalf("result = %#v", result)
	}
	if !strings.Contains(result.Output, "stdout") || !strings.Contains(result.Output, "stderr") {
		t.Fatalf("output = %q", result.Output)
	}
}

func TestExecToolPreservesOriginalCommandText(t *testing.T) {
	manager := newProcessSupervisorForTest(t, t.TempDir())
	command := "  " + toolHelperCommand("print", "preserved") + "  "
	result := executeAndDecodeProcessObservation(t, NewExecTool(manager), map[string]any{
		"command":  command,
		"yield_ms": 30000,
	})
	if result.Command != command || result.Output != "preserved" {
		t.Fatalf("result = %#v", result)
	}
}

func TestExecToolUsesWorkspaceRelativeWorkDirAndEnvironment(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv("REAGENT_TEST_VALUE", "inherited")
	if err := os.Mkdir(filepath.Join(workDir, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	manager := newProcessSupervisorForTest(t, workDir)
	result := executeAndDecodeProcessObservation(t, NewExecTool(manager), map[string]any{
		"command":  toolHelperCommand("cwd-env"),
		"workdir":  "nested",
		"env":      map[string]string{"REAGENT_TEST_VALUE": "configured"},
		"yield_ms": 30000,
	})
	resolvedWorkDir, err := filepath.EvalSymlinks(workDir)
	if err != nil {
		t.Fatal(err)
	}
	wantPrefix := filepath.Join(resolvedWorkDir, "nested") + "|configured"
	if result.Output != wantPrefix {
		t.Fatalf("output = %q, want %q", result.Output, wantPrefix)
	}
}

func TestExecToolTimesOutAndTerminatesCommand(t *testing.T) {
	manager := newProcessSupervisorForTest(t, t.TempDir())
	started := time.Now()
	result := executeAndDecodeProcessObservation(t, NewExecTool(manager), map[string]any{
		"command":    toolHelperCommand("sleep", "5000"),
		"timeout_ms": 50,
		"yield_ms":   30000,
	})
	if result.Status != "timed_out" || time.Since(started) > 2*time.Second {
		t.Fatalf("result = %#v, elapsed = %v", result, time.Since(started))
	}
}

func TestExecToolKeepsBoundedTailOutput(t *testing.T) {
	manager := newProcessSupervisorForTest(t, t.TempDir())
	result := executeAndDecodeProcessObservation(t, NewExecTool(manager), map[string]any{
		"command":  toolHelperCommand("large-output", "60000"),
		"yield_ms": 30000,
	})
	if !result.Truncated || len([]byte(result.Output)) > defaultProcessOutputBytes {
		t.Fatalf("truncated = %v, output bytes = %d", result.Truncated, len([]byte(result.Output)))
	}
}

func TestExecToolAutoBackgroundsAfterYield(t *testing.T) {
	manager := newProcessSupervisorForTest(t, t.TempDir())
	result := executeAndDecodeProcessObservation(t, NewExecTool(manager), map[string]any{
		"command":  toolHelperCommand("sleep-output", "200", "done"),
		"yield_ms": 10,
	})
	if result.Status != "running" || result.SessionID == "" {
		t.Fatalf("result = %#v", result)
	}
}

func TestExecToolYieldZeroReturnsImmediately(t *testing.T) {
	manager := newProcessSupervisorForTest(t, t.TempDir())
	started := time.Now()
	result := executeAndDecodeProcessObservation(t, NewExecTool(manager), map[string]any{
		"command":  toolHelperCommand("sleep", "1000"),
		"yield_ms": 0,
	})
	if result.Status != "running" || time.Since(started) > 500*time.Millisecond {
		t.Fatalf("result = %#v, elapsed = %v", result, time.Since(started))
	}
}

func TestExecToolRejectsInvalidArgumentsAndWorkDir(t *testing.T) {
	manager := newProcessSupervisorForTest(t, t.TempDir())
	tool := NewExecTool(manager)
	tests := []struct {
		args json.RawMessage
		want string
	}{
		{args: json.RawMessage(`{"command":`), want: "参数解析失败"},
		{args: json.RawMessage(`{"command":"true","extra":true}`), want: "unknown field"},
		{args: json.RawMessage(`{"command":" "}`), want: "command 不能为空"},
		{args: execArguments(t, map[string]any{"command": "true", "workdir": "../outside"}), want: "工作区"},
		{args: execArguments(t, map[string]any{"command": "true", "env": map[string]string{"PATH": "/tmp"}}), want: "PATH"},
		{args: execArguments(t, map[string]any{"command": "true", "timeout_ms": 0}), want: "timeout_ms"},
		{args: execArguments(t, map[string]any{"command": "true", "yield_ms": 30001}), want: "yield_ms"},
	}
	for _, tt := range tests {
		if _, err := tool.execute(context.Background(), tt.args); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Fatalf("Execute() error = %v, want containing %q", err, tt.want)
		}
	}
}

func TestExecToolHonorsCanceledContextAndClosedSupervisor(t *testing.T) {
	manager := newProcessSupervisorForTest(t, t.TempDir())
	tool := NewExecTool(manager)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := tool.execute(ctx, execArguments(t, map[string]any{"command": "true"})); err == nil || !strings.Contains(err.Error(), "取消") {
		t.Fatalf("canceled Execute() error = %v", err)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := tool.execute(context.Background(), execArguments(t, map[string]any{"command": "true"})); err == nil || !strings.Contains(err.Error(), "关闭") {
		t.Fatalf("closed Execute() error = %v", err)
	}
}

func executeAndDecodeProcessObservation(t *testing.T, tool *ExecTool, input map[string]any) processObservation {
	t.Helper()
	output, err := tool.execute(context.Background(), execArguments(t, input))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	var result processObservation
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("decode output %q: %v", output, err)
	}
	return result
}

func execArguments(t *testing.T, input map[string]any) json.RawMessage {
	t.Helper()
	arguments, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal arguments: %v", err)
	}
	return arguments
}
