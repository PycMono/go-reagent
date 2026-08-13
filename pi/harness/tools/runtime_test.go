package tools

import (
	"context"
	"errors"
	"testing"

	"github.com/PycMono/go-reagent/pi/ai"
)

type testToolEventPhase string

const (
	testToolEventStart  testToolEventPhase = "start"
	testToolEventUpdate testToolEventPhase = "update"
	testToolEventEnd    testToolEventPhase = "end"
)

type testToolEvent struct {
	Phase  testToolEventPhase
	Update *ai.ToolUpdate
}

type testToolResult struct {
	Content []ai.ContentBlock
	Details any
	IsError bool
}

type testToolRuntime struct {
	tools map[string]ai.Tool
}

func newTestToolRuntime(t *testing.T, tools ...ai.Tool) *testToolRuntime {
	t.Helper()
	toolRuntime := &testToolRuntime{tools: make(map[string]ai.Tool, len(tools))}
	for _, tool := range tools {
		toolRuntime.tools[tool.Definition().Name] = tool
	}
	return toolRuntime
}

func (r *testToolRuntime) Execute(
	ctx context.Context,
	call ai.ToolCall,
	observer func(context.Context, testToolEvent),
) (testToolResult, error) {
	tool := r.tools[call.Name]
	if observer != nil {
		observer(ctx, testToolEvent{Phase: testToolEventStart})
	}
	output, executeErr := tool.Execute(ctx, call.Arguments, func(update ai.ToolUpdate) {
		if observer != nil {
			observer(ctx, testToolEvent{Phase: testToolEventUpdate, Update: &update})
		}
	})
	result := testToolResult{Content: output.Content, Details: output.Details, IsError: executeErr != nil}
	if executeErr != nil && len(result.Content) == 0 {
		result.Content = []ai.ContentBlock{ai.TextBlock(executeErr.Error())}
	}
	if observer != nil {
		observer(ctx, testToolEvent{Phase: testToolEventEnd})
	}
	if errors.Is(executeErr, context.Canceled) || errors.Is(executeErr, context.DeadlineExceeded) {
		return result, executeErr
	}
	return result, nil
}

func toolResultText(t *testing.T, result testToolResult) string {
	t.Helper()
	return toolEventText(t, result.Content)
}

func toolEventText(t *testing.T, content []ai.ContentBlock) string {
	t.Helper()
	text, err := ai.TextContent(content)
	if err != nil {
		t.Fatalf("ai.TextContent() error = %v", err)
	}
	return text
}
