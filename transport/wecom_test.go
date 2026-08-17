package transport_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"unicode/utf8"

	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/transport"
)

type webhookRequest struct {
	MsgType  string `json:"msgtype"`
	Markdown struct {
		Content string `json:"content"`
	} `json:"markdown"`
}

func TestWeComReporterFiltersAgentEvents(t *testing.T) {
	var (
		mu       sync.Mutex
		requests []webhookRequest
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", request.Method)
		}
		if got := request.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q", got)
		}
		var payload webhookRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
		}
		mu.Lock()
		requests = append(requests, payload)
		mu.Unlock()
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer server.Close()

	reporter, err := transport.NewWeComReporter(server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewWeComReporter() error = %v", err)
	}
	ctx := context.Background()
	call := ai.ToolCall{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"a.txt"}`)}
	reporter.Report(ctx, pi.NewThinkingEvent())
	reporter.Report(ctx, pi.NewToolUpdateEvent(call, ai.ToolUpdate{Content: []ai.ContentBlock{ai.TextBlock("chunk")}}))
	reporter.Report(ctx, pi.NewToolEndEvent(call, pi.ToolResult{ToolCallID: call.ID, ToolName: call.Name}))
	reporter.Report(ctx, pi.NewToolStartEvent(call))
	reporter.Report(ctx, pi.NewToolEndEvent(call, pi.ToolResult{ToolCallID: call.ID, ToolName: call.Name, Content: []ai.ContentBlock{ai.TextBlock("permission denied")}, IsError: true}))
	reporter.Report(ctx, pi.NewMessageStartEvent())
	reporter.Report(ctx, pi.NewMessageUpdateEvent(ai.TextBlock("do")))
	reporter.Report(ctx, pi.NewMessageEndEvent(ai.Message{
		Role: ai.RoleAssistant, Content: []ai.ContentBlock{ai.TextBlock("intermediate")},
		ToolCalls: []ai.ToolCall{{ID: "call-2", Name: "read", Arguments: json.RawMessage(`{"path":"b.txt"}`)}},
	}))
	reporter.Report(ctx, pi.NewMessageStartEvent())
	reporter.Report(ctx, pi.NewMessageUpdateEvent(ai.TextBlock("done")))
	reporter.Report(ctx, pi.NewMessageEndEvent(ai.Message{
		Role:    ai.RoleAssistant,
		Content: []ai.ContentBlock{ai.TextBlock("done")},
	}))

	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 1 {
		t.Fatalf("request count = %d, want 1", len(requests))
	}
	wantContents := []string{"done"}
	for index, want := range wantContents {
		if requests[index].MsgType != "markdown" || requests[index].Markdown.Content != want {
			t.Fatalf("request %d = %#v, want markdown %q", index, requests[index], want)
		}
	}
}

func TestWeComReporterIgnoresNonAssistantMessages(t *testing.T) {
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount.Add(1)
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer server.Close()

	reporter, err := transport.NewWeComReporter(server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewWeComReporter() error = %v", err)
	}

	for _, test := range []struct {
		name string
		role ai.Role
	}{
		{name: "user", role: ai.RoleUser},
		{name: "system", role: ai.RoleSystem},
		{name: "tool", role: ai.RoleTool},
	} {
		t.Run(test.name, func(t *testing.T) {
			reporter.Report(context.Background(), pi.NewMessageEndEvent(ai.Message{
				Role:    test.role,
				Content: []ai.ContentBlock{ai.TextBlock("must not send")},
			}))
			if got := requestCount.Load(); got != 0 {
				t.Fatalf("request count = %d, want 0", got)
			}
		})
	}
}

func TestWeComReporterFormatsToolErrorAndTruncatesUTF8(t *testing.T) {
	requests := make(chan webhookRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var payload webhookRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
		}
		requests <- payload
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer server.Close()

	reporter, err := transport.NewWeComReporter(server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewWeComReporter() error = %v", err)
	}
	reporter.Report(context.Background(), pi.NewMessageEndEvent(ai.Message{
		Role:    ai.RoleAssistant,
		Content: []ai.ContentBlock{ai.TextBlock(strings.Repeat("企", 2000))},
	}))

	longRequest := <-requests
	if content := longRequest.Markdown.Content; len(content) > 4096 || !utf8.ValidString(content) || !strings.HasSuffix(content, "... (已截断)") {
		t.Fatalf("truncated content bytes = %d, valid = %v, suffix = %v", len(content), utf8.ValidString(content), strings.HasSuffix(content, "... (已截断)"))
	}
}
