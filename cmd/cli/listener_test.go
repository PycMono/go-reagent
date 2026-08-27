package main

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/toolexec"
)

func newTestListener() (*terminalListener, *bytes.Buffer, *bytes.Buffer) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	return newTerminalListener(stdout, stderr, false), stdout, stderr
}

func TestListenerStreamsTextDeltaToStdoutOnly(t *testing.T) {
	listener, stdout, stderr := newTestListener()
	ctx := context.Background()

	listener.OnEvent(ctx, pi.NewMessageStartEvent())
	listener.OnEvent(ctx, pi.NewMessageUpdateEvent(ai.TextBlock("你好")))
	listener.OnEvent(ctx, pi.NewMessageUpdateEvent(ai.TextBlock("，世界")))
	listener.OnEvent(ctx, pi.NewMessageEndEvent(ai.Message{}))

	if stdout.String() != "你好，世界\n" {
		t.Fatalf("stdout = %q, want 流式文本 + 换行", stdout.String())
	}
	if strings.Contains(stderr.String(), "你好") {
		t.Fatalf("stderr 不应包含模型正文, got %q", stderr.String())
	}
}

func TestListenerSkipsNonTextDelta(t *testing.T) {
	listener, stdout, _ := newTestListener()
	listener.OnEvent(context.Background(), pi.NewMessageUpdateEvent(ai.ContentBlock{Type: ai.ContentType("thinking"), Text: "思考"}))
	if stdout.String() != "" {
		t.Fatalf("非文本 delta 不应进 stdout, got %q", stdout.String())
	}
	if listener.lineOpen {
		t.Fatal("非文本 delta 不应置 lineOpen")
	}
}

func TestListenerToolEventsWithDuration(t *testing.T) {
	listener, _, stderr := newTestListener()
	base := time.Now()
	listener.clock = func() time.Time { return base }
	ctx := context.Background()

	call := ai.ToolCall{ID: "c1", Name: "read", Arguments: []byte(`{"path":"main.go"}`)}
	listener.OnEvent(ctx, pi.NewAgentToolEvent(toolexec.NewStartEvent(call)))
	listener.clock = func() time.Time { return base.Add(120 * time.Millisecond) }
	listener.OnEvent(ctx, pi.NewAgentToolEvent(toolexec.NewEndEvent(call, toolexec.Result{
		ToolCallID: "c1", ToolName: "read",
	})))

	got := stderr.String()
	if !strings.Contains(got, "🔧 read") || !strings.Contains(got, "✅ read（120ms）") {
		t.Fatalf("工具事件输出不符合预期: %q", got)
	}
	if len(listener.toolStarts) != 0 {
		t.Fatal("tool_end 后计时表应清空该 ID")
	}
}

func TestListenerToolEndError(t *testing.T) {
	listener, _, stderr := newTestListener()
	call := ai.ToolCall{ID: "c2", Name: "exec"}
	listener.OnEvent(context.Background(), pi.NewAgentToolEvent(toolexec.NewEndEvent(call, toolexec.Result{
		ToolCallID: "c2", ToolName: "exec", IsError: true,
		Content: []ai.ContentBlock{ai.TextBlock("exit status 1")},
	})))
	if !strings.Contains(stderr.String(), "❌ exec") || !strings.Contains(stderr.String(), "exit status 1") {
		t.Fatalf("失败工具输出不符合预期: %q", stderr.String())
	}
}

// 工具轮完成（message_end 已换行）后，下一轮模型流式输出到一半被取消：
// closeLine 必须补换行。
func TestListenerCloseLineAfterCancelMidStream(t *testing.T) {
	listener, stdout, _ := newTestListener()
	ctx := context.Background()

	listener.OnEvent(ctx, pi.NewMessageUpdateEvent(ai.TextBlock("第一段")))
	listener.OnEvent(ctx, pi.NewMessageEndEvent(ai.Message{}))
	// 第二轮：工具后流式输出被 cancel，没有 message_end。
	listener.OnEvent(ctx, pi.NewMessageUpdateEvent(ai.TextBlock("第二段没说完")))
	listener.closeLine()

	if stdout.String() != "第一段\n第二段没说完\n" {
		t.Fatalf("stdout = %q, want 取消处补换行", stdout.String())
	}
	listener.closeLine() // 幂等
	if stdout.String() != "第一段\n第二段没说完\n" {
		t.Fatalf("closeLine 应幂等, stdout = %q", stdout.String())
	}
}

func TestListenerConcurrentEventsLineIntegrity(t *testing.T) {
	listener, _, _ := newTestListener()
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			call := ai.ToolCall{ID: strings.Repeat("x", 1) + string(rune('a'+i)), Name: "tool"}
			listener.OnEvent(ctx, pi.NewAgentToolEvent(toolexec.NewStartEvent(call)))
			listener.OnEvent(ctx, pi.NewAgentToolEvent(toolexec.NewEndEvent(call, toolexec.Result{})))
		}(i)
	}
	wg.Wait()
	if len(listener.toolStarts) != 0 {
		t.Fatalf("并发结束后计时表应为空, got %d", len(listener.toolStarts))
	}
}
