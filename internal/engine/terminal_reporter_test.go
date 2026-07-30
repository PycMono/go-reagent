package engine

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

func TestTerminalReporterPrintsLifecycleEvents(t *testing.T) {
	var output bytes.Buffer
	reporter := newTerminalReporter(&output)
	ctx := context.Background()

	reporter.OnThinking(ctx)
	reporter.OnToolCall(ctx, "read_file", "line1\nline2\r")
	reporter.OnToolResult(ctx, "read_file", "ok", false)
	reporter.OnToolResult(ctx, "edit_file", "permission denied", true)
	reporter.OnMessage(ctx, "完成")

	got := output.String()
	for _, want := range []string{
		"思考中",
		"read_file",
		`line1\nline2\r`,
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
	reporter.OnMessage(context.Background(), "")
	if output.Len() != 0 {
		t.Fatalf("empty message output = %q", output.String())
	}

	var waitGroup sync.WaitGroup
	for index := range 16 {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			reporter.OnToolCall(context.Background(), fmt.Sprintf("tool-%d", index), "{}")
		}(index)
	}
	waitGroup.Wait()
	if got := strings.Count(output.String(), "[🛠️ 调用工具]"); got != 16 {
		t.Fatalf("tool-call output count = %d, want 16: %q", got, output.String())
	}
}
