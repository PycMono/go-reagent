package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestExaHostedMCP(t *testing.T) {
	if os.Getenv("GO_REAGENT_EXA_INTEGRATION") != "1" {
		t.Skip("set GO_REAGENT_EXA_INTEGRATION=1 to run")
	}
	apiKey := strings.TrimSpace(os.Getenv("EXA_API_KEY"))
	if apiKey == "" {
		t.Skip("set EXA_API_KEY to run")
	}
	transport, err := NewHTTPTransport(HTTPTransportOptions{
		Endpoint: "https://mcp.exa.ai/mcp",
		Headers:  http.Header{"X-Api-Key": []string{apiKey}},
		Timeout:  60 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(transport, "go-reagent-integration-test", "1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = client.Close(closeCtx)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := client.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatal(err)
	}
	requireRemoteTools(t, tools, "web_search_exa", "web_fetch_exa")
	search, err := client.CallTool(ctx, "web_search_exa", json.RawMessage(`{"query":"official Go programming language website","numResults":1}`))
	if err != nil || search.IsError || len(search.Content) == 0 {
		t.Fatalf("search content count = %d, isError = %v, error = %v", len(search.Content), search.IsError, err)
	}
	fetched, err := client.CallTool(ctx, "web_fetch_exa", json.RawMessage(`{"url":"https://go.dev/"}`))
	if err != nil || fetched.IsError || len(fetched.Content) == 0 {
		t.Fatalf("fetch content count = %d, isError = %v, error = %v", len(fetched.Content), fetched.IsError, err)
	}
}

func requireRemoteTools(t *testing.T, tools []Tool, required ...string) {
	t.Helper()
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	for _, name := range required {
		if !slices.Contains(names, name) {
			t.Fatalf("required remote tool %q is missing; discovered names: %v", name, names)
		}
	}
}
