package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestHTTPTransportHandlesJSONAndSession(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q", got)
		}
		if !strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
			t.Errorf("Accept = %q", r.Header.Get("Accept"))
		}
		if call == 1 {
			w.Header().Set("Mcp-Session-Id", "session-1")
		} else if got := r.Header.Get("Mcp-Session-Id"); got != "session-1" {
			t.Errorf("session header = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`)
	}))
	defer server.Close()

	transport, err := NewHTTPTransport(HTTPTransportOptions{Endpoint: server.URL, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	id := int64(1)
	for _, method := range []string{"initialize", "tools/list"} {
		response, err := transport.Send(context.Background(), Request{JSONRPC: "2.0", ID: &id, Method: method})
		if err != nil {
			t.Fatal(err)
		}
		if response.ID == nil || *response.ID != 1 {
			t.Fatalf("response = %#v", response)
		}
	}
}

func TestHTTPTransportParsesSSEAndSelectsMatchingResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		_, _ = io.WriteString(w, ": keepalive\n")
		_, _ = io.WriteString(w, "event: message\n")
		_, _ = io.WriteString(w, "data: {\"jsonrpc\":\"2.0\",\"id\":99,\n")
		_, _ = io.WriteString(w, "data: \"result\":{\"ignored\":true}}\n\n")
		_, _ = io.WriteString(w, "data: {\"jsonrpc\":\"2.0\",\"id\":7,\"result\":{\"ok\":true}}\n\n")
	}))
	defer server.Close()
	transport, err := NewHTTPTransport(HTTPTransportOptions{Endpoint: server.URL, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	id := int64(7)
	response, err := transport.Send(context.Background(), Request{JSONRPC: "2.0", ID: &id, Method: "tools/list"})
	if err != nil {
		t.Fatal(err)
	}
	if response.ID == nil || *response.ID != 7 || string(response.Result) != `{"ok":true}` {
		t.Fatalf("response = %#v", response)
	}
}

func TestHTTPTransportAcceptsEmptyNotificationResponses(t *testing.T) {
	for _, status := range []int{http.StatusAccepted, http.StatusNoContent} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
			}))
			defer server.Close()
			transport, err := NewHTTPTransport(HTTPTransportOptions{Endpoint: server.URL, Timeout: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			_, err = transport.Send(context.Background(), Request{JSONRPC: "2.0", Method: "notifications/initialized"})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestHTTPTransportRejectsInvalidResponsesWithoutLeakingBody(t *testing.T) {
	const secret = "never-print-mcp-secret"
	tests := []struct {
		name        string
		status      int
		contentType string
		body        string
	}{
		{name: "http status", status: http.StatusBadGateway, body: secret},
		{name: "content type", status: http.StatusOK, contentType: "text/plain", body: secret},
		{name: "malformed json", status: http.StatusOK, contentType: "application/json", body: `{` + secret},
		{name: "mismatched id", status: http.StatusOK, contentType: "application/json", body: `{"jsonrpc":"2.0","id":8,"result":{}}`},
		{name: "rpc error", status: http.StatusOK, contentType: "application/json", body: `{"jsonrpc":"2.0","id":7,"error":{"code":-1,"message":"` + secret + `"}}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if test.contentType != "" {
					w.Header().Set("Content-Type", test.contentType)
				}
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()
			transport, err := NewHTTPTransport(HTTPTransportOptions{Endpoint: server.URL, Timeout: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			id := int64(7)
			_, err = transport.Send(context.Background(), Request{JSONRPC: "2.0", ID: &id, Method: "tools/list"})
			if err == nil || strings.Contains(err.Error(), secret) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestHTTPTransportRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, strings.Repeat("x", int(maxHTTPResponseBytes)+1))
	}))
	defer server.Close()
	transport, err := NewHTTPTransport(HTTPTransportOptions{Endpoint: server.URL, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	id := int64(1)
	_, err = transport.Send(context.Background(), Request{JSONRPC: "2.0", ID: &id, Method: "tools/list"})
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("oversized response error = %v", err)
	}
}

func TestHTTPTransportHonorsTimeoutAndCallerCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{}}`)
	}))
	defer server.Close()

	t.Run("timeout", func(t *testing.T) {
		transport, err := NewHTTPTransport(HTTPTransportOptions{Endpoint: server.URL, Timeout: 20 * time.Millisecond})
		if err != nil {
			t.Fatal(err)
		}
		id := int64(1)
		_, err = transport.Send(context.Background(), Request{JSONRPC: "2.0", ID: &id, Method: "tools/list"})
		if err == nil || !strings.Contains(err.Error(), "deadline") {
			t.Fatalf("timeout error = %v", err)
		}
	})

	t.Run("caller cancellation", func(t *testing.T) {
		transport, err := NewHTTPTransport(HTTPTransportOptions{Endpoint: server.URL, Timeout: time.Second})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		id := int64(1)
		_, err = transport.Send(ctx, Request{JSONRPC: "2.0", ID: &id, Method: "tools/list"})
		if err == nil || !strings.Contains(err.Error(), "canceled") {
			t.Fatalf("cancellation error = %v", err)
		}
	})
}

func TestHTTPTransportValidatesEndpointHeadersAndRedirects(t *testing.T) {
	const secret = "never-print-mcp-secret"
	invalidOptions := []HTTPTransportOptions{
		{Endpoint: "http://example.com/mcp", Timeout: time.Second},
		{Endpoint: "https://user:password@example.com/mcp", Timeout: time.Second},
		{Endpoint: "https://example.com/mcp?key=" + secret, Timeout: time.Second},
		{Endpoint: "https://example.com/mcp#fragment", Timeout: time.Second},
		{Endpoint: "https://example.com/mcp", Timeout: time.Second, Headers: http.Header{"Host": {"bad"}}},
		{Endpoint: "https://example.com/mcp", Timeout: time.Second, Headers: http.Header{"Content-Length": {"4"}}},
		{Endpoint: "https://example.com/mcp", Timeout: time.Second, Headers: http.Header{"Mcp-Session-Id": {secret}}},
		{Endpoint: "https://example.com/mcp", Timeout: time.Second, Headers: http.Header{"X-Api-Key": {secret + "\r\nInjected: yes"}}},
		{Endpoint: "https://example.com/mcp", Timeout: time.Second, Headers: http.Header{"Bad Header": {secret}}},
	}
	for index, options := range invalidOptions {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			_, err := NewHTTPTransport(options)
			if err == nil || strings.Contains(err.Error(), secret) {
				t.Fatalf("constructor error = %v", err)
			}
		})
	}

	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	transport, err := NewHTTPTransport(HTTPTransportOptions{
		Endpoint: redirect.URL,
		Timeout:  time.Second,
		Headers:  http.Header{"Authorization": {"Bearer " + secret}},
	})
	if err != nil {
		t.Fatal(err)
	}
	id := int64(1)
	_, err = transport.Send(context.Background(), Request{JSONRPC: "2.0", ID: &id, Method: "tools/list"})
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("redirect error = %v", err)
	}
}

func TestHTTPTransportCloseDeletesSession(t *testing.T) {
	var deleted atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			if r.Header.Get("Mcp-Session-Id") == "session-close" {
				deleted.Store(true)
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Mcp-Session-Id", "session-close")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{}}`)
	}))
	defer server.Close()
	transport, err := NewHTTPTransport(HTTPTransportOptions{Endpoint: server.URL, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	id := int64(1)
	if _, err := transport.Send(context.Background(), Request{JSONRPC: "2.0", ID: &id, Method: "initialize"}); err != nil {
		t.Fatal(err)
	}
	if err := transport.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !deleted.Load() {
		t.Fatal("session was not deleted")
	}
}

type closeIdleTransportFake struct {
	closed atomic.Bool
}

func (*closeIdleTransportFake) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("unexpected request")
}

func (transport *closeIdleTransportFake) CloseIdleConnections() {
	transport.closed.Store(true)
}

func TestHTTPTransportCloseClosesDefaultIdleConnections(t *testing.T) {
	original := http.DefaultTransport
	recorder := &closeIdleTransportFake{}
	http.DefaultTransport = recorder
	t.Cleanup(func() { http.DefaultTransport = original })

	transport, err := NewHTTPTransport(HTTPTransportOptions{
		Endpoint: "http://127.0.0.1:1/mcp",
		Timeout:  time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := transport.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !recorder.closed.Load() {
		t.Fatal("default HTTP transport idle connections were not closed")
	}
}
