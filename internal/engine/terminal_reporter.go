package engine

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

const terminalArgumentLimit = 150

type terminalReporter struct {
	mu     sync.Mutex
	writer io.Writer
}

// NewTerminalReporter creates a Reporter that prints Agent lifecycle events.
func NewTerminalReporter() Reporter {
	return newTerminalReporter(os.Stdout)
}

func newTerminalReporter(writer io.Writer) Reporter {
	if writer == nil {
		writer = io.Discard
	}
	return &terminalReporter{writer: writer}
}

func (r *terminalReporter) OnThinking(context.Context) {
	r.write("\n[🤔 思考中] 模型正在推理...\n")
}

func (r *terminalReporter) OnToolCall(_ context.Context, toolName string, arguments string) {
	r.write(fmt.Sprintf(
		"[🛠️ 调用工具] %s\n   参数: %s\n",
		toolName,
		terminalDisplayArguments(arguments),
	))
}

func (r *terminalReporter) OnToolResult(_ context.Context, toolName string, result string, isError bool) {
	if !isError {
		r.write(fmt.Sprintf("[✅ 执行成功] %s\n", toolName))
		return
	}

	message := fmt.Sprintf("[❌ 执行失败] %s\n", toolName)
	if result != "" {
		message += fmt.Sprintf("   错误: %s\n", result)
	}
	r.write(message)
}

func (r *terminalReporter) OnMessage(_ context.Context, content string) {
	if content == "" {
		return
	}
	r.write(fmt.Sprintf("\n🤖 Agent 回复:\n%s\n\n", content))
}

func (r *terminalReporter) write(message string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, _ = io.WriteString(r.writer, message)
}

func terminalDisplayArguments(arguments string) string {
	display := strings.NewReplacer("\n", `\n`, "\r", `\r`).Replace(arguments)
	runes := []rune(display)
	if len(runes) <= terminalArgumentLimit {
		return display
	}
	return string(runes[:terminalArgumentLimit]) + "... (已截断)"
}
