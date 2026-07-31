package tools

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PycMono/go-reagent/internal/schema"
)

func TestProcessToolDefinitionUsesSevenActionCamelCaseSchema(t *testing.T) {
	supervisor := newProcessSupervisorForTest(t, t.TempDir())
	definition := NewProcessTool(supervisor).Definition()
	if definition.Name != "process" || definition.Description == "" || definition.ParallelSafe {
		t.Fatalf("definition = %#v", definition)
	}
	inputSchema := definition.InputSchema.(map[string]any)
	properties := inputSchema["properties"].(map[string]any)
	keys := make([]string, 0, len(properties))
	for key := range properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if want := []string{"action", "data", "eof", "limit", "offset", "sessionId", "timeout"}; !reflect.DeepEqual(keys, want) {
		t.Fatalf("properties = %#v, want %#v", keys, want)
	}
	actions := properties["action"].(map[string]any)["enum"].([]string)
	if want := []string{"list", "poll", "log", "write", "kill", "clear", "remove"}; !reflect.DeepEqual(actions, want) {
		t.Fatalf("actions = %#v, want %#v", actions, want)
	}
	timeoutSchema := properties["timeout"].(map[string]any)
	if timeoutSchema["minimum"] != 0 || timeoutSchema["maximum"] != 30_000 {
		t.Fatalf("timeout schema = %#v", timeoutSchema)
	}
}

func TestProcessToolListsPollsAndPagesRetainedLogs(t *testing.T) {
	supervisor := newProcessSupervisorForTest(t, t.TempDir())
	session := mustStartProcess(t, supervisor, ProcessStart{Command: toolHelperCommand("print", "abcdef")})
	if _, err := supervisor.Poll(context.Background(), session.id, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	tool := NewProcessTool(supervisor)

	list := executeProcessOutput(t, tool, map[string]any{"action": "list"})
	listDetails := list.Details.(ProcessDetails)
	if listDetails.Action != "list" || len(listDetails.Sessions) != 1 || listDetails.Sessions[0].SessionID != session.id {
		t.Fatalf("list Details = %#v", listDetails)
	}

	poll := executeProcessOutput(t, tool, map[string]any{"action": "poll", "sessionId": session.id, "timeout": 0})
	pollDetails := poll.Details.(ProcessDetails)
	if pollDetails.Status != "completed" || pollDetails.ExitCode == nil || *pollDetails.ExitCode != 0 {
		t.Fatalf("poll Details = %#v", pollDetails)
	}

	log := executeProcessOutput(t, tool, map[string]any{"action": "log", "sessionId": session.id, "offset": 0, "limit": 3})
	logDetails := log.Details.(ProcessDetails)
	if got := toolOutputText(t, log); got != "abc" {
		t.Fatalf("log Content = %q", got)
	}
	if logDetails.Offset != 0 || logDetails.NextOffset != 3 || !logDetails.Truncated {
		t.Fatalf("log Details = %#v", logDetails)
	}
}

func TestProcessToolWritesAndKillsWhileRetainingSessions(t *testing.T) {
	supervisor := newProcessSupervisorForTest(t, t.TempDir())
	tool := NewProcessTool(supervisor)
	writer := mustStartProcess(t, supervisor, ProcessStart{Command: toolHelperCommand("copy-stdin")})
	write := executeProcessOutput(t, tool, map[string]any{
		"action":    "write",
		"sessionId": writer.id,
		"data":      "hello stdin",
		"eof":       true,
	})
	writeDetails := write.Details.(ProcessDetails)
	if writeDetails.Action != "write" || writeDetails.SessionID != writer.id {
		t.Fatalf("write Details = %#v", writeDetails)
	}
	if _, err := supervisor.Poll(context.Background(), writer.id, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	page, err := supervisor.Log(writer.id, 0, 0)
	if err != nil || page.Content != "hello stdin" {
		t.Fatalf("writer log = %#v, error = %v", page, err)
	}

	running := mustStartProcess(t, supervisor, ProcessStart{Command: toolHelperCommand("sleep", "5000")})
	kill := executeProcessOutput(t, tool, map[string]any{"action": "kill", "sessionId": running.id})
	killDetails := kill.Details.(ProcessDetails)
	if killDetails.Status != "killed" {
		t.Fatalf("kill Details = %#v", killDetails)
	}
	if _, err := supervisor.Poll(context.Background(), running.id, 0); err != nil {
		t.Fatalf("Kill did not retain session: %v", err)
	}
}

func TestProcessToolClearOnlyFinishedAndRemoveTerminatesThenDeletes(t *testing.T) {
	supervisor := newProcessSupervisorForTest(t, t.TempDir())
	tool := NewProcessTool(supervisor)
	finished := mustStartProcess(t, supervisor, ProcessStart{Command: toolHelperCommand("print", "done")})
	if _, err := supervisor.Poll(context.Background(), finished.id, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	running := mustStartProcess(t, supervisor, ProcessStart{Command: toolHelperCommand("sleep", "5000")})

	clearOutput := executeProcessOutput(t, tool, map[string]any{"action": "clear"})
	clearDetails := clearOutput.Details.(ProcessDetails)
	if clearDetails.Removed != 1 {
		t.Fatalf("clear Details = %#v", clearDetails)
	}
	if _, err := supervisor.Poll(context.Background(), running.id, 0); err != nil {
		t.Fatalf("Clear removed running session: %v", err)
	}

	removeOutput := executeProcessOutput(t, tool, map[string]any{"action": "remove", "sessionId": running.id})
	removeDetails := removeOutput.Details.(ProcessDetails)
	if removeDetails.Action != "remove" || removeDetails.SessionID != running.id || removeDetails.Removed != 1 {
		t.Fatalf("remove Details = %#v", removeDetails)
	}
	if _, err := supervisor.Poll(context.Background(), running.id, 0); err == nil {
		t.Fatal("Remove retained session")
	}
}

func TestProcessToolRejectsLegacyFieldsUnknownActionsAndInvalidActionArguments(t *testing.T) {
	supervisor := newProcessSupervisorForTest(t, t.TempDir())
	registry := newTestRegistry(t, defaultMiddlewareRegistrations(), NewProcessTool(supervisor))
	tests := []struct {
		input map[string]any
		want  string
	}{
		{input: map[string]any{"action": "poll", "session_id": "x"}, want: "session_id"},
		{input: map[string]any{"action": "poll", "sessionId": "x", "wait_ms": 1}, want: "wait_ms"},
		{input: map[string]any{"action": "unknown"}, want: "action"},
		{input: map[string]any{"action": "poll", "sessionId": "x", "timeout": 30_001}, want: "timeout"},
		{input: map[string]any{"action": "log", "sessionId": "x", "offset": -1}, want: "offset"},
		{input: map[string]any{"action": "log", "sessionId": "x", "limit": 0}, want: "limit"},
		{input: map[string]any{"action": "poll"}, want: "sessionId"},
		{input: map[string]any{"action": "write", "sessionId": "x"}, want: "data"},
	}
	for index, tt := range tests {
		result, err := registry.Execute(context.Background(), schema.ToolCall{
			ID:        "invalid-process",
			Name:      "process",
			Arguments: processArguments(t, tt.input),
		}, nil)
		if err != nil || !result.IsError || !strings.Contains(toolResultText(t, result), tt.want) {
			t.Fatalf("case %d Execute() = (%#v, %v), want %q", index, result, err, tt.want)
		}
	}
}

func TestProcessToolNeverInvokesUpdateEmitter(t *testing.T) {
	supervisor := newProcessSupervisorForTest(t, t.TempDir())
	var updates atomic.Int32
	output, err := NewProcessTool(supervisor).Execute(context.Background(), processArguments(t, map[string]any{"action": "list"}), func(schema.ToolUpdate) {
		updates.Add(1)
	})
	if err != nil || updates.Load() != 0 || toolOutputText(t, output) == "" {
		t.Fatalf("Execute() = (%#v, %v), updates = %d", output, err, updates.Load())
	}
}

func executeProcessOutput(t *testing.T, tool *ProcessTool, input map[string]any) schema.ToolOutput {
	t.Helper()
	output, err := tool.Execute(context.Background(), processArguments(t, input), nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if toolOutputText(t, output) == "" {
		t.Fatal("Execute() returned empty text content")
	}
	return output
}

func processArguments(t *testing.T, input map[string]any) json.RawMessage {
	t.Helper()
	arguments, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal arguments: %v", err)
	}
	return arguments
}
