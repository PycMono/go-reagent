package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
)

type extensionClientFake struct {
	initialized int
	tools       []Tool
	listErr     error
	closed      int
}

func (client *extensionClientFake) Initialize(context.Context) error {
	client.initialized++
	return nil
}

func (client *extensionClientFake) ListTools(context.Context) ([]Tool, error) {
	return append([]Tool(nil), client.tools...), client.listErr
}

func (*extensionClientFake) CallTool(context.Context, string, json.RawMessage) (CallToolResult, error) {
	return CallToolResult{}, nil
}

func (client *extensionClientFake) Close(context.Context) error {
	client.closed++
	return nil
}

type extensionAPIFake struct {
	tools []ai.Tool
	err   error
}

func (api *extensionAPIFake) RegisterTool(tool ai.Tool) error {
	if api.err != nil {
		return api.err
	}
	api.tools = append(api.tools, tool)
	return nil
}

func exaRemoteTools() []Tool {
	return []Tool{
		{Name: "unrelated", InputSchema: map[string]any{"type": "object"}},
		{Name: "web_fetch_exa", Description: "fetch", InputSchema: map[string]any{"type": "object"}},
		{Name: "web_search_exa", Description: "search", InputSchema: map[string]any{"type": "object"}},
	}
}

func TestExtensionDiscoversOnlyAllowedTools(t *testing.T) {
	client := &extensionClientFake{tools: exaRemoteTools()}
	extension, err := newExtensionWithClient(ExtensionOptions{
		Name: "exa", AllowTools: []string{"web_search_exa", "web_fetch_exa"}, ToolPrefix: "exa",
	}, client)
	if err != nil {
		t.Fatal(err)
	}
	if extension.Name() != "mcp:exa" {
		t.Fatalf("Name = %q", extension.Name())
	}
	api := &extensionAPIFake{}
	if err := extension.Register(context.Background(), api); err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(api.tools))
	for index, tool := range api.tools {
		names[index] = tool.Definition().Name
	}
	if !slices.Equal(names, []string{"exa_web_fetch_exa", "exa_web_search_exa"}) {
		t.Fatalf("registered tools = %v", names)
	}
	if client.initialized != 1 {
		t.Fatalf("Initialize calls = %d", client.initialized)
	}
	if err := extension.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if client.closed != 1 {
		t.Fatalf("Close calls = %d", client.closed)
	}
}

func TestExtensionRequiresEveryAllowedTool(t *testing.T) {
	client := &extensionClientFake{tools: exaRemoteTools()[:2]}
	extension, err := newExtensionWithClient(ExtensionOptions{
		Name: "exa", AllowTools: []string{"web_search_exa", "web_fetch_exa"},
	}, client)
	if err != nil {
		t.Fatal(err)
	}
	api := &extensionAPIFake{}
	err = extension.Register(context.Background(), api)
	if err == nil || !strings.Contains(err.Error(), "web_search_exa") || len(api.tools) != 0 {
		t.Fatalf("Register error = %v, tools = %#v", err, api.tools)
	}
}

func TestExtensionValidatesOptionsAndPropagatesRegistrationFailure(t *testing.T) {
	client := &extensionClientFake{tools: exaRemoteTools()}
	for _, options := range []ExtensionOptions{
		{Name: "", AllowTools: []string{"a"}},
		{Name: "exa", AllowTools: nil},
		{Name: "exa", AllowTools: []string{"same", "same"}},
		{Name: "exa", AllowTools: []string{"ok"}, ToolPrefix: "bad prefix"},
	} {
		if _, err := newExtensionWithClient(options, client); err == nil {
			t.Fatalf("invalid options accepted: %#v", options)
		}
	}

	extension, err := newExtensionWithClient(ExtensionOptions{Name: "exa", AllowTools: []string{"web_search_exa"}}, client)
	if err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("registry rejected")
	if err := extension.Register(context.Background(), &extensionAPIFake{err: sentinel}); !errors.Is(err, sentinel) {
		t.Fatalf("Register error = %v", err)
	}
}

var _ pi.Extension = (*Extension)(nil)
var _ pi.ExtensionCloser = (*Extension)(nil)
