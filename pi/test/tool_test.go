package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/pi/ai"
)

type toolCallerFake struct {
	name      string
	arguments json.RawMessage
	result    CallToolResult
	err       error
}

func (caller *toolCallerFake) CallTool(_ context.Context, name string, arguments json.RawMessage) (CallToolResult, error) {
	caller.name = name
	caller.arguments = append(json.RawMessage(nil), arguments...)
	return caller.result, caller.err
}

func TestProxyToolMapsDefinitionAndText(t *testing.T) {
	caller := &toolCallerFake{result: CallToolResult{
		Content: []Content{{Type: "text", Text: "result one"}, {Type: "text", Text: "result two"}},
	}}
	tool := newProxyTool(caller, Tool{
		Name: "web_search_exa", Title: "Web search", Description: "Search the web",
		InputSchema: map[string]any{"type": "object"},
	}, "web_search_exa")
	arguments := json.RawMessage(`{"query":"go"}`)
	output, err := tool.Execute(context.Background(), arguments, nil)
	if err != nil {
		t.Fatal(err)
	}
	definition := tool.Definition()
	if definition.Name != "web_search_exa" || definition.Label != "Web search" ||
		definition.Description != "Search the web" || definition.ParallelSafe {
		t.Fatalf("definition = %#v", definition)
	}
	if got, _ := ai.TextContent(output.Content); got != "result one\nresult two" {
		t.Fatalf("text = %q", got)
	}
	if caller.name != "web_search_exa" || string(caller.arguments) != string(arguments) {
		t.Fatalf("forwarded = %q %s", caller.name, caller.arguments)
	}
}

func TestProxyToolUsesStructuredContentWhenTextIsAbsent(t *testing.T) {
	caller := &toolCallerFake{result: CallToolResult{StructuredContent: map[string]any{"answer": "ok"}}}
	tool := newProxyTool(caller, Tool{Name: "remote", InputSchema: map[string]any{"type": "object"}}, "remote")
	output, err := tool.Execute(context.Background(), json.RawMessage(`{}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := ai.TextContent(output.Content); got != `{"answer":"ok"}` {
		t.Fatalf("structured text = %q", got)
	}
}

func TestProxyToolRejectsUnsupportedOrRemoteErrorContent(t *testing.T) {
	tests := []struct {
		name   string
		result CallToolResult
		want   string
	}{
		{name: "unsupported", result: CallToolResult{Content: []Content{{Type: "image"}}}, want: "unsupported"},
		{name: "mixed", result: CallToolResult{Content: []Content{{Type: "text", Text: "ok"}, {Type: "image"}}}, want: "unsupported"},
		{name: "remote error", result: CallToolResult{Content: []Content{{Type: "text", Text: "remote failed"}}, IsError: true}, want: "remote tool"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tool := newProxyTool(&toolCallerFake{result: test.result}, Tool{Name: "remote", InputSchema: map[string]any{"type": "object"}}, "remote")
			_, err := tool.Execute(context.Background(), json.RawMessage(`{}`), nil)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Execute error = %v", err)
			}
			if strings.Contains(err.Error(), "remote failed") {
				t.Fatalf("remote content leaked in error = %v", err)
			}
		})
	}
}

func TestProxyToolPropagatesCallerErrorWithoutArguments(t *testing.T) {
	const secret = "never-print-tool-argument"
	caller := &toolCallerFake{err: context.Canceled}
	tool := newProxyTool(caller, Tool{Name: "remote", InputSchema: map[string]any{"type": "object"}}, "remote")
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"secret":"`+secret+`"}`), nil)
	if !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), secret) {
		t.Fatalf("Execute error = %v", err)
	}
}
