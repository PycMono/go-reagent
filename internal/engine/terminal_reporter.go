package engine

import (
	"context"
	"fmt"
)

type terminalReporter struct{}

// NewTerminalReporter preserves the current user-facing terminal response.
func NewTerminalReporter() Reporter {
	return terminalReporter{}
}

func (terminalReporter) OnThinking(context.Context) {}

func (terminalReporter) OnToolCall(context.Context, string, string) {}

func (terminalReporter) OnToolResult(context.Context, string, string, bool) {}

func (terminalReporter) OnMessage(_ context.Context, content string) {
	fmt.Printf("🤖 [对外回复]: %s\n", content)
}
