package integration_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/PycMono/go-reagent"
	"github.com/PycMono/go-reagent/agent"
	"github.com/PycMono/go-reagent/ai"
	"github.com/PycMono/go-reagent/internal/cli/dispatch"
)

func TestReporterRoutesExecUpdatesOnlyToTerminal(t *testing.T) {
	var (
		mu       sync.Mutex
		requests int
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		requests++
		mu.Unlock()
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer server.Close()

	registrations, err := dispatch.NewReporterRegistrations(&reagent.Config{
		Bot: reagent.BotConfig{
			WeCom: reagent.WeComConfig{WebhookURL: server.URL},
		},
	})
	if err != nil {
		t.Fatalf("NewReporterRegistrations() error = %v", err)
	}
	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	originalStdout := os.Stdout
	os.Stdout = writeEnd
	terminal := dispatch.NewTerminalReporter()
	os.Stdout = originalStdout
	registrations = append(registrations, agent.ReporterRegistration{Name: "terminal", Order: 100, Reporter: terminal})
	reporter := agent.NewMultiReporter(registrations)
	ctx := context.Background()
	execCall := ai.ToolCall{ID: "exec-1", Name: "exec", Arguments: []byte(`{"command":"go test ./..."}`)}
	reporter.Report(ctx, agent.NewToolUpdateEvent(execCall, agent.ToolUpdate{
		Content: []ai.ContentBlock{ai.TextBlock("streamed output")},
	}))
	mu.Lock()
	requestsAfterUpdate := requests
	mu.Unlock()
	if requestsAfterUpdate != 0 {
		t.Fatalf("WeCom requests after exec update = %d, want 0", requestsAfterUpdate)
	}

	reporter.Report(ctx, agent.NewToolStartEvent(execCall))
	reporter.Report(ctx, agent.NewToolEndEvent(execCall, agent.ToolResult{
		ToolCallID: execCall.ID,
		ToolName:   execCall.Name,
		Content:    []ai.ContentBlock{ai.TextBlock("exit status 1")},
		IsError:    true,
	}))
	reporter.Report(ctx, agent.NewMessageEvent(ai.Message{
		Role:    ai.RoleAssistant,
		Content: []ai.ContentBlock{ai.TextBlock("final answer")},
	}))
	mu.Lock()
	finalRequests := requests
	mu.Unlock()
	if finalRequests != 3 {
		t.Fatalf("WeCom request count = %d, want 3", finalRequests)
	}

	if err := writeEnd.Close(); err != nil {
		t.Fatal(err)
	}
	terminalOutput, err := io.ReadAll(readEnd)
	if err != nil {
		t.Fatal(err)
	}
	if err := readEnd.Close(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(terminalOutput, []byte("streamed output")) || strings.Count(string(terminalOutput), "streamed output") != 1 {
		t.Fatalf("terminal output = %q, want one exec update", terminalOutput)
	}
}
