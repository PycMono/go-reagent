package dispatch

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/PycMono/go-reagent/pi/agent"
	"github.com/PycMono/go-reagent/pi/ai"
)

const terminalArgumentLimit = 150

type terminalReporter struct {
	mu     sync.Mutex
	writer io.Writer
}

// NewTerminalReporter creates a Reporter that prints Agent lifecycle events.
func NewTerminalReporter() agent.Reporter {
	return newTerminalReporter(os.Stdout)
}

func newTerminalReporter(writer io.Writer) agent.Reporter {
	if writer == nil {
		writer = io.Discard
	}
	return &terminalReporter{writer: writer}
}

func (r *terminalReporter) Report(_ context.Context, event agent.AgentEvent) {
	switch event.Type {
	case agent.AgentEventThinking:
		r.write("\n[🤔 思考中] 模型正在推理...\n")
	case agent.AgentEventToolStart:
		if event.Tool == nil {
			return
		}
		r.write(fmt.Sprintf(
			"[🛠️ 调用工具] %s\n   参数: %s\n",
			event.Tool.Call.Name,
			terminalDisplayArguments(string(event.Tool.Call.Arguments)),
		))
	case agent.AgentEventToolUpdate:
		if event.Tool == nil || event.Tool.Call.Name != "exec" || event.Tool.Update == nil {
			return
		}
		if content := terminalEventText(event.Tool.Update.Content); content != "" {
			r.write(content)
		}
	case agent.AgentEventToolEnd:
		if event.Tool == nil || event.Tool.Result == nil {
			return
		}
		if !event.Tool.Result.IsError {
			r.write(fmt.Sprintf("[✅ 执行成功] %s\n", event.Tool.Call.Name))
			return
		}
		message := fmt.Sprintf("[❌ 执行失败] %s\n", event.Tool.Call.Name)
		if content := terminalEventText(event.Tool.Result.Content); content != "" {
			message += fmt.Sprintf("   错误: %s\n", content)
		}
		r.write(message)
	case agent.AgentEventMessage:
		if event.Message == nil {
			return
		}
		if content := terminalEventText(event.Message.Content); content != "" {
			r.write(fmt.Sprintf("\n🤖 Agent 回复:\n%s\n\n", content))
		}
	}
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

func terminalEventText(content []ai.ContentBlock) string {
	text, err := ai.TextContent(content)
	if err != nil {
		return ""
	}
	return text
}
