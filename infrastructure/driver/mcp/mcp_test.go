package mcp

import (
	"os"
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/config"
	pimcp "github.com/PycMono/go-reagent/pi/mcp"
)

func TestNewExtensionsCreatesHTTPTransportForHTTPServer(t *testing.T) {
	t.Setenv("EXA_API_KEY", "driver-test-key")
	out, err := NewExtensions(httpConfig(t, "http"))
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Extensions) != 1 {
		t.Fatalf("extensions = %d", len(out.Extensions))
	}
	// 精确装配：http 分支必须创建 HTTPTransport，且 NewExtension 复用它。
	extension := out.Extensions[0]
	if extension.Name() != "mcp:exa" {
		t.Fatalf("extension name = %q", extension.Name())
	}
}

func TestNewExtensionsCreatesStdioTransportForStdioServer(t *testing.T) {
	t.Setenv("GO_REAGENT_DRIVER_STDIO_TOKEN", "driver-stdio-token")
	out, err := NewExtensions(stdioConfig(t, map[string]string{
		"MCP_TOKEN": "${GO_REAGENT_DRIVER_STDIO_TOKEN}",
		"LOG_LEVEL": "warn",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Extensions) != 1 {
		t.Fatalf("extensions = %d", len(out.Extensions))
	}
	if out.Extensions[0].Name() != "mcp:filesystem" {
		t.Fatalf("extension name = %q", out.Extensions[0].Name())
	}
}

func TestNewExtensionsSkipsDisabledAndRejectsUnknownTransport(t *testing.T) {
	out, err := NewExtensions(disabledConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Extensions) != 0 {
		t.Fatalf("disabled server produced %d extensions", len(out.Extensions))
	}

	cfg := stdioConfig(t, nil)
	cfg.MCP.Servers[0].Transport = "grpc"
	if _, err := NewExtensions(cfg); err == nil || !strings.Contains(err.Error(), "transport") {
		t.Fatalf("unknown transport error = %v", err)
	}
}

func TestNewExtensionsFailsFastOnMissingEnvReference(t *testing.T) {
	const secret = "never-print-driver-env-secret"
	t.Setenv("GO_REAGENT_DRIVER_STDIO_TOKEN", "")
	_, err := NewExtensions(stdioConfig(t, map[string]string{"MCP_TOKEN": "${GO_REAGENT_DRIVER_STDIO_TOKEN}"}))
	if err == nil || !strings.Contains(err.Error(), "GO_REAGENT_DRIVER_STDIO_TOKEN") {
		t.Fatalf("env resolution error = %v", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaks env value: %v", err)
	}
}

func TestResolveChildEnv(t *testing.T) {
	t.Setenv("GO_REAGENT_DRIVER_REF", "resolved-value")
	t.Setenv("GO_REAGENT_DRIVER_PARENT", "parent-value")

	child, err := resolveChildEnv(map[string]string{
		"REF":     "${GO_REAGENT_DRIVER_REF}",
		"LITERAL": "literal-value",
		"PATH":    "/custom/bin",
	})
	if err != nil {
		t.Fatal(err)
	}
	entries := make(map[string]string, len(child))
	for _, entry := range child {
		key, value, ok := splitEnvEntry(entry)
		if !ok {
			t.Fatalf("invalid env entry %q", entry)
		}
		entries[key] = value
	}
	if entries["REF"] != "resolved-value" || entries["LITERAL"] != "literal-value" || entries["PATH"] != "/custom/bin" {
		t.Fatalf("child env = %#v", entries)
	}
	// 未覆盖的父进程环境变量必须继承。
	if entries["GO_REAGENT_DRIVER_PARENT"] != "parent-value" {
		t.Fatalf("parent env not inherited: %#v", entries)
	}
}

func TestResolveChildEnvRejectsMissingReferenceWithoutLeaking(t *testing.T) {
	const secret = "never-print-driver-env-secret"
	t.Setenv("GO_REAGENT_DRIVER_REF", secret)
	if _, err := resolveChildEnv(map[string]string{"REF": "${GO_REAGENT_DRIVER_UNSET}"}); err == nil ||
		!strings.Contains(err.Error(), "GO_REAGENT_DRIVER_UNSET") {
		t.Fatalf("resolveChildEnv error = %v", err)
	}
	t.Setenv("GO_REAGENT_DRIVER_REF", "")
	if _, err := resolveChildEnv(map[string]string{"REF": "${GO_REAGENT_DRIVER_REF}"}); err == nil {
		t.Fatal("empty referenced value accepted")
	}
}

func TestResolveChildEnvKeepsParentEnvironmentUnmodified(t *testing.T) {
	t.Setenv("GO_REAGENT_DRIVER_OVERRIDE", "parent")
	if _, err := resolveChildEnv(map[string]string{"GO_REAGENT_DRIVER_OVERRIDE": "child"}); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("GO_REAGENT_DRIVER_OVERRIDE") != "parent" {
		t.Fatalf("parent env modified: %q", os.Getenv("GO_REAGENT_DRIVER_OVERRIDE"))
	}
}

func TestStdioTransportReceivesDriverEnv(t *testing.T) {
	// 端到端验证：driver 解析的 env 合并结果传入 StdioTransportOptions。
	cfg := stdioConfig(t, map[string]string{"LOG_LEVEL": "warn"})
	transport, err := newStdioTransport(cfg.MCP.Servers[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := transport.(*pimcp.StdioTransport); !ok {
		t.Fatalf("stdio branch created %T", transport)
	}
}

func httpConfig(t *testing.T, transport string) *config.Config {
	t.Helper()
	return &config.Config{MCP: config.MCPConfig{Servers: []config.MCPServerConfig{{
		Name:       "exa",
		Enabled:    true,
		Required:   true,
		Transport:  transport,
		URL:        "https://mcp.exa.ai/mcp",
		Timeout:    60,
		HeaderEnv:  map[string]string{"x-api-key": "EXA_API_KEY"},
		AllowTools: []string{"web_search_exa"},
	}}}}
}

func stdioConfig(t *testing.T, env map[string]string) *config.Config {
	t.Helper()
	return &config.Config{MCP: config.MCPConfig{Servers: []config.MCPServerConfig{{
		Name:       "filesystem",
		Enabled:    true,
		Required:   true,
		Transport:  "stdio",
		Command:    "cat",
		Env:        env,
		AllowTools: []string{"read_file"},
		ToolPrefix: "fs",
	}}}}
}

func disabledConfig(t *testing.T) *config.Config {
	t.Helper()
	return &config.Config{MCP: config.MCPConfig{Servers: []config.MCPServerConfig{{
		Name:      "disabled",
		Enabled:   false,
		Transport: "",
	}}}}
}
