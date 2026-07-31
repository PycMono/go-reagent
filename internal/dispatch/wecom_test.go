package dispatch_test

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

	"github.com/PycMono/go-reagent/internal/dispatch"
	"github.com/PycMono/go-reagent/internal/schema"
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

	reporter, err := dispatch.NewWeComReporter(server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewWeComReporter() error = %v", err)
	}
	ctx := context.Background()
	call := schema.ToolCall{ID: "call-1", Name: "read_file", Arguments: json.RawMessage(`{"path":"a.txt"}`)}
	reporter.Report(ctx, schema.NewThinkingEvent())
	reporter.Report(ctx, schema.NewToolUpdateEvent(call, schema.ToolUpdate{Content: []schema.ContentBlock{schema.TextBlock("chunk")}}))
	reporter.Report(ctx, schema.NewToolEndEvent(call, schema.ToolResult{ToolCallID: call.ID, ToolName: call.Name}))
	reporter.Report(ctx, schema.NewToolStartEvent(call))
	reporter.Report(ctx, schema.NewToolEndEvent(call, schema.ToolResult{ToolCallID: call.ID, ToolName: call.Name, Content: []schema.ContentBlock{schema.TextBlock("permission denied")}, IsError: true}))
	reporter.Report(ctx, schema.NewMessageEvent(schema.Message{
		Role:    schema.RoleAssistant,
		Content: []schema.ContentBlock{schema.TextBlock("done")},
	}))

	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 3 {
		t.Fatalf("request count = %d, want 3", len(requests))
	}
	wantContents := []string{
		"🛠️ **正在执行工具**：`read_file`\n参数：`{\"path\":\"a.txt\"}`",
		"⚠️ **执行报错** (read_file)：\npermission denied",
		"done",
	}
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

	reporter, err := dispatch.NewWeComReporter(server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewWeComReporter() error = %v", err)
	}

	for _, test := range []struct {
		name string
		role schema.Role
	}{
		{name: "user", role: schema.RoleUser},
		{name: "system", role: schema.RoleSystem},
		{name: "tool", role: schema.RoleTool},
	} {
		t.Run(test.name, func(t *testing.T) {
			reporter.Report(context.Background(), schema.NewMessageEvent(schema.Message{
				Role:    test.role,
				Content: []schema.ContentBlock{schema.TextBlock("must not send")},
			}))
			if got := requestCount.Load(); got != 0 {
				t.Fatalf("request count = %d, want 0", got)
			}
		})
	}
}

func TestWeComReporterFormatsToolErrorAndTruncatesUTF8(t *testing.T) {
	requests := make(chan webhookRequest, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var payload webhookRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
		}
		requests <- payload
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer server.Close()

	reporter, err := dispatch.NewWeComReporter(server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewWeComReporter() error = %v", err)
	}
	call := schema.ToolCall{ID: "call-1", Name: "read_file"}
	reporter.Report(context.Background(), schema.NewToolEndEvent(call, schema.ToolResult{
		ToolCallID: call.ID,
		ToolName:   call.Name,
		Content:    []schema.ContentBlock{schema.TextBlock("permission denied")},
		IsError:    true,
	}))
	reporter.Report(context.Background(), schema.NewMessageEvent(schema.Message{
		Role:    schema.RoleAssistant,
		Content: []schema.ContentBlock{schema.TextBlock(strings.Repeat("企", 2000))},
	}))

	errorRequest := <-requests
	if got := errorRequest.Markdown.Content; got != "⚠️ **执行报错** (read_file)：\npermission denied" {
		t.Fatalf("error content = %q", got)
	}
	longRequest := <-requests
	if content := longRequest.Markdown.Content; len(content) > 4096 || !utf8.ValidString(content) || !strings.HasSuffix(content, "... (已截断)") {
		t.Fatalf("truncated content bytes = %d, valid = %v, suffix = %v", len(content), utf8.ValidString(content), strings.HasSuffix(content, "... (已截断)"))
	}
}
