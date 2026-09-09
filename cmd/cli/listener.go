package main

// terminalListener 实现 pi.EventListener：模型正文流式写 stdout，工具进度与
// 错误写 stderr，保证 stdout 纯净（可管道）。所有写入与同stdoutLineOpen、
// 工具计时表由同一把 mutex 保护——工具事件可能并发到达。

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
)

type terminalListener struct {
	mu         sync.Mutex
	stdout     io.Writer
	stderr     io.Writer
	verbose    bool
	started    time.Time
	clock      func() time.Time
	lineOpen   bool // stdoutLineOpen：已写出未换行的文本 delta
	toolStarts map[string]time.Time
}

func newTerminalListener(stdout, stderr io.Writer, verbose bool) *terminalListener {
	return &terminalListener{
		stdout:     stdout,
		stderr:     stderr,
		verbose:    verbose,
		clock:      time.Now,
		toolStarts: make(map[string]time.Time),
	}
}

func (l *terminalListener) OnEvent(_ context.Context, event pi.AgentEvent) {
	l.mu.Lock()
	defer l.mu.Unlock()

	switch event.Type {
	case pi.AgentEventMessageStart:
		l.started = l.clock()
		fmt.Fprintln(l.stderr, "🤖")
	case pi.AgentEventMessageUpdate:
		if event.Delta == nil {
			return
		}
		if event.Delta.Type != ai.ContentTypeText {
			if l.verbose {
				fmt.Fprintf(l.stderr, "（跳过非文本块 %s）\n", event.Delta.Type)
			}
			return
		}
		if event.Delta.Text == "" {
			return
		}
		fmt.Fprint(l.stdout, event.Delta.Text)
		l.lineOpen = true
	case pi.AgentEventMessageEnd:
		if l.lineOpen {
			fmt.Fprintln(l.stdout)
			l.lineOpen = false
		}
	case pi.AgentEventToolStart:
		if event.Tool == nil {
			return
		}
		l.toolStarts[event.Tool.Call.ID] = l.clock()
		fmt.Fprintf(l.stderr, "🔧 %s %s\n", event.Tool.Call.Name, truncate(string(event.Tool.Call.Arguments), 200))
	case pi.AgentEventToolUpdate:
		if l.verbose && event.Tool != nil {
			fmt.Fprintf(l.stderr, "… %s 执行中\n", event.Tool.Call.Name)
		}
	case pi.AgentEventToolEnd:
		if event.Tool == nil {
			return
		}
		duration := ""
		if start, ok := l.toolStarts[event.Tool.Call.ID]; ok {
			duration = fmt.Sprintf("（%s）", l.clock().Sub(start).Round(time.Millisecond))
			delete(l.toolStarts, event.Tool.Call.ID)
		}
		if event.Tool.IsError {
			fmt.Fprintf(l.stderr, "❌ %s%s %s\n", event.Tool.Call.Name, duration,
				truncate(resultText(event.Tool.Content), 200))
			return
		}
		fmt.Fprintf(l.stderr, "✅ %s%s\n", event.Tool.Call.Name, duration)
	}
}

// closeLine 在 Run 结束/取消时调用：stdout 上仍有未换行的 delta 时补一个
// 换行，让终端提示符回到行首。返回是否补了换行。
func (l *terminalListener) closeLine() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.lineOpen {
		fmt.Fprintln(l.stdout)
		l.lineOpen = false
	}
	// 取消场景下 tool_end 可能永远不到达，清空计时表避免泄漏到下一轮。
	l.toolStarts = make(map[string]time.Time)
}

func resultText(blocks ai.ContentBlocks) string {
	text, err := blocks.Text()
	if err != nil {
		return fmt.Sprintf("（非文本结果 %d 块）", len(blocks))
	}
	return text
}

func truncate(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
