package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PycMono/go-reagent/config"
	"github.com/PycMono/go-reagent/pi"
	"github.com/PycMono/go-reagent/pi/ai"
	"github.com/PycMono/go-reagent/pi/ai/providers"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"
)

func TestNewMCPExtensionsResolvesHeaderEnvironment(t *testing.T) {
	t.Setenv("EXA_API_KEY", "test-exa-key")
	out, err := newMCPExtensions(&config.Config{MCP: config.MCPConfig{Servers: []config.MCPServerConfig{
		{
			Name: "exa", Enabled: true, Required: true, URL: "https://mcp.exa.ai/mcp", Timeout: 60,
			HeaderEnv:  map[string]string{"X-Api-Key": "EXA_API_KEY"},
			AllowTools: []string{"web_search_exa", "web_fetch_exa"},
		},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Extensions) != 1 || out.Extensions[0].Name() != "mcp:exa" {
		t.Fatalf("extensions = %#v", out.Extensions)
	}
}

func TestNewMCPExtensionsRejectsMissingHeaderEnvironment(t *testing.T) {
	const secret = "never-print-web-mcp-secret"
	_ = os.Unsetenv("MISSING_EXA_API_KEY")
	_, err := newMCPExtensions(&config.Config{MCP: config.MCPConfig{Servers: []config.MCPServerConfig{
		{
			Name: "exa", Enabled: true, Required: true, URL: "https://mcp.exa.ai/mcp", Timeout: 60,
			HeaderEnv:  map[string]string{"X-Api-Key": "MISSING_EXA_API_KEY"},
			AllowTools: []string{"web_search_exa"}, ToolPrefix: secret,
		},
	}}})
	if err == nil || !strings.Contains(err.Error(), "MISSING_EXA_API_KEY") || strings.Contains(err.Error(), secret) {
		t.Fatalf("newMCPExtensions error = %v", err)
	}
}

func TestNewMCPExtensionsIgnoresDisabledServers(t *testing.T) {
	out, err := newMCPExtensions(&config.Config{MCP: config.MCPConfig{Servers: []config.MCPServerConfig{
		{Name: "disabled", Enabled: false},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Extensions) != 0 {
		t.Fatalf("extensions = %#v", out.Extensions)
	}
}

func TestWebMCPDiscoveryFinishesBeforeConsumerLifecycle(t *testing.T) {
	server := newWebMCPTestServer(t, true)
	var consumerStarted atomic.Bool
	app, runtime := newWebMCPTestApp(t, server.URL, &consumerStarted)
	app.RequireStart()
	t.Cleanup(app.RequireStop)
	if !consumerStarted.Load() {
		t.Fatal("consumer lifecycle did not start")
	}
	names := webMCPDefinitionNames(runtime.Definitions())
	if !slices.Contains(names, "web_search_exa") || !slices.Contains(names, "web_fetch_exa") {
		t.Fatalf("tool names = %v", names)
	}
}

func TestWebMCPMissingRequiredToolPreventsConsumerStart(t *testing.T) {
	server := newWebMCPTestServer(t, false)
	var consumerStarted atomic.Bool
	app, _ := newWebMCPTestApp(t, server.URL, &consumerStarted)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := app.Start(ctx)
	if err == nil || !strings.Contains(err.Error(), "web_fetch_exa") {
		t.Fatalf("Start error = %v", err)
	}
	if consumerStarted.Load() {
		t.Fatal("consumer started after MCP discovery failure")
	}
}

func newWebMCPTestApp(t *testing.T, endpoint string, consumerStarted *atomic.Bool) (*fxtest.App, pi.ToolRuntime) {
	t.Helper()
	t.Setenv("WEB_MCP_TEST_KEY", "test-mcp-key")
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("You are a test chat Agent."), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{MCP: config.MCPConfig{Servers: []config.MCPServerConfig{
		{
			Name: "exa", Enabled: true, Required: true, URL: endpoint, Timeout: 2,
			HeaderEnv:  map[string]string{"X-Api-Key": "WEB_MCP_TEST_KEY"},
			AllowTools: []string{"web_search_exa", "web_fetch_exa"},
		},
	}}}
	var runtime pi.ToolRuntime
	app := fxtest.New(
		t,
		agentRegister,
		fx.Provide(newMCPExtensions),
		fx.Supply(
			cfg,
			pi.WorkDir(root),
			providers.Options{
				ID: "test", Protocol: providers.ProtocolOpenAI, BaseURL: "https://example.test/v1/",
				APIKey: "test-key", Model: "test-model", Pricing: &providers.Pricing{},
			},
		),
		fx.Populate(&runtime),
		fx.Invoke(func(lifecycle fx.Lifecycle, toolRuntime pi.ToolRuntime) {
			lifecycle.Append(fx.Hook{OnStart: func(context.Context) error {
				names := webMCPDefinitionNames(toolRuntime.Definitions())
				if !slices.Contains(names, "web_search_exa") || !slices.Contains(names, "web_fetch_exa") {
					return fmt.Errorf("MCP tools unavailable at consumer start: %v", names)
				}
				consumerStarted.Store(true)
				return nil
			}})
		}),
	)
	return app, runtime
}

func webMCPDefinitionNames(definitions []ai.ToolDefinition) []string {
	names := make([]string, len(definitions))
	for index, definition := range definitions {
		names[index] = definition.Name
	}
	return names
}

func newWebMCPTestServer(t *testing.T, includeFetch bool) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Api-Key"); got != "test-mcp-key" {
			t.Errorf("X-Api-Key = %q", got)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var request struct {
			ID     *int64 `json:"id"`
			Method string `json:"method"`
		}
		if err := json.Unmarshal(data, &request); err != nil {
			t.Errorf("decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if request.Method == "notifications/initialized" {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		response := map[string]any{"jsonrpc": "2.0", "id": request.ID}
		switch request.Method {
		case "initialize":
			response["result"] = map[string]any{
				"protocolVersion": "2025-03-26", "capabilities": map[string]any{},
				"serverInfo": map[string]any{"name": "test", "version": "1"},
			}
		case "tools/list":
			tools := []any{map[string]any{
				"name": "web_search_exa", "description": "search", "inputSchema": map[string]any{"type": "object"},
			}}
			if includeFetch {
				tools = append(tools, map[string]any{
					"name": "web_fetch_exa", "description": "fetch", "inputSchema": map[string]any{"type": "object"},
				})
			}
			response["result"] = map[string]any{"tools": tools}
		default:
			response["error"] = map[string]any{"code": -32601, "message": "not found"}
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	t.Cleanup(server.Close)
	return server
}
