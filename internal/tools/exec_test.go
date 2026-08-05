package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PycMono/go-reagent/agent"
	"github.com/PycMono/go-reagent/ai"
)

func TestExecToolDefinitionUsesFinalCamelCaseSchemaAndDefaults(t *testing.T) {
	supervisor := newProcessSupervisorForTest(t, t.TempDir())
	definition := NewExecTool(supervisor).Definition()
	if definition.Name != "exec" || definition.Description == "" || definition.ParallelSafe {
		t.Fatalf("definition = %#v", definition)
	}
	inputSchema := definition.InputSchema.(map[string]any)
	properties := inputSchema["properties"].(map[string]any)
	keys := make([]string, 0, len(properties))
	for key := range properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if want := []string{"background", "command", "env", "timeout", "workdir", "yieldMs"}; !reflect.DeepEqual(keys, want) {
		t.Fatalf("properties = %#v, want %#v", keys, want)
	}
	yieldSchema := properties["yieldMs"].(map[string]any)
	if yieldSchema["minimum"] != 0 || yieldSchema["maximum"] != 30_000 || yieldSchema["default"] != 10_000 {
		t.Fatalf("yieldMs schema = %#v", yieldSchema)
	}
	timeoutSchema := properties["timeout"].(map[string]any)
	if timeoutSchema["minimum"] != 1 || timeoutSchema["maximum"] != 600 || timeoutSchema["default"] != 120 {
		t.Fatalf("timeout schema = %#v", timeoutSchema)
	}
}

func TestExecToolCompletesForegroundWithWorkspaceEnvironmentAndDefaults(t *testing.T) {
	workDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(workDir, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	supervisor := newProcessSupervisorForTest(t, workDir)
	command := "  " + toolHelperCommand("cwd-env") + "  "
	output, err := NewExecTool(supervisor).Execute(context.Background(), execArguments(t, map[string]any{
		"command": command,
		"workdir": "nested",
		"env":     map[string]string{"REAGENT_TEST_VALUE": "configured"},
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	details, ok := output.Details.(ExecDetails)
	if !ok {
		t.Fatalf("Details = %#v", output.Details)
	}
	resolvedRoot, err := filepath.EvalSymlinks(workDir)
	if err != nil {
		t.Fatal(err)
	}
	wantCWD := filepath.Join(resolvedRoot, "nested")
	if details.Status != "completed" || details.Command != command || details.CWD != wantCWD || details.ExitCode == nil || *details.ExitCode != 0 || details.SessionID == "" {
		t.Fatalf("Details = %#v", details)
	}
	if got := toolOutputText(t, output); got != wantCWD+"|configured" {
		t.Fatalf("Content = %q", got)
	}
}

func TestExecToolStreamsForegroundStdoutAndStderrAndMarksNonzeroAsError(t *testing.T) {
	supervisor := newProcessSupervisorForTest(t, t.TempDir())
	registry := newTestRegistry(t, agent.DefaultMiddlewareRegistrations(), NewExecTool(supervisor))
	call := ai.ToolCall{ID: "exec-stream", Name: "exec", Arguments: execArguments(t, map[string]any{
		"command": toolHelperCommand("output-exit"),
		"yieldMs": 30_000,
	})}
	var events []agent.ToolEvent
	result, err := registry.Execute(context.Background(), call, func(_ context.Context, event agent.ToolEvent) {
		events = append(events, event)
	})
	if err != nil || !result.IsError || !strings.Contains(toolResultText(t, result), "stdout") || !strings.Contains(toolResultText(t, result), "stderr") {
		t.Fatalf("Execute() = (%#v, %v)", result, err)
	}
	details, ok := result.Details.(ExecDetails)
	if !ok || details.Status != "completed" || details.ExitCode == nil || *details.ExitCode != 7 {
		t.Fatalf("Details = %#v", result.Details)
	}
	if len(events) < 4 || events[0].Phase != agent.ToolEventStart || events[len(events)-1].Phase != agent.ToolEventEnd {
		t.Fatalf("events = %#v", events)
	}
	streams := make(map[string]string)
	for _, event := range events[1 : len(events)-1] {
		if event.Phase != agent.ToolEventUpdate || event.Update == nil {
			t.Fatalf("event = %#v", event)
		}
		stream, ok := event.Update.Details.(StreamDetails)
		if !ok || stream.Bytes != len(toolEventText(t, event.Update.Content)) {
			t.Fatalf("update = %#v", event.Update)
		}
		streams[stream.Stream] += toolEventText(t, event.Update.Content)
	}
	if streams["stdout"] != "stdout" || streams["stderr"] != "stderr" {
		t.Fatalf("streams = %#v", streams)
	}
}

func TestExecToolExplicitBackgroundStartsWithStreamingGateClosed(t *testing.T) {
	supervisor := newProcessSupervisorForTest(t, t.TempDir())
	registry := newTestRegistry(t, agent.DefaultMiddlewareRegistrations(), NewExecTool(supervisor))
	var eventsMu sync.Mutex
	var events []agent.ToolEvent
	result, err := registry.Execute(context.Background(), ai.ToolCall{
		ID:   "exec-background",
		Name: "exec",
		Arguments: execArguments(t, map[string]any{
			"command":    toolHelperCommand("sleep-output", "100", "background-output"),
			"background": true,
		}),
	}, func(_ context.Context, event agent.ToolEvent) {
		eventsMu.Lock()
		events = append(events, event)
		eventsMu.Unlock()
	})
	if err != nil || result.IsError {
		t.Fatalf("Execute() = (%#v, %v)", result, err)
	}
	details := result.Details.(ExecDetails)
	if details.Status != "running" || details.SessionID == "" {
		t.Fatalf("Details = %#v", details)
	}
	if _, err := supervisor.Poll(context.Background(), details.SessionID, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	eventsMu.Lock()
	defer eventsMu.Unlock()
	if len(events) != 2 || events[0].Phase != agent.ToolEventStart || events[1].Phase != agent.ToolEventEnd {
		t.Fatalf("events after background completion = %#v", events)
	}
}

func TestExecToolYieldClosesStreamingGateBeforeToolEnd(t *testing.T) {
	supervisor := newProcessSupervisorForTest(t, t.TempDir())
	registry := newTestRegistry(t, agent.DefaultMiddlewareRegistrations(), NewExecTool(supervisor))
	var eventsMu sync.Mutex
	var events []agent.ToolEvent
	result, err := registry.Execute(context.Background(), ai.ToolCall{
		ID:   "exec-yield",
		Name: "exec",
		Arguments: execArguments(t, map[string]any{
			"command": toolHelperCommand("paced-output", "500"),
			"yieldMs": 200,
		}),
	}, func(_ context.Context, event agent.ToolEvent) {
		eventsMu.Lock()
		events = append(events, event)
		eventsMu.Unlock()
	})
	if err != nil || result.IsError {
		t.Fatalf("Execute() = (%#v, %v)", result, err)
	}
	details := result.Details.(ExecDetails)
	if details.Status != "running" || details.SessionID == "" {
		t.Fatalf("Details = %#v", details)
	}
	if _, err := supervisor.Poll(context.Background(), details.SessionID, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	eventsMu.Lock()
	defer eventsMu.Unlock()
	if len(events) != 3 || events[0].Phase != agent.ToolEventStart || events[1].Phase != agent.ToolEventUpdate || events[2].Phase != agent.ToolEventEnd {
		t.Fatalf("events after yielded completion = %#v", events)
	}
	if got := toolEventText(t, events[1].Update.Content); got != "early" {
		t.Fatalf("foreground update = %q", got)
	}
}

func TestExecToolTimeoutIsOrdinaryErrorAndKillsProcessGroup(t *testing.T) {
	supervisor := newProcessSupervisorForTest(t, t.TempDir())
	registry := newTestRegistry(t, agent.DefaultMiddlewareRegistrations(), NewExecTool(supervisor))
	marker := filepath.Join(t.TempDir(), "timeout-grandchild")
	result, err := registry.Execute(context.Background(), ai.ToolCall{
		ID:   "exec-timeout",
		Name: "exec",
		Arguments: execArguments(t, map[string]any{
			"command": toolHelperCommand("spawn-child", marker, "1500"),
			"timeout": 1,
			"yieldMs": 30_000,
		}),
	}, nil)
	if err != nil || !result.IsError {
		t.Fatalf("Execute() = (%#v, %v)", result, err)
	}
	details, ok := result.Details.(ExecDetails)
	if !ok || details.Status != "timed_out" {
		t.Fatalf("Details = %#v", result.Details)
	}
	time.Sleep(700 * time.Millisecond)
	if _, statErr := os.Stat(marker); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("grandchild survived timeout: %v", statErr)
	}
}

func TestExecToolPropagatesParentCancellationAsControlFlowError(t *testing.T) {
	supervisor := newProcessSupervisorForTest(t, t.TempDir())
	registry := newTestRegistry(t, agent.DefaultMiddlewareRegistrations(), NewExecTool(supervisor))
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	result, err := registry.Execute(ctx, ai.ToolCall{
		ID:        "exec-canceled",
		Name:      "exec",
		Arguments: execArguments(t, map[string]any{"command": toolHelperCommand("sleep", "5000"), "yieldMs": 30_000}),
	}, nil)
	if !errors.Is(err, context.Canceled) || !result.IsError {
		t.Fatalf("Execute() = (%#v, %v)", result, err)
	}
}

func TestExecToolRejectsLegacyFieldsAndInvalidSecondsOrMilliseconds(t *testing.T) {
	supervisor := newProcessSupervisorForTest(t, t.TempDir())
	registry := newTestRegistry(t, agent.DefaultMiddlewareRegistrations(), NewExecTool(supervisor))
	tests := []map[string]any{
		{"command": "true", "timeout_ms": 1000},
		{"command": "true", "yield_ms": 1},
		{"command": "true", "timeout": 0},
		{"command": "true", "timeout": 601},
		{"command": "true", "yieldMs": -1},
		{"command": "true", "yieldMs": 30_001},
		{"command": " ", "yieldMs": 0},
	}
	for index, input := range tests {
		result, err := registry.Execute(context.Background(), ai.ToolCall{
			ID:        "invalid-exec",
			Name:      "exec",
			Arguments: execArguments(t, input),
		}, nil)
		if err != nil || !result.IsError {
			t.Fatalf("case %d Execute() = (%#v, %v)", index, result, err)
		}
	}
}

func execArguments(t *testing.T, input map[string]any) json.RawMessage {
	t.Helper()
	arguments, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal arguments: %v", err)
	}
	return arguments
}

func toolOutputText(t *testing.T, output agent.ToolOutput) string {
	t.Helper()
	return toolEventText(t, output.Content)
}
