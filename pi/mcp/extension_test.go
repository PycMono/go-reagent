package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/extension"
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
	extension, err := buildTestExtension(ExtensionOptions{
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
	extension, err := buildTestExtension(ExtensionOptions{
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
		if _, err := buildTestExtension(options, client); err == nil {
			t.Fatalf("invalid options accepted: %#v", options)
		}
	}

	extension, err := buildTestExtension(ExtensionOptions{Name: "exa", AllowTools: []string{"web_search_exa"}}, client)
	if err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("registry rejected")
	if err := extension.Register(context.Background(), &extensionAPIFake{err: sentinel}); !errors.Is(err, sentinel) {
		t.Fatalf("Register error = %v", err)
	}
}

var _ extension.Extension = (*mcpExtension)(nil)
var _ extension.Closer = (*mcpExtension)(nil)

// buildTestExtension 替代已删除的生产辅助函数：归一化选项后直接用 fake client 组装。
func buildTestExtension(options ExtensionOptions, client extensionClient) (*mcpExtension, error) {
	// Extension 行为测试不关心 Transport，注入 fake 以通过必填校验。
	if options.Transport == nil {
		options.Transport = &clientTransportFake{}
	}
	normalized, err := normalizeExtensionOptions(options)
	if err != nil {
		return nil, err
	}
	return buildExtension(normalized, client), nil
}

func TestNewExtensionRequiresTransport(t *testing.T) {
	if _, err := NewExtension(ExtensionOptions{Name: "exa", AllowTools: []string{"a"}}); err == nil ||
		!strings.Contains(err.Error(), "transport is required") {
		t.Fatalf("NewExtension without transport error = %v", err)
	}
	transport := &clientTransportFake{
		responses: []Response{
			{JSONRPC: "2.0", Result: json.RawMessage(`{"protocolVersion":"` + ProtocolVersion + `","capabilities":{},"serverInfo":{"name":"s","version":"1"}}`)},
			{JSONRPC: "2.0"},
			{JSONRPC: "2.0", Result: json.RawMessage(`{"tools":[{"name":"a","inputSchema":{"type":"object"}}]}`)},
		},
	}
	created, err := NewExtension(ExtensionOptions{
		Name: "exa", Transport: transport, AllowTools: []string{"a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Name() != "mcp:exa" {
		t.Fatalf("Name = %q", created.Name())
	}
	closer, ok := created.(extension.Closer)
	if !ok {
		t.Fatal("extension does not implement Closer")
	}
	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := closer.Close(closeCtx); err != nil {
		t.Fatal(err)
	}
	if transport.closed != 1 {
		t.Fatalf("transport close calls = %d", transport.closed)
	}
}
