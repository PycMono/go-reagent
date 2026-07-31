package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestProcessToolDefinitionIsExclusive(t *testing.T) {
	manager := newProcessSupervisorForTest(t, t.TempDir())
	definition := NewProcessTool(manager).Definition()
	if definition.Name != "process" || definition.Description == "" || definition.ParallelSafe {
		t.Fatalf("definition = %#v", definition)
	}
}

func TestProcessToolPollsBackgroundCommandToCompletion(t *testing.T) {
	manager := newProcessSupervisorForTest(t, t.TempDir())
	execResult := executeAndDecodeProcessObservation(t, NewExecTool(manager), map[string]any{
		"command":    toolHelperCommand("sleep-output", "50", "done"),
		"background": true,
	})
	result := executeProcessAction(t, NewProcessTool(manager), map[string]any{
		"action":     "poll",
		"session_id": execResult.SessionID,
		"wait_ms":    5000,
	})
	if result.Status != "completed" || result.Output != "done" || result.ExitCode == nil || *result.ExitCode != 0 {
		t.Fatalf("result = %#v", result)
	}
}

func TestProcessToolWritesStdinAndClosesIt(t *testing.T) {
	manager := newProcessSupervisorForTest(t, t.TempDir())
	execResult := executeAndDecodeProcessObservation(t, NewExecTool(manager), map[string]any{
		"command":    toolHelperCommand("copy-stdin"),
		"background": true,
	})
	writeResult := executeProcessAction(t, NewProcessTool(manager), map[string]any{
		"action":     "write",
		"session_id": execResult.SessionID,
		"data":       "hello stdin",
		"eof":        true,
	})
	if writeResult.SessionID != execResult.SessionID {
		t.Fatalf("write result = %#v", writeResult)
	}
	result := executeProcessAction(t, NewProcessTool(manager), map[string]any{
		"action":     "poll",
		"session_id": execResult.SessionID,
		"wait_ms":    5000,
	})
	if result.Status != "completed" || result.Output != "hello stdin" {
		t.Fatalf("result = %#v", result)
	}
}

func TestProcessToolWriteHonorsCancellation(t *testing.T) {
	manager := newProcessSupervisorForTest(t, t.TempDir())
	execResult := executeAndDecodeProcessObservation(t, NewExecTool(manager), map[string]any{
		"command":    toolHelperCommand("sleep", "1000"),
		"background": true,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := NewProcessTool(manager).execute(ctx, processArguments(t, map[string]any{
		"action":     "write",
		"session_id": execResult.SessionID,
		"data":       strings.Repeat("x", 1024*1024),
	}))
	if err == nil || !strings.Contains(err.Error(), "取消") || time.Since(started) > 500*time.Millisecond {
		t.Fatalf("Execute() error = %v, elapsed = %v", err, time.Since(started))
	}
	result, err := manager.Poll(context.Background(), execResult.SessionID, 0)
	if err != nil || result.Status != "canceled" {
		t.Fatalf("snapshot = %#v, error = %v", result, err)
	}
}

func TestProcessToolListsAndKillsOwnedSessions(t *testing.T) {
	manager := newProcessSupervisorForTest(t, t.TempDir())
	execResult := executeAndDecodeProcessObservation(t, NewExecTool(manager), map[string]any{
		"command":    toolHelperCommand("sleep", "5000"),
		"background": true,
	})
	output, err := NewProcessTool(manager).execute(context.Background(), processArguments(t, map[string]any{"action": "list"}))
	if err != nil || !strings.Contains(output, execResult.SessionID) {
		t.Fatalf("list output = %q, error = %v", output, err)
	}
	result := executeProcessAction(t, NewProcessTool(manager), map[string]any{
		"action":     "kill",
		"session_id": execResult.SessionID,
	})
	if result.Status != "killed" {
		t.Fatalf("result = %#v", result)
	}
}

func TestProcessToolRejectsUnknownSessionAndInvalidActions(t *testing.T) {
	manager := newProcessSupervisorForTest(t, t.TempDir())
	tool := NewProcessTool(manager)
	tests := []struct {
		input map[string]any
		want  string
	}{
		{input: map[string]any{"action": "unknown"}, want: "action"},
		{input: map[string]any{"action": "poll"}, want: "session_id"},
		{input: map[string]any{"action": "poll", "session_id": "missing"}, want: "不存在"},
		{input: map[string]any{"action": "poll", "session_id": "missing", "wait_ms": 30001}, want: "wait_ms"},
	}
	for _, tt := range tests {
		if _, err := tool.execute(context.Background(), processArguments(t, tt.input)); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Fatalf("Execute() error = %v, want containing %q", err, tt.want)
		}
	}
}

func TestProcessSupervisorCloseKillsRunningSessions(t *testing.T) {
	manager := newProcessSupervisorForTest(t, t.TempDir())
	execResult := executeAndDecodeProcessObservation(t, NewExecTool(manager), map[string]any{
		"command":    toolHelperCommand("sleep", "5000"),
		"background": true,
	})
	session, err := manager.session(execResult.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) > 2*time.Second {
		t.Fatalf("Close() took %v", time.Since(started))
	}
	if result := session.snapshot(); result.Status != "killed" {
		t.Fatalf("snapshot = %#v", result)
	}
	manager.mu.RLock()
	retainedSessions := len(manager.sessions)
	manager.mu.RUnlock()
	if retainedSessions != 0 {
		t.Fatalf("retained sessions after Close = %d", retainedSessions)
	}
	if _, err := NewProcessTool(manager).execute(context.Background(), processArguments(t, map[string]any{"action": "list"})); err == nil || !strings.Contains(err.Error(), "关闭") {
		t.Fatalf("process Execute() after Close error = %v", err)
	}
}

func executeProcessAction(t *testing.T, tool *ProcessTool, input map[string]any) processObservation {
	t.Helper()
	output, err := tool.execute(context.Background(), processArguments(t, input))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	var result processObservation
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("decode output %q: %v", output, err)
	}
	return result
}

func processArguments(t *testing.T, input map[string]any) json.RawMessage {
	t.Helper()
	arguments, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal arguments: %v", err)
	}
	return arguments
}
