package wecom

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"unicode/utf8"

	"github.com/PycMono/go-reagent/pi"
)

type webhookRequest struct {
	MsgType  string `json:"msgtype"`
	Markdown struct {
		Content string `json:"content"`
	} `json:"markdown"`
}

func TestNotifySendsMarkdownToWebhook(t *testing.T) {
	requests := make(chan webhookRequest, 1)
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
		requests <- payload
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer server.Close()

	New(server.URL, server.Client()).Notify(context.Background(), pi.Notification{Kind: pi.NotificationRunError, Summary: "最终回复"})

	payload := <-requests
	if payload.MsgType != "markdown" || payload.Markdown.Content != "[告警] run_error\n最终回复" {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestNotifyTruncatesUTF8Safely(t *testing.T) {
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

	New(server.URL, server.Client()).Notify(context.Background(), pi.Notification{Kind: pi.NotificationRunError, Summary: strings.Repeat("企", 2000)})

	payload := <-requests
	if content := payload.Markdown.Content; len(content) > markdownMaxBytes || !utf8.ValidString(content) || !strings.HasSuffix(content, "... (已截断)") {
		t.Fatalf("truncated bytes = %d, valid = %v", len(content), utf8.ValidString(content))
	}
}

func TestNotifyFailureDoesNotPanic(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"errcode":93000,"errmsg":"invalid webhook"}`))
	}))
	defer server.Close()

	notifier := New(server.URL, server.Client())
	notifier.Notify(context.Background(), pi.Notification{Kind: pi.NotificationRunError, Summary: "x"})
	notifier.Notify(context.Background(), pi.Notification{Kind: pi.NotificationRunError, Summary: "y"})
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2（失败后仍可继续）", calls.Load())
	}
}
