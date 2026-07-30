package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PycMono/go-reagent/internal/schema"
)

func TestProviderFromConfigBuildsSelectedPlatform(t *testing.T) {
	path := writeAppConfig(t, `{
		"currentPlatform": "zhipu",
		"platforms": [{
			"id": "zhipu",
			"protocol": "anthropic",
			"baseURL": "https://example.com/anthropic/",
			"apiKey": "fake-key",
			"model": "glm-test"
		}]
	}`)

	llmProvider, platform, err := providerFromConfig(path)
	if err != nil {
		t.Fatalf("providerFromConfig() error = %v", err)
	}
	if llmProvider == nil || platform.ID != "zhipu" || platform.Protocol != "anthropic" || platform.Model != "glm-test" {
		t.Fatalf("provider = %T, platform = %#v", llmProvider, platform)
	}
}

func TestProviderFromConfigReturnsLoadError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	_, _, err := providerFromConfig(path)
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("providerFromConfig() error = %v, want path %q", err, path)
	}
}

func TestConfigurationPathUsesOptionalEnvironmentOverride(t *testing.T) {
	t.Setenv("CONFIG_PATH", "")
	if got := configurationPath(); got != "config.json" {
		t.Fatalf("configurationPath() = %q", got)
	}

	t.Setenv("CONFIG_PATH", " /secure/reagent.json ")
	if got := configurationPath(); got != "/secure/reagent.json" {
		t.Fatalf("configurationPath() = %q", got)
	}
}

func TestRegistryForWorkDirAdvertisesAndExecutesFileTools(t *testing.T) {
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "hello.txt"), []byte("hello from workspace"), 0o600); err != nil {
		t.Fatal(err)
	}
	serverPath := filepath.Join(workDir, "server.go")
	serverContent := "package main\n\nif true {\n\tfmt.Println(\"open\")\n}\n"
	if err := os.WriteFile(serverPath, []byte(serverContent), 0o640); err != nil {
		t.Fatal(err)
	}

	registry, closer, err := registryForWorkDir(workDir)
	if err != nil {
		t.Fatalf("registryForWorkDir() error = %v", err)
	}
	t.Cleanup(func() { _ = closer.Close() })

	definitions := registry.GetAvailableTools()
	if len(definitions) != 2 || definitions[0].Name != "edit_file" || definitions[1].Name != "read_file" {
		t.Fatalf("available tools = %#v", definitions)
	}

	result := registry.Execute(context.Background(), schema.ToolCall{
		ID:        "call-read",
		Name:      "read_file",
		Arguments: []byte(`{"path":"hello.txt"}`),
	})
	if result.IsError || result.ToolCallID != "call-read" || result.Output != "hello from workspace" {
		t.Fatalf("tool result = %#v", result)
	}

	editResult := registry.Execute(context.Background(), schema.ToolCall{
		ID:        "call-edit",
		Name:      "edit_file",
		Arguments: []byte(`{"path":"server.go","old_text":"if true {\nfmt.Println(\"open\")\n}","new_text":"if user == nil {\n\tfmt.Println(\"Forbidden!\")\n\treturn\n}"}`),
	})
	if editResult.IsError || editResult.ToolCallID != "call-edit" || editResult.Output != "成功修改文件: server.go" {
		t.Fatalf("edit tool result = %#v", editResult)
	}

	wantServer := "package main\n\nif user == nil {\n\tfmt.Println(\"Forbidden!\")\n\treturn\n}\n"
	readEditedResult := registry.Execute(context.Background(), schema.ToolCall{
		ID:        "call-read-edited",
		Name:      "read_file",
		Arguments: []byte(`{"path":"server.go"}`),
	})
	if readEditedResult.IsError || readEditedResult.Output != wantServer {
		t.Fatalf("read edited result = %#v", readEditedResult)
	}
	info, err := os.Stat(serverPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("server permissions = %o", info.Mode().Perm())
	}
}

func writeAppConfig(t *testing.T, document string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatalf("write app config: %v", err)
	}
	return path
}
