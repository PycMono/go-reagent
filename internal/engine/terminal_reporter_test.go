package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/PycMono/go-reagent/internal/schema"
)

func TestTerminalReporterPrintsLifecycleEvents(t *testing.T) {
	var output bytes.Buffer
	reporter := newTerminalReporter(&output)
	ctx := context.Background()
	call := schema.ToolCall{ID: "call-1", Name: "read_file", Arguments: json.RawMessage(`{"path":"a.txt"}`)}
	execCall := schema.ToolCall{ID: "call-2", Name: "exec", Arguments: json.RawMessage(`{"command":"go test"}`)}

	reporter.Report(ctx, schema.NewThinkingEvent())
	reporter.Report(ctx, schema.NewToolStartEvent(call))
	reporter.Report(ctx, schema.NewToolUpdateEvent(execCall, schema.ToolUpdate{
		Content: []schema.ContentBlock{schema.TextBlock("stderr chunk")},
		Details: map[string]any{"stream": "stderr", "bytes": 4},
	}))
	reporter.Report(ctx, schema.NewToolEndEvent(call, schema.ToolResult{
		ToolCallID: call.ID,
		ToolName:   call.Name,
		Content:    []schema.ContentBlock{schema.TextBlock("ok")},
	}))
	failedCall := schema.ToolCall{ID: "call-3", Name: "edit_file"}
	reporter.Report(ctx, schema.NewToolEndEvent(failedCall, schema.ToolResult{
		ToolCallID: failedCall.ID,
		ToolName:   failedCall.Name,
		Content:    []schema.ContentBlock{schema.TextBlock("permission denied")},
		IsError:    true,
	}))
	reporter.Report(ctx, schema.NewMessageEvent(schema.Message{
		Role:    schema.RoleAssistant,
		Content: []schema.ContentBlock{schema.TextBlock("完成")},
	}))

	got := output.String()
	for _, want := range []string{
		"思考中",
		"read_file",
		"stderr chunk",
		"执行成功",
		"执行失败",
		"permission denied",
		"Agent 回复",
		"完成",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("terminal output missing %q: %q", want, got)
		}
	}
}

func TestTerminalDisplayArgumentsTruncatesAtRuneBoundary(t *testing.T) {
	got := terminalDisplayArguments(strings.Repeat("界", 151))
	want := strings.Repeat("界", 150) + "... (已截断)"
	if got != want {
		t.Fatalf("terminalDisplayArguments() = %q, want %q", got, want)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("terminalDisplayArguments() returned invalid UTF-8: %q", got)
	}
}

func TestTerminalReporterIgnoresEmptyMessageAndSerializesConcurrentEvents(t *testing.T) {
	var output bytes.Buffer
	reporter := newTerminalReporter(&output)
	reporter.Report(context.Background(), schema.NewMessageEvent(schema.Message{Role: schema.RoleAssistant}))
	if output.Len() != 0 {
		t.Fatalf("empty message output = %q", output.String())
	}

	var waitGroup sync.WaitGroup
	for index := range 16 {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			reporter.Report(context.Background(), schema.NewToolStartEvent(schema.ToolCall{
				Name:      fmt.Sprintf("tool-%d", index),
				Arguments: json.RawMessage(`{}`),
			}))
		}(index)
	}
	waitGroup.Wait()
	if got := strings.Count(output.String(), "[🛠️ 调用工具]"); got != 16 {
		t.Fatalf("tool-call output count = %d, want 16: %q", got, output.String())
	}
}
