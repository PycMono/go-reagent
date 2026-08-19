package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
)

type clientTransportFake struct {
	mu        sync.Mutex
	requests  []Request
	responses []Response
	errors    []error
	closed    int
}

func (transport *clientTransportFake) Send(_ context.Context, request Request) (Response, error) {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	transport.requests = append(transport.requests, request)
	index := len(transport.requests) - 1
	if index < len(transport.errors) && transport.errors[index] != nil {
		return Response{}, transport.errors[index]
	}
	if index >= len(transport.responses) {
		return Response{}, errors.New("unexpected request")
	}
	response := transport.responses[index]
	if response.ID == nil && request.ID != nil {
		id := *request.ID
		response.ID = &id
	}
	return response, nil
}

func (transport *clientTransportFake) Close(context.Context) error {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	transport.closed++
	return nil
}

func rpcResult(t *testing.T, value any) Response {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return Response{JSONRPC: "2.0", Result: data}
}

func initializedClientFake(t *testing.T, tail ...Response) (*Client, *clientTransportFake) {
	t.Helper()
	transport := &clientTransportFake{responses: append([]Response{
		rpcResult(t, map[string]any{
			"protocolVersion": ProtocolVersion,
			"capabilities":    map[string]any{},
			"serverInfo":      map[string]any{"name": "test-server", "version": "1"},
		}),
		{},
	}, tail...)}
	client, err := NewClient(transport, "go-reagent-test", "1")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	return client, transport
}

func TestClientInitializesListsPagesAndCallsTool(t *testing.T) {
	client, transport := initializedClientFake(t,
		rpcResult(t, map[string]any{
			"tools": []any{map[string]any{
				"name": "web_search_exa", "description": "search", "inputSchema": map[string]any{"type": "object"},
			}},
			"nextCursor": "page-2",
		}),
		rpcResult(t, map[string]any{
			"tools": []any{map[string]any{
				"name": "web_fetch_exa", "description": "fetch", "inputSchema": map[string]any{"type": "object"},
			}},
		}),
		rpcResult(t, map[string]any{
			"content": []any{map[string]any{"type": "text", "text": "found"}},
		}),
	)

	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{tools[0].Name, tools[1].Name}; !slices.Equal(got, []string{"web_search_exa", "web_fetch_exa"}) {
		t.Fatalf("tools = %v", got)
	}
	arguments := json.RawMessage(`{"query":"go","numResults":1}`)
	result, err := client.CallTool(context.Background(), "web_search_exa", arguments)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Content) != 1 || result.Content[0].Text != "found" {
		t.Fatalf("call result = %#v", result)
	}

	methods := make([]string, len(transport.requests))
	for index, request := range transport.requests {
		methods[index] = request.Method
	}
	wantMethods := []string{"initialize", "notifications/initialized", "tools/list", "tools/list", "tools/call"}
	if !slices.Equal(methods, wantMethods) {
		t.Fatalf("methods = %v, want %v", methods, wantMethods)
	}
	initialize, ok := transport.requests[0].Params.(initializeParams)
	if !ok || initialize.ProtocolVersion != ProtocolVersion ||
		initialize.ClientInfo.Name != "go-reagent-test" || initialize.ClientInfo.Version != "1" {
		t.Fatalf("initialize params = %#v", transport.requests[0].Params)
	}
	secondList, ok := transport.requests[3].Params.(listToolsParams)
	if !ok || secondList.Cursor != "page-2" {
		t.Fatalf("second list params = %#v", transport.requests[3].Params)
	}
	call, ok := transport.requests[4].Params.(callToolParams)
	if !ok || call.Name != "web_search_exa" || !reflect.DeepEqual(call.Arguments, map[string]any{"query": "go", "numResults": json.Number("1")}) {
		t.Fatalf("call params = %#v", transport.requests[4].Params)
	}
}

func TestClientPropagatesTransportCancellation(t *testing.T) {
	client, transport := initializedClientFake(t, Response{})
	transport.errors = append([]error{nil, nil}, context.Canceled)
	_, err := client.ListTools(context.Background())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ListTools error = %v", err)
	}
}

func TestClientRejectsProtocolMismatch(t *testing.T) {
	transport := &clientTransportFake{responses: []Response{rpcResult(t, map[string]any{
		"protocolVersion": "2024-11-05", "capabilities": map[string]any{},
	})}}
	client, err := NewClient(transport, "test", "1")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Initialize(context.Background()); err == nil || !strings.Contains(err.Error(), "protocol") {
		t.Fatalf("Initialize error = %v", err)
	}
	if len(transport.requests) != 1 {
		t.Fatalf("requests = %#v", transport.requests)
	}
}

func TestClientRequiresInitialization(t *testing.T) {
	client, err := NewClient(&clientTransportFake{}, "test", "1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListTools(context.Background()); err == nil || !strings.Contains(err.Error(), "initialized") {
		t.Fatalf("ListTools error = %v", err)
	}
	if _, err := client.CallTool(context.Background(), "tool", json.RawMessage(`{}`)); err == nil || !strings.Contains(err.Error(), "initialized") {
		t.Fatalf("CallTool error = %v", err)
	}
}

func TestClientRejectsDuplicateToolsAndCursorCycles(t *testing.T) {
	t.Run("duplicate tool", func(t *testing.T) {
		client, _ := initializedClientFake(t,
			rpcResult(t, map[string]any{
				"tools": []any{map[string]any{
					"name": "same", "inputSchema": map[string]any{"type": "object"},
				}},
				"nextCursor": "next",
			}),
			rpcResult(t, map[string]any{
				"tools": []any{map[string]any{
					"name": "same", "inputSchema": map[string]any{"type": "object"},
				}},
			}),
		)
		if _, err := client.ListTools(context.Background()); err == nil || !strings.Contains(err.Error(), "duplicate") {
			t.Fatalf("duplicate error = %v", err)
		}
	})

	t.Run("cursor cycle", func(t *testing.T) {
		client, _ := initializedClientFake(t,
			rpcResult(t, map[string]any{"tools": []any{}, "nextCursor": "same"}),
			rpcResult(t, map[string]any{"tools": []any{}, "nextCursor": "same"}),
		)
		if _, err := client.ListTools(context.Background()); err == nil || !strings.Contains(err.Error(), "cursor") {
			t.Fatalf("cursor error = %v", err)
		}
	})
}

func TestClientRejectsMalformedArgumentsAndResults(t *testing.T) {
	t.Run("arguments", func(t *testing.T) {
		client, _ := initializedClientFake(t)
		for _, arguments := range []json.RawMessage{json.RawMessage(`[]`), json.RawMessage(`{} {}`)} {
			if _, err := client.CallTool(context.Background(), "tool", arguments); err == nil {
				t.Fatalf("arguments %s accepted", arguments)
			}
		}
	})
	t.Run("result", func(t *testing.T) {
		client, _ := initializedClientFake(t, Response{JSONRPC: "2.0", Result: json.RawMessage(`{`)})
		if _, err := client.ListTools(context.Background()); err == nil || !strings.Contains(err.Error(), "decode") {
			t.Fatalf("result error = %v", err)
		}
	})
}

func TestClientReturnsMCPToolErrorResult(t *testing.T) {
	client, _ := initializedClientFake(t, rpcResult(t, map[string]any{
		"content": []any{map[string]any{"type": "text", "text": "remote failed"}},
		"isError": true,
	}))
	result, err := client.CallTool(context.Background(), "web_search_exa", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError || result.Content[0].Text != "remote failed" {
		t.Fatalf("result = %#v", result)
	}
}

func TestClientCloseIsIdempotentAfterSuccess(t *testing.T) {
	client, transport := initializedClientFake(t)
	if err := client.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := client.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if transport.closed != 1 {
		t.Fatalf("Close calls = %d", transport.closed)
	}
	if _, err := client.ListTools(context.Background()); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("ListTools after close error = %v", err)
	}
}
