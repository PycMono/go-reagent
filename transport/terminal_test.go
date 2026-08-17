package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
)

func TestTerminalReporterPrintsLifecycleEvents(t *testing.T) {
	var output bytes.Buffer
	reporter := newTerminalReporter(&output)
	ctx := context.Background()
	call := ai.ToolCall{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"a.txt"}`)}
	execCall := ai.ToolCall{ID: "call-2", Name: "exec", Arguments: json.RawMessage(`{"command":"go test"}`)}

	reporter.Report(ctx, pi.NewThinkingEvent())
	reporter.Report(ctx, pi.NewToolStartEvent(call))
	reporter.Report(ctx, pi.NewToolUpdateEvent(execCall, ai.ToolUpdate{
		Content: []ai.ContentBlock{ai.TextBlock("stderr chunk")},
		Details: map[string]any{"stream": "stderr", "bytes": 4},
	}))
	reporter.Report(ctx, pi.NewToolEndEvent(call, pi.ToolResult{
		ToolCallID: call.ID,
		ToolName:   call.Name,
		Content:    []ai.ContentBlock{ai.TextBlock("ok")},
	}))
	failedCall := ai.ToolCall{ID: "call-3", Name: "edit"}
	reporter.Report(ctx, pi.NewToolEndEvent(failedCall, pi.ToolResult{
		ToolCallID: failedCall.ID,
		ToolName:   failedCall.Name,
		Content:    []ai.ContentBlock{ai.TextBlock("permission denied")},
		IsError:    true,
	}))
	reporter.Report(ctx, pi.NewMessageStartEvent())
	reporter.Report(ctx, pi.NewMessageUpdateEvent(ai.TextBlock("完")))
	reporter.Report(ctx, pi.NewMessageUpdateEvent(ai.TextBlock("成")))
	reporter.Report(ctx, pi.NewMessageEndEvent(ai.Message{
		Role:    ai.RoleAssistant,
		Content: []ai.ContentBlock{ai.TextBlock("完成")},
	}))

	got := output.String()
	for _, want := range []string{
		"思考中",
		"read",
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
	if strings.Count(got, "完成") != 1 {
		t.Fatalf("streamed reply was duplicated: %q", got)
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
	reporter.Report(context.Background(), pi.NewMessageStartEvent())
	reporter.Report(context.Background(), pi.NewMessageEndEvent(ai.Message{Role: ai.RoleAssistant}))
	if got := output.String(); !strings.Contains(got, "Agent 回复") || strings.Contains(got, "空消息") {
		t.Fatalf("empty streamed message output = %q", got)
	}
	output.Reset()

	var waitGroup sync.WaitGroup
	for index := range 16 {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			reporter.Report(context.Background(), pi.NewToolStartEvent(ai.ToolCall{
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
