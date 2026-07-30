package dispatch_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/PycMono/go-reagent/internal/dispatch"
)

type webhookRequest struct {
	MsgType  string `json:"msgtype"`
	Markdown struct {
		Content string `json:"content"`
	} `json:"markdown"`
}

func TestWeComReporterSendsOneMarkdownRequestPerLifecycleEvent(t *testing.T) {
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
	reporter.OnThinking(ctx)
	reporter.OnToolCall(ctx, "read_file", `{"path":"a.txt"}`)
	reporter.OnToolResult(ctx, "read_file", "file A", false)
	reporter.OnMessage(ctx, "done")

	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 4 {
		t.Fatalf("request count = %d, want 4", len(requests))
	}
	wantContents := []string{
		"🤔 模型正在慢思考 (Thinking)...",
		"🛠️ **正在执行工具**：`read_file`\n参数：`{\"path\":\"a.txt\"}`",
		"✅ **执行成功** (read_file)",
		"done",
	}
	for index, want := range wantContents {
		if requests[index].MsgType != "markdown" || requests[index].Markdown.Content != want {
			t.Fatalf("request %d = %#v, want markdown %q", index, requests[index], want)
		}
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
	reporter.OnToolResult(context.Background(), "read_file", "permission denied", true)
	reporter.OnMessage(context.Background(), strings.Repeat("企", 2000))

	errorRequest := <-requests
	if got := errorRequest.Markdown.Content; got != "⚠️ **执行报错** (read_file)：\npermission denied" {
		t.Fatalf("error content = %q", got)
	}
	longRequest := <-requests
	if content := longRequest.Markdown.Content; len(content) > 4096 || !utf8.ValidString(content) || !strings.HasSuffix(content, "... (已截断)") {
		t.Fatalf("truncated content bytes = %d, valid = %v, suffix = %v", len(content), utf8.ValidString(content), strings.HasSuffix(content, "... (已截断)"))
	}
}
